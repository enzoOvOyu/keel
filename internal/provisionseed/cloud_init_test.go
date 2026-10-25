package provisionseed

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func testIdentity(t *testing.T) Identity {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	project := strings.Repeat("1", 32)
	return Identity{ProjectID: project, MachineID: strings.Repeat("2", 32), InstanceID: strings.Repeat("3", 32), Hostname: "rack-01-node-02", PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " keel-ctrl-" + project + "-v1\n"}
}

func TestCloudInitRestrictedIdentity(t *testing.T) {
	id := testIdentity(t)
	template := []byte("#cloud-config\nlocale: en_US.UTF-8\npackages: [openssh-server]\npackage_update: false\n")
	seed, err := Render(template, id)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Render(template, id)
	if err != nil || *again != *seed {
		t.Fatal("seed rendering is not deterministic")
	}
	var parsed struct {
		Hostname         string `yaml:"hostname"`
		PreserveHostname bool   `yaml:"preserve_hostname"`
		Users            []struct {
			Name           string   `yaml:"name"`
			PasswordLocked bool     `yaml:"lock_passwd"`
			Sudo           any      `yaml:"sudo"`
			Keys           []string `yaml:"ssh_authorized_keys"`
		} `yaml:"users"`
		PasswordAuth   bool       `yaml:"ssh_pwauth"`
		DisableRoot    bool       `yaml:"disable_root"`
		DatasourceKeys bool       `yaml:"allow_public_ssh_keys"`
		Packages       []string   `yaml:"packages"`
		RunCmd         [][]string `yaml:"runcmd"`
	}
	if err = yaml.Unmarshal([]byte(seed.UserData), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname != id.Hostname || parsed.PreserveHostname || parsed.PasswordAuth || !parsed.DisableRoot || parsed.DatasourceKeys || len(parsed.Users) != 1 || parsed.Users[0].Name != "keel-ctrl" || !parsed.Users[0].PasswordLocked || parsed.Users[0].Sudo != nil || len(parsed.Users[0].Keys) != 1 || !strings.HasPrefix(parsed.Users[0].Keys[0], `restrict,command="`+Helper+`" ssh-ed25519 `) || len(parsed.Packages) != 1 || len(parsed.RunCmd) != 2 {
		t.Fatalf("restricted seed contract changed: %s", seed.UserData)
	}
	if !strings.HasPrefix(seed.UserData, "#cloud-config\n") || strings.Contains(seed.UserData, "PRIVATE KEY") || strings.Contains(seed.UserData, "password:") {
		t.Fatal("invalid seed header or credential exposure")
	}
	id.InstanceID = strings.Repeat("4", 32)
	reinstall, err := Render(template, id)
	if err != nil {
		t.Fatal(err)
	}
	if reinstall.MetaData == seed.MetaData {
		t.Fatal("a fresh provisioning request reused the old instance ID")
	}
}

func TestCloudInitRejectsUntrustedSubstitutions(t *testing.T) {
	id := testIdentity(t)
	for name, template := range map[string]string{
		"hostname override":  "hostname: attacker\n",
		"user override":      "users: [root]\n",
		"password login":     "ssh_pwauth: true\n",
		"commands":           "runcmd: [[sh, -c, id]]\n",
		"file replacement":   "write_files: []\n",
		"duplicate":          "locale: en_US\nlocale: other\n",
		"merge":              "apt: &base {primary: []}\nntp: {<<: *base}\n",
		"alias":              "packages: &x [one]\nmounts: *x\n",
		"mapping tag":        "apt: !evil {}\n",
		"root tag":           "!evil {locale: en_US}\n",
		"invalid packages":   "packages: true\n",
		"invalid boolean":    "package_upgrade: [true]\n",
		"custom tag":         "timezone: !shell id\n",
		"multiple documents": "locale: en_US\n---\nusers: [root]\n",
		"invalid UTF8":       string([]byte{0xff}),
		"oversize":           strings.Repeat(" ", MaxTemplateBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Render([]byte(template), id); err == nil {
				t.Fatal("unsafe template accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*Identity){
		"shell hostname":           func(i *Identity) { i.Hostname = "host;id" },
		"long hostname":            func(i *Identity) { i.Hostname = strings.Repeat("a", 64) },
		"foreign project key":      func(i *Identity) { i.ProjectID = strings.Repeat("5", 32) },
		"key options":              func(i *Identity) { i.PublicKey = `command="sh" ` + i.PublicKey },
		"second key":               func(i *Identity) { i.PublicKey += i.PublicKey },
		"missing request identity": func(i *Identity) { i.InstanceID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := id
			mutate(&copy)
			if _, err := Render([]byte("{}\n"), copy); err == nil {
				t.Fatal("untrusted identity accepted")
			}
		})
	}
}

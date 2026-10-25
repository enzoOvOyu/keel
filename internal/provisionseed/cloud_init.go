// Package provisionseed renders public, deterministic NoCloud seed previews.
// It has no network, token issuance, file installation or execution capability.
package provisionseed

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

const MaxTemplateBytes = 64 << 10
const Helper = "/usr/local/libexec/keel-ssh-runner"

type Identity struct{ ProjectID, MachineID, InstanceID, Hostname, PublicKey string }
type Seed struct {
	UserData string `json:"user_data"`
	MetaData string `json:"meta_data"`
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Only declarative OS settings are accepted. Identity, users, scripts, files,
// networking and callbacks are owned by the provisioning contract, never
// substituted through text templates or shell interpolation.
var templateFields = map[string]bool{
	"locale": true, "timezone": true, "keyboard": true, "packages": true,
	"package_update": true, "package_upgrade": true, "apt": true, "yum_repos": true,
	"ca_certs": true, "ntp": true, "growpart": true, "resize_rootfs": true,
	"disk_setup": true, "fs_setup": true, "mounts": true,
}

func ValidHostname(value string) bool { return hostnamePattern.MatchString(value) }

func validateTree(node *yaml.Node, depth int, count *int) error {
	*count += 1
	if depth > 16 || *count > 4096 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errors.New("cloud-init template exceeds structural bounds or uses YAML aliases")
	}
	if (node.Kind == yaml.MappingNode && node.Tag != "!!map") || (node.Kind == yaml.SequenceNode && node.Tag != "!!seq") {
		return errors.New("unsupported cloud-init container tag")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return errors.New("cloud-init template requires unique string keys without merges")
			}
			seen[key.Value] = true
		}
	}
	if node.Kind == yaml.ScalarNode && node.Tag != "!!str" && node.Tag != "!!bool" && node.Tag != "!!int" && node.Tag != "!!float" && node.Tag != "!!null" {
		return errors.New("unsupported cloud-init scalar tag")
	}
	for _, child := range node.Content {
		if err := validateTree(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func Render(template []byte, identity Identity) (*Seed, error) {
	if len(template) == 0 || len(template) > MaxTemplateBytes || !utf8.Valid(template) || !idPattern.MatchString(identity.ProjectID) || !idPattern.MatchString(identity.MachineID) || !idPattern.MatchString(identity.InstanceID) || !ValidHostname(identity.Hostname) {
		return nil, errors.New("invalid bounded template or BOM identity")
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(identity.PublicKey))
	if err != nil || key.Type() != ssh.KeyAlgoED25519 || len(options) != 0 || len(rest) != 0 || len(identity.PublicKey) > 256 || strings.Contains(identity.PublicKey, "\r") || strings.Count(identity.PublicKey, "\n") > 1 || !strings.HasPrefix(comment, "keel-ctrl-"+identity.ProjectID+"-") {
		return nil, errors.New("a plain project-domain Ed25519 public key is required")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(template))
	var doc yaml.Node
	if err = decoder.Decode(&doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("cloud-init template must be one YAML mapping")
	}
	if decoder.Decode(new(yaml.Node)) != io.EOF {
		return nil, errors.New("only one cloud-init document is allowed")
	}
	count := 0
	if err = validateTree(&doc, 0, &count); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		field, value := root.Content[i].Value, root.Content[i+1]
		switch field {
		case "locale", "timezone":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || len(value.Value) == 0 || len(value.Value) > 256 {
				return nil, errors.New("cloud-init locale/timezone requires a bounded string")
			}
		case "package_update", "package_upgrade":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				return nil, errors.New("cloud-init package flags require booleans")
			}
		case "packages", "mounts":
			if value.Kind != yaml.SequenceNode {
				return nil, errors.New("cloud-init packages/mounts require arrays")
			}
		case "resize_rootfs":
			if value.Kind != yaml.ScalarNode || (value.Tag != "!!bool" && !(value.Tag == "!!str" && value.Value == "noblock")) {
				return nil, errors.New("invalid cloud-init resize_rootfs")
			}
		default:
			if templateFields[field] && value.Kind != yaml.MappingNode {
				return nil, errors.New("cloud-init structured settings require mappings")
			}
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		if !templateFields[root.Content[i].Value] {
			return nil, errors.New("cloud-init template contains a platform-owned or unsupported field: " + root.Content[i].Value)
		}
	}
	forced := `restrict,command="` + Helper + `" ` + strings.TrimSpace(identity.PublicKey)
	owned := map[string]any{
		"hostname": identity.Hostname, "preserve_hostname": false, "manage_etc_hosts": true,
		"disable_root": true, "ssh_pwauth": false, "allow_public_ssh_keys": false,
		"ssh_deletekeys": true, "ssh_genkeytypes": []string{"ed25519"},
		"users":  []any{map[string]any{"name": "keel-ctrl", "gecos": "Keel restricted execution", "lock_passwd": true, "shell": "/bin/sh", "sudo": nil, "ssh_authorized_keys": []string{forced}}},
		"runcmd": [][]string{{"/usr/bin/test", "-x", Helper}, {"/usr/bin/install", "-d", "-m", "0700", "-o", "keel-ctrl", "-g", "keel-ctrl", "/var/lib/keel-ssh/receipts"}},
	}
	var addition yaml.Node
	if err = addition.Encode(owned); err != nil {
		return nil, err
	}
	root.Content = append(root.Content, addition.Content...)
	raw, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	metadata, err := yaml.Marshal(map[string]string{"instance-id": "keel-" + identity.InstanceID, "local-hostname": identity.Hostname})
	if err != nil {
		return nil, err
	}
	return &Seed{UserData: "#cloud-config\n" + string(raw), MetaData: string(metadata)}, nil
}

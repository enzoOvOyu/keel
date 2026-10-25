package mediamanifest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

func TestSignedOSBootRoles(t *testing.T) {
	signer, key, payload := fixture(t)
	base := payload.Artifacts[0]
	payload.Artifacts = nil
	for i, role := range []string{"kernel", "initrd", "template", "rootfs"} {
		node := base
		node.MediaID = strings.Repeat(string(rune('1'+i)), 32)
		node.Name = role
		node.Arch = "amd64"
		node.Type = "os"
		if role == "template" {
			node.Type = "config"
			node.Arch = "noarch"
		}
		node.URI = "media://" + node.Type + "/" + node.Name + "/v1"
		payload.Artifacts = append(payload.Artifacts, node)
	}
	root := &payload.Artifacts[3]
	payload.RootID = root.MediaID
	root.OSBoot = &mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: payload.Artifacts[0].MediaID, InitrdMediaID: payload.Artifacts[1].MediaID, TemplateMediaID: payload.Artifacts[2].MediaID}
	for _, child := range payload.Artifacts[:3] {
		root.Dependencies = append(root.Dependencies, Dependency{MediaID: child.MediaID, SHA256: child.SHA256})
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Payload.Artifacts[3].OSBoot == nil || *verified.Payload.Artifacts[3].OSBoot != *root.OSBoot {
		t.Fatal("boot roles lost in signed roundtrip")
	}
	for name, mutate := range map[string]func(*Payload){
		"unsupported-installer": func(p *Payload) { p.Artifacts[3].OSBoot.Installer = "arbitrary-script" },
		"self-role":             func(p *Payload) { p.Artifacts[3].OSBoot.KernelMediaID = p.RootID },
		"unbound-role":          func(p *Payload) { p.Artifacts[3].OSBoot.KernelMediaID = strings.Repeat("a", 32) },
		"aliased-role":          func(p *Payload) { p.Artifacts[3].OSBoot.KernelMediaID = p.Artifacts[3].OSBoot.InitrdMediaID },
		"wrong-kernel-arch":     func(p *Payload) { p.Artifacts[0].Arch = "noarch" },
		"non-os-root":           func(p *Payload) { p.Artifacts[3].Type = "tool"; p.Artifacts[3].URI = "media://tool/rootfs/v1" },
	} {
		t.Run(name, func(t *testing.T) {
			var p Payload
			encoded, _ := json.Marshal(payload)
			if e := json.Unmarshal(encoded, &p); e != nil {
				t.Fatal(e)
			}
			mutate(&p)
			if _, e := signer.Sign(p); e == nil {
				t.Fatal("invalid OS boot roles signed")
			}
		})
	}
	var tampered Envelope
	if err = json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Payload.Artifacts[3].OSBoot.KernelMediaID = tampered.Payload.Artifacts[3].OSBoot.InitrdMediaID
	forged, _ := json.Marshal(tampered)
	if _, err = Verify(forged, key); err == nil {
		t.Fatal("modified boot roles retained valid signature")
	}
	aliased := strings.Replace(string(raw), `"kernel_media_id":`, `"Kernel_media_id":`, 1)
	if _, err = Verify([]byte(aliased), key); err == nil {
		t.Fatal("noncanonical role key accepted")
	}
}

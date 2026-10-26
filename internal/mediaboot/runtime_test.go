package mediaboot

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestIndependentRuntimeSetRejectsAmbiguousInputs(t *testing.T) {
	s := RuntimeSet{Schema: RuntimeSetSchema, Arch: "amd64"}
	fields := []*RuntimeComponent{&s.Init, &s.Preflight, &s.SSHHelper, &s.SMART, &s.BootCA}
	for i, c := range fields {
		c.MediaID = strings.Repeat(string(rune('1'+i)), 32)
		c.SHA256 = strings.Repeat(string(rune('a'+i)), 64)
	}
	raw, _ := json.Marshal(s)
	if got, err := DecodeRuntimeSet(raw); err != nil || got != s || s.Helpers().Validate() != nil {
		t.Fatal("independent inputs require no final initrd", err)
	}
	for name, b := range map[string][]byte{
		"unknown nested": bytes.Replace(raw, []byte(`"init":{`), []byte(`"init":{"extra":"x",`), 1),
		"duplicate":      bytes.Replace(raw, []byte(`"arch":"amd64"`), []byte(`"arch":"amd64","arch":"amd64"`), 1),
		"case alias":     bytes.Replace(raw, []byte(`"media_id"`), []byte(`"Media_ID"`), 1),
		"trailing":       append(append([]byte(nil), raw...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRuntimeSet(b); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
	s.BootCA = s.Init
	if s.Validate() == nil {
		t.Fatal("same artifact in two roles")
	}
}

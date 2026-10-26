package mediaboot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIndependentHelperSetStrictData(t *testing.T) {
	set := HelperSet{Schema: HelperSetSchema, Arch: "arm64", PreflightMediaID: strings.Repeat("1", 32), SSHHelperMediaID: strings.Repeat("2", 32), PreflightSHA256: strings.Repeat("a", 64), SSHHelperSHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(set)
	if _, err := DecodeHelperSet(raw); err != nil {
		t.Fatal(err)
	}
	for _, document := range []string{
		string(raw) + `{}`, strings.Replace(string(raw), `"arch":"arm64"`, `"arch":"arm64","arch":"amd64"`, 1), strings.Replace(string(raw), `"schema":`, `"Schema":`, 1), strings.Replace(string(raw), `"arch":"arm64"`, `"arch":null`, 1), strings.Replace(string(raw), `"arch":"arm64"`, `"arch":{}`, 1), strings.Replace(string(raw), `"schema":`, `"kernel_media_id":"future-image","schema":`, 1),
	} {
		if _, err := DecodeHelperSet([]byte(document)); err == nil {
			t.Fatal("accepted ambiguous or post-image helper set")
		}
	}
}

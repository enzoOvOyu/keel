package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func TestDHCP6CandidateCommandDoesNotOverwriteOrContactServer(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	output := filepath.Join(dir, "candidate.json")
	in := dhcp6CandidateInput{Version: "kea-dhcp6-candidate/v1", Live: json.RawMessage(`{"Dhcp6":{"subnet6":[{"id":6,"subnet":"2001:db8:1::/64"}]}}`), Subnet: "2001:db8:1::/64", Owner: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bindings: []fabricboot.KeaDHCP6Reservation{{SwitchID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", DUID: "00:03:00:01:02:00:00:00:00:01", Address: "2001:db8:1::10", Hostname: "leaf-01"}}, URIs: []string{"https://boot.example"}}
	raw, _ := json.Marshal(in)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--input-file", input, "--output-file", output}
	if err := runZTPDHCP6("ztp-dhcp6-candidate", args); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 {
		t.Fatal("candidate is not private")
	}
	if err := runZTPDHCP6("ztp-dhcp6-candidate", args); err == nil {
		t.Fatal("existing candidate overwritten")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("existing candidate changed")
	}
	if err := runZTPDHCP6("ztp-dhcp6-probe", []string{"--input-file", input}); err == nil {
		t.Fatal("probe did not require explicit confirmation")
	}
	os.Chmod(input, 0644)
	if _, err := readDHCP6OperatorInput(input); err == nil {
		t.Fatal("public operator input accepted")
	}
	os.Chmod(input, 0600)
	link := filepath.Join(dir, "alias.json")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readDHCP6OperatorInput(link); err == nil {
		t.Fatal("symlink input accepted")
	}
}

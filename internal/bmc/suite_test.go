package bmc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedSuitePinsOperatorBindingAndSimOrigin(t *testing.T) {
	target := testTarget()
	config := SuiteConfig{Schema: "keel.bmc-inventory/v1", TimeoutSeconds: 5, Targets: []TargetConfig{{ProjectID: target.ProjectID, MachineID: target.MachineID, Origin: "sim", CredentialBatchRef: target.CredentialBatchRef}}}
	path := filepath.Join(t.TempDir(), "suite.json")
	save := func() {
		t.Helper()
		raw, _ := json.Marshal(config)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	save()
	suite, e := LoadSuite(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer suite.Close()
	selection, e := suite.Selection(target.ProjectID, target.MachineID)
	if e != nil {
		t.Fatal(e)
	}
	target.AdapterBindingHash = selection.BindingHash
	reader, e := suite.Reader(target)
	if e != nil {
		t.Fatal(e)
	}
	if reader.Origin() != "sim" {
		t.Fatal("fixture labeled as real")
	}
	if _, e = reader.Read(context.Background(), "/redfish/v1/"); e != nil {
		t.Fatal(e)
	}
	target.AdapterBindingHash = strings.Repeat("a", 64)
	if _, e = suite.Reader(target); e == nil {
		t.Fatal("accepted modified frozen adapter hash")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSuite(path, nil); e == nil {
		t.Fatal("accepted world-readable operator bindings")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	config.Targets[0].Origin = "real"
	save()
	if _, e = LoadSuite(path, nil); e == nil {
		t.Fatal("real adapter lacked explicit HTTPS origin")
	}
	config.Targets[0].Endpoint = "https://user:secret@bmc.invalid"
	save()
	if _, e = LoadSuite(path, nil); e == nil {
		t.Fatal("accepted credential-bearing URL")
	}
	config.Targets[0].Endpoint = "http://127.0.0.1"
	save()
	if _, e = LoadSuite(path, nil); e == nil {
		t.Fatal("accepted insecure real origin")
	}
	if _, e = ReadOwnedFile(filepath.Join(t.TempDir(), "missing"), 100); e == nil {
		t.Fatal("accepted absent protected file")
	}
}
func TestCanonicalDigestMatchesJSONBNumberNormalization(t *testing.T) {
	for _, pair := range [][2]string{{`{"n":1e3}`, `{"n":1000}`}, {`{"n":-0.000}`, `{"n":0}`}, {`{"n":0.00120}`, `{"n":12e-4}`}, {`{"n":123456789012345678901234567890}`, `{"n":123456789012345678901234567890.0}`}} {
		if CanonicalDigest([]byte(pair[0])) == "" || CanonicalDigest([]byte(pair[0])) != CanonicalDigest([]byte(pair[1])) {
			t.Fatalf("number normalization differs: %v", pair)
		}
	}
	if CanonicalDigest([]byte(`{"n":1e999999999}`)) != "" {
		t.Fatal("unbounded decimal exponent accepted")
	}
}

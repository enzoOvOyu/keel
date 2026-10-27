package bmc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureFRU(serial string) []byte {
	area := []byte{1, 0, 0}
	for _, value := range []string{"Keel Fixture", "Simulated System", "PART-0001", "v1", serial, "fixture-asset", "fixture-file"} {
		area = append(area, 0xc0|byte(len(value)))
		area = append(area, []byte(value)...)
	}
	area = append(area, 0xc1)
	for (len(area)+1)%8 != 0 {
		area = append(area, 0)
	}
	area = append(area, 0)
	area[1] = byte(len(area) / 8)
	var sum byte
	for _, v := range area {
		sum += v
	}
	area[len(area)-1] -= sum
	header := []byte{1, 0, 0, 0, 1, 0, 0, 0}
	sum = 0
	for _, v := range header {
		sum += v
	}
	header[7] -= sum
	return append(header, area...)
}
func fixtureIPMI(serial string) *ipmiTool {
	return &ipmiTool{origin: "sim", fixture: map[string][]byte{"device-id": {0x20, 1, 2, 0x30, 0x02, 0, 0x57, 1, 0, 1, 0}, "system-guid": make([]byte, 16), "chassis-status": {1, 0, 0}, "fru-0": fixtureFRU(serial)}}
}
func TestIPMIFallbackRetainsOriginalCauseAndNeverCompletesMissingCapabilities(t *testing.T) {
	target := testTarget()
	primary := NewFixtureReader(nil, map[string]Failure{"/redfish/v1/": {Class: "timeout", Code: "request_timeout", Retryable: true}})
	collector := Collector{Reader: primary, Fallback: fixtureIPMI(target.Serial), Raw: &testRawSink{}, Config: DefaultConfig()}
	out, e := collector.Collect(context.Background(), target, "L3")
	if e != nil {
		t.Fatal(e)
	}
	if e = out.Validate(); e != nil {
		t.Fatal(e)
	}
	if out.State != "partial" || !out.IdentityMatched || !out.Degraded || out.Fallback == nil || out.Fallback.Reason != "redfish_timeout" || out.Fallback.ResourceCount != 4 || out.Origin != "sim" || out.Capabilities["Processors"] != "unknown" {
		t.Fatalf("fallback invented complete hardware: %+v", out)
	}
	for _, r := range out.Resources {
		if !strings.HasPrefix(r.Path, "/ipmi/") || r.Raw.Version == "" || r.DataSHA256 != CanonicalDigest(r.Data) {
			t.Fatal("fallback lost raw/protocol identity")
		}
	}
	// Decoded identity cannot be forged independently from the retained FRU bytes.
	resource := &out.Resources[len(out.Resources)-1]
	var envelope IPMIEnvelope
	_ = json.Unmarshal(resource.Data, &envelope)
	envelope.Decoded["ProductSerialNumber"] = "forged"
	resource.Data, _ = json.Marshal(envelope)
	resource.DataSHA256 = CanonicalDigest(resource.Data)
	if out.Validate() == nil {
		t.Fatal("forged decoded FRU identity accepted")
	}
}
func TestIPMIFallbackDoesNotBypassAuthenticationOrTLS(t *testing.T) {
	target := testTarget()
	for _, failure := range []Failure{{Class: "authentication_failed", Code: "authentication_rejected"}, {Class: "authentication_failed", Code: "credential_expired", Retryable: true}, {Class: "partial_failure", Code: "transport_or_tls_failed", Retryable: true}} {
		reader := NewFixtureReader(nil, map[string]Failure{"/redfish/v1/": failure})
		out, e := (&Collector{Reader: reader, Fallback: fixtureIPMI(target.Serial), Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
		if e != nil || out.Fallback != nil || len(out.Resources) != 0 {
			t.Fatalf("unsafe fallback for %+v: %+v %v", failure, out, e)
		}
	}
	fallback := fixtureIPMI("OTHER-SERVER")
	out, e := (&Collector{Reader: NewFixtureReader(nil, map[string]Failure{"/redfish/v1/": {Class: "version_unsupported", Code: "resource_unsupported"}}), Fallback: fallback, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil || out.IdentityMatched || out.Validate() != nil {
		t.Fatal("fallback matched another server")
	}
}
func TestIPMIOriginAndStorageFailureRemainFailures(t *testing.T) {
	target := testTarget()
	reader := NewFixtureReader(nil, map[string]Failure{"/redfish/v1/": {Class: "timeout", Code: "request_timeout"}})
	fallback := fixtureIPMI(target.Serial)
	fallback.origin = "real"
	out, e := (&Collector{Reader: reader, Fallback: fallback, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil || out.Fallback != nil || out.IdentityMatched || out.Validate() != nil {
		t.Fatal("mixed origin inventory accepted")
	}
	fallback.origin = "sim"
	out, e = (&Collector{Reader: reader, Fallback: fallback, Raw: &testRawSink{fail: true}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil || len(out.Resources) != 0 || out.IdentityMatched || out.State != "failed" || out.Validate() != nil {
		t.Fatal("raw storage failure produced usable fallback")
	}
}
func TestFRURequiresChecksumsAndProductSerial(t *testing.T) {
	valid := fixtureFRU("SN-001")
	decoded, e := decodeFRU(valid)
	if e != nil || decoded["ProductSerialNumber"] != "SN-001" {
		t.Fatalf("valid product serial lost: %+v %v", decoded, e)
	}
	for _, mutate := range []func([]byte) []byte{func(v []byte) []byte { return v[:7] }, func(v []byte) []byte { v[7]++; return v }, func(v []byte) []byte { v[len(v)-1]++; return v }, func(v []byte) []byte { return v[:len(v)-4] }} {
		if _, e = decodeFRU(mutate(append([]byte(nil), valid...))); e == nil {
			t.Fatal("corrupt or truncated FRU accepted")
		}
	}
	for _, path := range []string{"/ipmi/v2/inventory/reset", "/ipmi/v2/inventory/fru-0?command=write", "/ipmi/v2/inventory/../reset"} {
		if _, e = InventoryPath(path); e == nil {
			t.Fatal("arbitrary IPMI resource allowed")
		}
	}
	if _, e := CanonicalPath("/redfish/v1/", "/ipmi/v2/inventory/device-id"); e == nil {
		t.Fatal("HTTP reader accepted IPMI command identity")
	}
}
func TestProtectedFallbackFixturesPinContentAndRejectRealLabel(t *testing.T) {
	target := testTarget()
	dir := t.TempDir()
	redfish := filepath.Join(dir, "redfish.json")
	ipmi := filepath.Join(dir, "ipmi.json")
	suitePath := filepath.Join(dir, "suite.json")
	primary, _ := json.Marshal(map[string]any{"schema": "keel.redfish-fixture/v1", "documents": map[string]any{}, "failures": map[string]Failure{"/redfish/v1/": {Class: "timeout", Code: "request_timeout", Retryable: true}}})
	if e := os.WriteFile(redfish, primary, 0600); e != nil {
		t.Fatal(e)
	}
	encoded := map[string]string{}
	for k, v := range fixtureIPMI(target.Serial).fixture {
		encoded[k] = hex.EncodeToString(v)
	}
	raw, _ := json.Marshal(map[string]any{"schema": IPMIParserVersion, "replies": encoded})
	if e := os.WriteFile(ipmi, raw, 0600); e != nil {
		t.Fatal(e)
	}
	config := SuiteConfig{Schema: "keel.bmc-inventory/v1", TimeoutSeconds: 5, Targets: []TargetConfig{{ProjectID: target.ProjectID, MachineID: target.MachineID, Origin: "sim", CredentialBatchRef: target.CredentialBatchRef, RedfishFixtureFile: redfish, IPMI: &IPMIConfig{FixtureFile: ipmi}}}}
	save := func() {
		t.Helper()
		raw, _ := json.Marshal(config)
		if e := os.WriteFile(suitePath, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	save()
	suite, e := LoadSuite(suitePath, nil)
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
	fallback, e := suite.Fallback(target)
	if e != nil {
		t.Fatal(e)
	}
	out, e := (&Collector{Reader: reader, Fallback: fallback, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil || out.Validate() != nil || !out.IdentityMatched {
		t.Fatal("configured fixture did not use common collection path")
	}
	encoded["fru-0"] = hex.EncodeToString(fixtureFRU("NEW-SN"))
	raw, _ = json.Marshal(map[string]any{"schema": IPMIParserVersion, "replies": encoded})
	if e = os.WriteFile(ipmi, raw, 0600); e != nil {
		t.Fatal(e)
	}
	changed, e := LoadSuite(suitePath, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer changed.Close()
	if _, e = changed.Fallback(target); e == nil {
		t.Fatal("changed fixture silently replaced frozen binding")
	}
	config.Targets[0].Origin = "real"
	config.Targets[0].Endpoint = "https://127.0.0.1"
	save()
	if _, e = LoadSuite(suitePath, nil); e == nil {
		t.Fatal("real binding accepted simulated hardware")
	}
}
func TestPinnedIPMIToolUsesAnonymousCredentialDescriptor(t *testing.T) {
	target := testTarget()
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "ipmitool-fixture")
	// A local test executable, never a network fixture. Its only accepted input
	// is the fixed inventory invocation and a credential on inherited fd3.
	script := []byte("#!/bin/sh\nset -eu\n[ -z \"${IPMI_PASSWORD-}\" ]\n[ -z \"${IPMITOOL_PASSWORD-}\" ]\nfor arg in \"$@\"; do [ \"$arg\" != '-P' ]; [ \"$arg\" != 'fixture-secret' ]; done\nIFS= read -r secret < /proc/self/fd/3\n[ \"$secret\" = 'fixture-secret' ]\nprintf '20 01 02 30 02 00 57 01 00 01 00\\n'\n")
	if e := os.WriteFile(toolPath, script, 0700); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(script)
	provider := &testCredentials{value: Credentials{Username: "fixture-user", Password: "fixture-secret", BatchRef: target.CredentialBatchRef, Version: target.CredentialVersion, ExpiresAt: time.Now().Add(time.Minute)}}
	config := IPMIConfig{Address: "127.0.0.1", ToolPath: toolPath, ToolSHA256: hex.EncodeToString(sum[:])}
	tool, _, e := newIPMITool(config, "sim", provider, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	payload, e := tool.ReadIPMI(context.Background(), target, "device-id")
	if e != nil || len(payload) != 11 {
		t.Fatalf("fixed descriptor invocation failed: %v", e)
	}
	if _, e = tool.ReadIPMI(context.Background(), target, "reset"); e == nil {
		t.Fatal("unlisted operation executed")
	}
	if e = os.WriteFile(toolPath, append(script, []byte("# changed\n")...), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = tool.ReadIPMI(context.Background(), target, "device-id"); e == nil {
		t.Fatal("modified executable passed its frozen digest")
	}
}

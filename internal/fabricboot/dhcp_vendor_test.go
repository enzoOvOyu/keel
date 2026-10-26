package fabricboot

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// These are explicit fixture values, not a supported vendor allocation.
func testDHCPVendorProfile() DHCPVendorProfile {
	return DHCPVendorProfile{Version: "dhcp-vendor-tlv/v1", SourceRef: "protocol-fixture-only", VCI: "fixture-nos", NOS: "fixture", VersionIntent: "1.x", NOSSuboption: 101, VersionSuboption: 102}
}

func TestDHCPVendorContract(t *testing.T) {
	p := testDHCPVendorProfile()
	data, err := p.Payload()
	if err != nil || hex.EncodeToString(data) != "6507666978747572656603312e78" {
		t.Fatalf("payload %x: %v", data, err)
	}
	for _, change := range []func(*DHCPVendorProfile){
		func(p *DHCPVendorProfile) { p.Version = "" },
		func(p *DHCPVendorProfile) { p.SourceRef = "" },
		func(p *DHCPVendorProfile) { p.VCI = "fixture\n" },
		func(p *DHCPVendorProfile) { p.NOSSuboption = 0 },
		func(p *DHCPVendorProfile) { p.VersionSuboption = 255 },
		func(p *DHCPVendorProfile) { p.VersionSuboption = p.NOSSuboption },
		func(p *DHCPVendorProfile) {
			p.NOS = strings.Repeat("x", 128)
			p.VersionIntent = strings.Repeat("y", 128)
		},
	} {
		bad := p
		change(&bad)
		if _, err := bad.Payload(); err == nil {
			t.Fatal("invalid profile accepted", bad)
		}
	}
	proof := &DHCPProof{Option60VCI: p.VCI, Option43Hex: hex.EncodeToString(data), VendorProfileSHA256: ManagementPlanDigest(p)}
	if !p.MatchesProof(proof) || p.MatchesProof(nil) {
		t.Fatal("vendor proof binding")
	}
	changed := p
	changed.SourceRef = "different-contract"
	if changed.MatchesProof(proof) {
		t.Fatal("different vendor contract accepted")
	}
	a, err := NewProbeSuite(ProbeConfig{Version: "fabric-probes/v1", Vendor: &p})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewProbeSuite(ProbeConfig{Version: "fabric-probes/v1", Vendor: &changed})
	if err != nil || a.Fingerprint == b.Fingerprint {
		t.Fatal("vendor profile not bound to environment fingerprint", err)
	}
	copy := a.DHCPVendorProfile()
	copy.VCI = "mutated"
	if a.DHCPVendorProfile().VCI != p.VCI {
		t.Fatal("profile exposes mutable configuration")
	}
}

func TestKeaVendorReservationOwnership(t *testing.T) {
	live := json.RawMessage(`{"Dhcp4":{"subnet4":[{"subnet":"192.0.2.0/24","reservations":[]}]},"Logging":{"keep":true}}`)
	bindings := []KeaReservation{{SwitchID: "switch-1", MAC: "02:00:00:00:00:01", IP: "192.0.2.10", Hostname: "fixture-switch"}}
	p := testDHCPVendorProfile()
	first, err := MergeBootstrapVendor(live, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project", bindings, nil, p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MergeBootstrapVendor(first, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project", bindings, nil, p)
	if err != nil || KeaConfigDigest(first) != KeaConfigDigest(second) {
		t.Fatal("owned profile not repeatable", err)
	}
	var config map[string]any
	_ = json.Unmarshal(first, &config)
	dhcp := config["Dhcp4"].(map[string]any)
	record := dhcp["subnet4"].([]any)[0].(map[string]any)["reservations"].([]any)[0].(map[string]any)
	options := record["option-data"].([]any)
	if len(options) != 2 || config["Logging"].(map[string]any)["keep"] != true {
		t.Fatal("unrelated configuration changed")
	}
	if _, err := MergeBootstrap239(first, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project", bindings); err == nil {
		t.Fatal("vendor option silently removed")
	}
	delete(record["user-context"].(map[string]any), "keel-vendor-profile")
	foreign, _ := json.Marshal(config)
	if _, err := MergeBootstrapVendor(foreign, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project", bindings, nil, p); err == nil {
		t.Fatal("independent option43 adopted")
	}
	for _, defs := range []string{
		`"option-def":[{"code":43,"type":"binary"}]`,
		`"client-classes":[{"name":"foreign","option-def":[{"code":43,"type":"binary"}]}]`,
		`"option-def":[{"code":101,"space":"vendor-encapsulated-options-space","type":"string"}]`,
	} {
		raw := strings.Replace(string(live), `"subnet4":`, defs+`,"subnet4":`, 1)
		if _, err := MergeBootstrapVendor(json.RawMessage(raw), "192.0.2.0/24", "https://boot.example.invalid/ztp", "project", bindings, nil, p); err == nil {
			t.Fatal("custom vendor definition overwritten", defs)
		}
	}
}

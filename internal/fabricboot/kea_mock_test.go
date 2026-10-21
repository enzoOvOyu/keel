package fabricboot

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func sampleOptions() DHCPOptions {
	return DHCPOptions{
		ProjectID: "0123456789abcdef0123456789abcdef", BatchRef: "rack-a", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24",
		Option239URL:  "https://boot.example.invalid/ztp?batch=rack-a&stage=N-S4",
		Option143URIs: []string{"https://boot.example.invalid/sztp"},
		Option60VCI:   "CumulusLinux", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"},
	}
}
func sampleDiscover() DORARequest {
	return DORARequest{ProjectID: sampleOptions().ProjectID, BatchRef: "rack-a", StageRef: "N-S4", Interface: "eth0", LinkIP: "192.0.2.1", VendorClass: "CumulusLinux"}
}
func TestMockKeaStageCommitDORA(t *testing.T) {
	mock := NewMockKea()
	config := sampleOptions()
	digest, err := mock.Stage(config)
	if err != nil {
		t.Fatal(err)
	}
	req := sampleDiscover()
	before, err := mock.DORA(req)
	if err != nil || before.Outcome != "no_offer" || before.Reason != "no_active_plan" {
		t.Fatalf("staging leaked into offers: %+v %v", before, err)
	}
	if err = mock.Commit(config.ProjectID, config.BatchRef, config.StageRef, "wrong"); err == nil {
		t.Fatal("wrong staged digest accepted")
	}
	if err = mock.Commit(config.ProjectID, config.BatchRef, config.StageRef, digest); err != nil {
		t.Fatal(err)
	}
	got, err := mock.DORA(req)
	if err != nil || got.Outcome != "ack" || got.ConfigHash != digest {
		t.Fatalf("committed offer: %+v %v", got, err)
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/kea_dora_golden.json")
	if err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if !bytes.Equal(append(raw, '\n'), expected) {
		t.Fatalf("DORA contract drift:\n%s", raw)
	}
	// Stage a replacement: current active offer remains until exact commit.
	config.Option239URL = "https://boot.example.invalid/ztp-v2?batch=rack-a&stage=N-S4"
	nextDigest, err := mock.Stage(config)
	if err != nil || nextDigest == digest {
		t.Fatalf("staging replacement: %s %v", nextDigest, err)
	}
	unchanged, err := mock.DORA(req)
	if err != nil || unchanged.ConfigHash != digest {
		t.Fatalf("staged replacement changed active offer: %+v %v", unchanged, err)
	}
	if err = mock.Commit(config.ProjectID, config.BatchRef, config.StageRef, digest); err == nil {
		t.Fatal("stale staged digest accepted")
	}
	if err = mock.Commit(config.ProjectID, config.BatchRef, config.StageRef, nextDigest); err != nil {
		t.Fatal(err)
	}
	replaced, err := mock.DORA(req)
	if err != nil || replaced.ConfigHash != nextDigest || replaced.Option239URL != config.Option239URL {
		t.Fatalf("committed replacement absent: %+v %v", replaced, err)
	}
}
func TestMockKeaManagementOnlyAndValidation(t *testing.T) {
	mock := NewMockKea()
	cfg := sampleOptions()
	digest, err := mock.Stage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = mock.Commit(cfg.ProjectID, cfg.BatchRef, cfg.StageRef, digest); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*DORARequest){
		"front-panel":  func(r *DORARequest) { r.Interface = "swp1" },
		"outside-pool": func(r *DORARequest) { r.LinkIP = "198.51.100.1" },
		"wrong-vendor": func(r *DORARequest) { r.VendorClass = "OtherNOS" },
		"wrong-batch":  func(r *DORARequest) { r.BatchRef = "rack-b" },
	} {
		req := sampleDiscover()
		change(&req)
		got, err := mock.DORA(req)
		if err != nil || got.Outcome != "no_offer" || got.Option239URL != "" || len(got.Option143URIs) != 0 {
			t.Fatalf("%s received ZTP options: %+v %v", name, got, err)
		}
	}
	cases := map[string]func(*DHCPOptions){
		"identity-in-url":  func(x *DHCPOptions) { x.Option239URL += "&%73erial=SW-1" },
		"wrong-route":      func(x *DHCPOptions) { x.Option239URL = "https://boot.example.invalid/ztp?batch=other&stage=N-S4" },
		"insecure-sztp":    func(x *DHCPOptions) { x.Option143URIs = []string{"http://boot.example.invalid/sztp"} },
		"front-panel-pool": func(x *DHCPOptions) { x.ManagementCIDR = "198.51.100.0/24"; x.Option239URL += "&mac=aa:bb:cc:dd:ee:ff" },
		"missing-nos":      func(x *DHCPOptions) { delete(x.Option43, "nos") },
		"bad-prefix":       func(x *DHCPOptions) { x.ManagementCIDR = "192.0.2.1/24" },
	}
	badScope := sampleDiscover()
	badScope.ProjectID = cfg.ProjectID + "\x00" + cfg.BatchRef
	if _, err = mock.DORA(badScope); err == nil {
		t.Fatal("invalid project scope accepted")
	}
	if err = mock.Commit(cfg.ProjectID+"\x00", cfg.BatchRef, cfg.StageRef, digest); err == nil {
		t.Fatal("invalid commit scope accepted")
	}
	for name, change := range cases {
		x := sampleOptions()
		change(&x)
		if _, err := NewMockKea().Stage(x); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

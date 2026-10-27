package monitor

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestMonitorPoliciesEnforceGlobalAndPerDeviceBudgets(t *testing.T) {
	for _, stage := range []string{"M1", "M2", "M3", "M4", "maintained"} {
		if p := DefaultPolicy(stage); p.Validate() != nil {
			t.Fatal("invalid documented policy", stage)
		}
	}
	p := DefaultPolicy("M4")
	p.WindowBytesPerSecond = 200001
	if p.Validate() == nil {
		t.Fatal("window device cap ignored")
	}
	p = DefaultPolicy("maintained")
	p.Intervals["D1"] = 10
	if p.Validate() == nil {
		t.Fatal("maintained in-band collection allowed")
	}
	for _, devices := range []int{1, 200, 10000, 50000} {
		share, e := Allocate(devices)
		if e != nil || devices*share > PayloadBytesPerSecond || share > 10000 {
			t.Fatal("global reservation exceeded", devices, share, e)
		}
	}
	if _, e := Allocate(100000); e == nil {
		t.Fatal("unadmittable population accepted")
	}
	if Jitter("one", time.Second) != Jitter("one", time.Second) || Jitter("one", time.Second) >= time.Second || Backoff(20, "one") > 96*time.Second {
		t.Fatal("jitter/backoff was unbounded or unstable")
	}
}
func TestMonitorFrameKeepsUnknownAndDeltaBaselinesExplicit(t *testing.T) {
	now := time.Now().UTC()
	value := 12.0
	f := Frame{Version: Version, ID: "frame1", PolicyID: "policy", Full: true, Samples: []Sample{{Key: "host.load1", Entity: "host", Value: &value, Quality: "good", ObservedAt: now}}}
	decode := func(f Frame) error { raw, _ := json.Marshal(f); _, e := DecodeFrame(raw, now); return e }
	if e := decode(f); e != nil {
		t.Fatal(e)
	}
	f.Full = false
	if decode(f) == nil {
		t.Fatal("delta missing baseline accepted")
	}
	f.BaselineID = "prior"
	if e := decode(f); e != nil {
		t.Fatal(e)
	}
	f.Samples[0].Quality = "unknown"
	if decode(f) == nil {
		t.Fatal("unknown observation carried a healthy number")
	}
	f.Samples[0].Value = nil
	if e := decode(f); e != nil {
		t.Fatal(e)
	}
	f.Samples = append(f.Samples, f.Samples[0])
	if decode(f) == nil {
		t.Fatal("duplicate identities accepted")
	}
	f.Samples = f.Samples[:1]
	f.Samples[0].Quality = "good"
	bad := math.Inf(1)
	f.Samples[0].Value = &bad
	if f.Samples[0].Validate(now) == nil {
		t.Fatal("infinite telemetry accepted")
	}
	if _, e := DecodeFrame([]byte(`{"version":"keel.monitor/v1","id":"frame","policy_id":"p","full":true,"samples":[],"unchanged":[],"verdict":"PASS"}`), now); e == nil {
		t.Fatal("client invented verdict field accepted")
	}
}
func TestMonitorAlertsRespectRolesFreshnessCounterResetsAndThresholds(t *testing.T) {
	now := time.Now().UTC()
	down := Sample{Key: "link.state", Entity: "nic", State: "down", Quality: "good", ObservedAt: now}
	if len(Evaluate(down, nil, false, 4, nil)) != 0 {
		t.Fatal("disabled role created network alert")
	}
	out := Evaluate(down, nil, true, 3, nil)
	if len(out) != 2 || out[0].State != "firing" || out[1].Category != "T4" {
		t.Fatal("down/flapping classification lost")
	}
	down.Quality = "unknown"
	down.State = ""
	out = Evaluate(down, nil, true, 0, nil)
	if out[0].State != "unknown" {
		t.Fatal("unknown became resolved or healthy")
	}
	value := -10.0
	optics := Sample{Key: "dom.rx_power", Entity: "nic", Value: &value, Quality: "good", ObservedAt: now}
	if out = Evaluate(optics, nil, true, 0, nil); out[0].State != "unknown" {
		t.Fatal("missing catalog threshold invented healthy optics")
	}
	min := -8.0
	out = Evaluate(optics, nil, true, 0, map[string]Limit{"rx_dbm": {CriticalMin: &min}})
	if out[0].State != "firing" || out[0].Severity != "critical" || out[0].Category != "T3" {
		t.Fatal("catalog optics threshold not applied")
	}
	before := 100.0
	after := 1.0
	prior := Sample{Key: "gpu.ecc", Entity: "gpu", Value: &before, Quality: "good", ObservedAt: now.Add(-time.Minute)}
	current := prior
	current.Value = &after
	current.ObservedAt = now
	out = Evaluate(current, &prior, false, 0, nil)
	if out[0].State == "firing" {
		t.Fatal("ECC reset counted as growth")
	}
	after = 101
	out = Evaluate(current, &prior, false, 0, nil)
	if out[0].State != "firing" {
		t.Fatal("ECC growth missed")
	}
	prior.ObservedAt = now.Add(-time.Hour)
	out = Evaluate(current, &prior, false, 0, nil)
	if out[0].State != "unknown" {
		t.Fatal("gap treated as continuous counter history")
	}
}

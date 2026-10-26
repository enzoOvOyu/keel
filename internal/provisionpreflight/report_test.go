package provisionpreflight

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportRecomputesFactsAndBindsWholeFrozenSpec(t *testing.T) {
	s, o, now := fixture()
	r, err := Evaluate(s, o, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	verified, err := EvaluateReport(s, raw, now)
	if err != nil || verified.Status != "PASS" {
		t.Fatalf("%+v %v", verified, err)
	}
	for _, edit := range []func(*Report){
		func(r *Report) { r.Spec.FencingToken++ },
		func(r *Report) { r.Spec.AttemptID = strings.Repeat("9", 32) },
		func(r *Report) { r.Spec.Expected.Serial = "other"; r.Observation.Serial = "other" },
		func(r *Report) { n := 0; r.Spec.Expected.GPUCount = &n },
		func(r *Report) { r.Spec.ExpiresAt = r.Spec.ExpiresAt.AddDate(0, 0, 1) },
		func(r *Report) { r.Observation.Serial = "different-host" },
		func(r *Report) { r.Observation.Disks[0].Healthy = nil },
		func(r *Report) { r.Status = "WAIVED" },
		func(r *Report) { r.Checks = nil },
	} {
		r, _ := Evaluate(s, o, now)
		edit(r)
		raw, _ := json.Marshal(r)
		if _, err := EvaluateReport(s, raw, now); err == nil {
			t.Fatal("accepted changed expectation or forged PASS")
		}
	}
	// A truthful failed report can be stored, but cannot become installation PASS.
	s, o, now = fixture()
	no := false
	o.Disks[0].Healthy = &no
	r, _ = Evaluate(s, o, now)
	raw, _ = json.Marshal(r)
	verified, err = EvaluateReport(s, raw, now)
	if err != nil || verified.Status != "FAIL" {
		t.Fatalf("%+v %v", verified, err)
	}
}

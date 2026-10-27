package platform

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// Evidence reuse must never depend on the time the operator opened the page.
func TestDiagnosticRecordIdentityFreezesEvidence(t *testing.T) {
	observed := time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC)
	window, crc, warn := 60, uint64(3), -10.0
	original := FabricDiagnosticsReport{
		RecordID: "first-record", CollectionID: "first-run", ProjectID: "project", PackageID: "package", DeviceID: "switch",
		EvidenceSource: "protocol_fixture", CollectedAt: &observed, ValidUntil: observed.Add(30 * time.Second),
		Policy: fabricmon.CounterPolicy{WindowSeconds: &window, CRCMax: &crc}, PolicyHash: "policy", IntentHash: "intent",
		DDMLimits:    map[string]fabricmon.Range{"rx_dbm": {WarnMin: &warn}},
		Ports:        []fabricmon.PortDiagnostics{{Port: "swp1", InUse: true, Checks: []fabricmon.DiagnosticCheck{{RuleRef: "PRECHECK-DDM@1", IssueType: "T3", State: "anomaly", Observed: map[string]string{"ddm_state": "warning"}}}}},
		EvidenceRefs: []string{"sample-digest"},
	}
	hash := func(v *FabricDiagnosticsReport, scope string) string {
		t.Helper()
		h, e := diagnosticRecordHash(v, scope)
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	clone := func() FabricDiagnosticsReport {
		t.Helper()
		raw, e := json.Marshal(original)
		if e != nil {
			t.Fatal(e)
		}
		var v FabricDiagnosticsReport
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	baseline := hash(&original, "")
	repeat := clone()
	repeat.RecordID, repeat.CollectionID, repeat.ValidUntil = "later-record", "later-run", observed.Add(time.Minute)
	if hash(&repeat, "") != baseline {
		t.Fatal("same snapshots must reuse their original record and expiry")
	}
	for _, tc := range []struct {
		name   string
		change func(*FabricDiagnosticsReport)
	}{
		{"project", func(v *FabricDiagnosticsReport) { v.ProjectID = "other" }},
		{"BOM", func(v *FabricDiagnosticsReport) { v.PackageID = "replacement" }},
		{"device", func(v *FabricDiagnosticsReport) { v.DeviceID = "other-switch" }},
		{"origin", func(v *FabricDiagnosticsReport) { v.EvidenceSource = "device" }},
		{"counter policy", func(v *FabricDiagnosticsReport) { n := uint64(4); v.Policy.CRCMax = &n }},
		{"intent version", func(v *FabricDiagnosticsReport) { v.IntentHash = "new-intent" }},
		{"DDM threshold", func(v *FabricDiagnosticsReport) { n := -11.0; v.DDMLimits["rx_dbm"] = fabricmon.Range{WarnMin: &n} }},
		{"rule version", func(v *FabricDiagnosticsReport) { v.Ports[0].Checks[0].RuleRef = "PRECHECK-DDM@2" }},
		{"sample", func(v *FabricDiagnosticsReport) { v.EvidenceRefs[0] = "new-sample-digest" }},
		{"observation", func(v *FabricDiagnosticsReport) { v.Ports[0].Checks[0].Observed["ddm_state"] = "critical" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := clone()
			tc.change(&v)
			if hash(&v, "") == baseline {
				t.Fatal("changed evidence must not alias its predecessor")
			}
		})
	}
	if hash(&original, "swp1") == baseline {
		t.Fatal("device and single-port scopes must remain distinct")
	}
	if original.RecordID != "first-record" || original.CollectionID != "first-run" || !original.ValidUntil.Equal(observed.Add(30*time.Second)) {
		t.Fatal("fingerprinting mutated the original evidence")
	}
}

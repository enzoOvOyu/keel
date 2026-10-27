package wiring

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHistoricalArchiveCannotReplaceCommandScopeOrOrigin(t *testing.T) {
	driver := newCaptureDriver()
	binding := recollectBinding(strings.Repeat("a", 32))
	archive, e := CaptureWithDriver(context.Background(), driver.expected, binding.CommandID, driver)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(archive)
	_, result, e := EvaluateArchive(raw, driver.expected, binding, "sim")
	if e != nil || result.Step2 != "BLOCKED" || result.Step3 != "BLOCKED" {
		t.Fatal("stopped partial fixture acquired a gate verdict", e, result)
	}
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "real"); e == nil {
		t.Fatal("fixture origin could be upgraded by the consumer")
	}
	changed := driver.expected
	changed.Serial += "-different"
	if _, _, e = EvaluateArchive(raw, changed, binding, "sim"); e == nil {
		t.Fatal("capture replaced the frozen command expectation")
	}
	changed = driver.expected
	changed.ScopeHash = strings.Repeat("b", 64)
	if _, _, e = EvaluateArchive(raw, changed, binding, "sim"); e == nil {
		t.Fatal("capture rebound to a different admission scope")
	}
	other := binding
	other.CommandID = strings.Repeat("c", 32)
	if _, _, e = EvaluateArchive(raw, driver.expected, other, "sim"); e == nil {
		t.Fatal("capture rebound to another original command")
	}
}
func TestArchiveRejectsInventedMergedSwitchProjection(t *testing.T) {
	driver := newCaptureDriver()
	binding := recollectBinding(strings.Repeat("d", 32))
	ledger := newRecollectLedger()
	client := &recollectClient{ledger: ledger}
	archive, e := CaptureWithReadback(context.Background(), driver.expected, binding.CommandID, driver, NewRecollector(binding, client, ledger))
	if e != nil || archive == nil || len(archive.Capture.Missing) != 0 {
		t.Fatal("coordinated fixture not retained", e)
	}
	raw, _ := json.Marshal(archive)
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "sim"); e != nil {
		t.Fatal(e)
	}
	archive.Readback.Devices[0].Provider = "invented-projection"
	raw, _ = json.Marshal(archive)
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "sim"); e == nil {
		t.Fatal("invented projection overrode the original response")
	}
}

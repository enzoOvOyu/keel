package platform

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/assetview"
)

func TestAssetViewCSVKeepsUnknownAndEscapesFormula(t *testing.T) {
	data := &AssetViewData{View: &AssetView{Version: 2, Definition: assetview.Definition{Columns: []assetview.Column{{ID: "firmware", Title: "=HEADER"}}}}, SourceFingerprint: "source-fingerprint", Items: []assetview.Row{{ID: "asset", DeviceID: "device", Kind: "server", Name: " \t=HYPERLINK(unsafe)", Serial: "@serial", Cells: map[string]assetview.Cell{"firmware": {Values: []any{"+formula"}, State: "simulated", Tone: "unknown", Baseline: &assetview.Baseline{Key: "firmware.bios", Version: "pinned-catalog", Value: "-baseline", State: "active"}}}, Sources: map[string]assetview.Source{"redfish": {Origin: "sim", State: "simulated"}}}}}
	raw, e := AssetViewCSV(data)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if e != nil || len(rows) != 2 {
		t.Fatal("CSV malformed", e)
	}
	if rows[0][8] != "'=HEADER" || !strings.HasPrefix(rows[1][3], "'") || rows[1][4] != "'@serial" || rows[1][8] != "'+formula" || rows[1][9] != "simulated" || rows[1][10] != "unknown" || !strings.Contains(rows[1][11], "pinned-catalog") {
		t.Fatal("CSV lost source state or spreadsheet defense", rows)
	}
}

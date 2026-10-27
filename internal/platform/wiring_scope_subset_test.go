package platform

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

func TestWiringPortRerunKeepsOriginalBaseline(t *testing.T) {
	raw, e := os.ReadFile("../../sim/fixtures/lldp/expectation.json")
	if e != nil {
		t.Fatal(e)
	}
	var full wiring.Expectation
	if e = json.Unmarshal(raw, &full); e != nil {
		t.Fatal(e)
	}
	selected := full
	selected.Ports = append([]wiring.ExpectedPort{}, full.Ports[1])
	if !wiringExpectationWithin(full, selected) {
		t.Fatal("an exact rail/physical-port subset was rejected")
	}
	for name, change := range map[string]func(*wiring.Expectation){
		"scope":     func(v *wiring.Expectation) { v.ScopeHash = strings.Repeat("b", 64) },
		"machine":   func(v *wiring.Expectation) { v.MachineID = strings.Repeat("c", 32) },
		"port":      func(v *wiring.Expectation) { v.Ports[0].PhysicalPortID = strings.Repeat("d", 32) },
		"rail":      func(v *wiring.Expectation) { v.Ports[0].Rail = "different" },
		"cable":     func(v *wiring.Expectation) { v.Ports[0].CableID = strings.Repeat("e", 32) },
		"peer":      func(v *wiring.Expectation) { v.Ports[0].SwitchPort = "swp999" },
		"BDF":       func(v *wiring.Expectation) { v.Ports[0].BDF = "0000:aa:00.0" },
		"duplicate": func(v *wiring.Expectation) { v.Ports = append(v.Ports, v.Ports[0]) },
		"empty":     func(v *wiring.Expectation) { v.Ports = nil },
	} {
		t.Run(name, func(t *testing.T) {
			copy := selected
			copy.Ports = append([]wiring.ExpectedPort{}, selected.Ports...)
			change(&copy)
			if wiringExpectationWithin(full, copy) {
				t.Fatal("rerun changed the baseline")
			}
		})
	}
	if len(full.Ports) != 2 || full.Ports[1].Rail != selected.Ports[0].Rail {
		t.Fatal("selection mutated original Step 1")
	}
}

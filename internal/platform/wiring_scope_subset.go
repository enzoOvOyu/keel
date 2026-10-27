package platform

import "github.com/enzoOvOyu/keel/internal/wiring"

// Selection may narrow only the port list. Every physical identity and the
// original full-batch scope hash remain fixed by Step 1.
func wiringExpectationWithin(full, selected wiring.Expectation) bool {
	if full.Validate() != nil || selected.Validate() != nil || len(selected.Ports) > len(full.Ports) {
		return false
	}
	ports := full.Ports
	full.Ports = selected.Ports
	if string(networkJSON(full)) != string(networkJSON(selected)) {
		return false
	}
	for _, selectedPort := range selected.Ports {
		found := false
		for _, original := range ports {
			if string(networkJSON(original)) == string(networkJSON(selectedPort)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

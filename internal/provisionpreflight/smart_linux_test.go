//go:build linux

package provisionpreflight

import (
	"fmt"
	"testing"
)

func TestSMARTUnknownFailureAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		exit       int
		want       *bool
	}{
		{"healthy", `{"smartctl":{"exit_status":0},"smart_status":{"passed":true}}`, 0, boolPtr(true)},
		{"current_threshold_failure", `{"smartctl":{"exit_status":16},"smart_status":{"passed":true}}`, 16, boolPtr(false)},
		{"overall_failure", `{"smartctl":{"exit_status":8},"smart_status":{"passed":false}}`, 8, boolPtr(false)},
		{"history_not_current_failure", `{"smartctl":{"exit_status":64},"smart_status":{"passed":true}}`, 64, boolPtr(true)},
		{"unsupported", `{"smartctl":{"exit_status":2},"smart_status":{"passed":true}}`, 2, nil},
		{"missing_status", `{"smartctl":{"exit_status":0}}`, 0, nil},
		{"JSON_process_mismatch", `{"smartctl":{"exit_status":0},"smart_status":{"passed":true}}`, 8, nil},
		{"nvme_warning", `{"smartctl":{"exit_status":0},"smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":4}}`, 0, boolPtr(false)},
		{"nvme_missing_warning", `{"smartctl":{"exit_status":0},"smart_status":{"passed":true},"nvme_smart_health_information_log":{}}`, 0, nil},
		{"trailing_JSON", `{"smartctl":{"exit_status":0},"smart_status":{"passed":true}} {}`, 0, nil},
		{"bad_json", `{`, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disk := ParseSMART("/dev/sda", []byte(tc.body), tc.exit)
			if fmt.Sprint(deref(disk.Healthy)) != fmt.Sprint(deref(tc.want)) {
				t.Fatalf("%+v", disk)
			}
		})
	}
}
func boolPtr(v bool) *bool { return &v }
func deref(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

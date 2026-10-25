package agentclient

import (
	"testing"
	"time"
)

func TestTaskDeliveryDeadline(t *testing.T) {
	now := time.Unix(1700000000, 123000000)
	for _, test := range []struct {
		name     string
		deadline int64
		want     bool
	}{
		{"missing", 0, false},
		{"past", now.Add(-time.Millisecond).UnixMilli(), false},
		{"exact_expiry", now.UnixMilli(), false},
		{"live_lease", now.Add(5 * time.Second).UnixMilli(), true},
		{"bounded_max", now.Add(35 * time.Second).UnixMilli(), true},
		{"unbounded_future", now.Add(35*time.Second + time.Millisecond).UnixMilli(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := taskFrameCurrent(test.deadline, now); got != test.want {
				t.Fatal("delivery deadline admission", got)
			}
		})
	}
}

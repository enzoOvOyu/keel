package platform

import "testing"

func TestZTPStageOrderAndRetryBounds(t *testing.T) {
	for i, stage := range ztpStages {
		want := ""
		if i+1 < len(ztpStages) {
			want = ztpStages[i+1]
		}
		if got := nextZTPStage(stage); got != want {
			t.Fatalf("%s -> %s, want %s", stage, got, want)
		}
	}
	for _, terminal := range []string{"managed", "isolated", "conflict", "failed"} {
		if nextZTPStage(terminal) != "" {
			t.Fatalf("terminal state %s advanced", terminal)
		}
	}
	if ztpRetryDelay(1).Seconds() != 10 || ztpRetryDelay(2).Seconds() != 20 || ztpRetryDelay(3).Seconds() != 40 {
		t.Fatal("retry backoff changed")
	}
}

package platform

import (
	"testing"
	"time"
)

func TestWiringStoredTimeUsesBinaryMicrosecondsWithoutRewritingOriginal(t *testing.T) {
	original := time.Date(2026, 10, 27, 12, 0, 0, 123456789, time.UTC)
	stored := original.Truncate(time.Microsecond)
	if !wiringStoredTimeEqual(stored, original) {
		t.Fatal("binary timestamptz projection rejected")
	}
	if wiringStoredTimeEqual(stored.Add(time.Microsecond), original) {
		t.Fatal("a different microsecond was accepted")
	}
	if original.Nanosecond() != 123456789 {
		t.Fatal("original timestamp was rewritten")
	}
}

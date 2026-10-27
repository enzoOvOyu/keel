package platform

import "time"

// pgx binary timestamptz stores integer microseconds. Compare that exact
// projection while retaining original nanoseconds in pinned raw JSON/hashes.
// This is storage precision, not a freshness or advertising-window tolerance.
func wiringStoredTimeEqual(stored, original time.Time) bool {
	return stored.Equal(original.Truncate(time.Microsecond))
}

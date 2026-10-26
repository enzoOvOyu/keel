package agentclient

import (
	"fmt"
	"testing"
)

func TestTaskFrameWindowBoundedAcrossLongTransfers(t *testing.T) {
	window := taskRequestWindow{active: map[string]bool{}, recent: map[string]bool{}}
	if !window.admit("active") || window.admit("active") {
		t.Fatal("active duplicate admitted")
	}
	for i := 0; i < 2048; i++ {
		id := fmt.Sprint(i)
		if !window.admit(id) {
			t.Fatal("long transfer truncated", i)
		}
		window.complete(id)
		if window.admit(id) {
			t.Fatal("recent duplicate admitted")
		}
	}
	if len(window.recent) != 256 || len(window.order) != 256 || len(window.active) != 1 || window.admit("active") {
		t.Fatal("unbounded or evicted active frame")
	}
	window.complete("active")
	if window.admit("active") {
		t.Fatal("just completed duplicate admitted")
	}
}

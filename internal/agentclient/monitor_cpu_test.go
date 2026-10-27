package agentclient

import "testing"

func TestMonitorCPUUsesCounterDeltaAndPreservesUnknownOnReset(t *testing.T) {
	counter := &monitorCPUCounter{}
	if _, ok := counter.observe([]byte("cpu 10 0 10 80 0 0 0 0 999 999\n")); ok {
		t.Fatal("first counter invented utilization")
	}
	value, ok := counter.observe([]byte("cpu 20 0 20 160 0 0 0 0 999 999\n"))
	if !ok || value != 20 {
		t.Fatal("guest double counted or delta lost", value, ok)
	}
	if _, ok = counter.observe([]byte("cpu 1 0 1 8 0 0 0 0\n")); ok {
		t.Fatal("reset counted as CPU utilization")
	}
	if _, ok = counter.observe([]byte("cpu invalid\n")); ok {
		t.Fatal("malformed CPU counter accepted")
	}
}

package fabricmon

import (
	"testing"
	"time"
)

func TestLogAnalysisAggregatesAndRetainsUnknown(t *testing.T) {
	now := time.Now().UTC()
	rules, e := DefaultLogRules()
	if e != nil {
		t.Fatal(e)
	}
	entries := []LogEntry{{At: now, Level: "error", Source: "syslog", Process: "bgpd", Message: "BGP peer down", Raw: "BGP peer down", ObjectRef: "swp1"}, {At: now.Add(-time.Minute), Level: "error", Source: "snmp_trap", Message: "BGP peer down", Raw: "BGP peer down", ObjectRef: "swp2"}, {At: now, Level: "error", Source: "syslog", Process: "x", Message: "odd failure 123", Raw: "odd failure 123"}, {At: now, Level: "error", Source: "syslog", Process: "x", Message: "odd failure 456", Raw: "odd failure 456"}}
	out := AnalyzeLogs(entries, rules, now.Add(-time.Hour), "", "")
	if len(out) != 2 {
		t.Fatalf("findings %+v", out)
	}
	for _, f := range out {
		if f.Count != 2 || f.RuleRef == "" {
			t.Fatalf("aggregation %+v", f)
		}
	}
	if len(AnalyzeLogs(entries, rules, now.Add(-time.Hour), "warning", "")) != 0 {
		t.Fatal("level filter ignored")
	}
	if len(AnalyzeLogs(entries, rules, now.Add(-time.Hour), "", "BGP")) != 1 {
		t.Fatal("keyword filter ignored")
	}
	entry := entries[0]
	entry.Raw = "password=unsafe"
	if entry.Validate(now.Add(-time.Hour), now) == nil {
		t.Fatal("credentials accepted")
	}
	entry = entries[0]
	entry.At = now.Add(-2 * time.Hour)
	if entry.Validate(now.Add(-time.Hour), now) == nil {
		t.Fatal("out of window accepted")
	}
}

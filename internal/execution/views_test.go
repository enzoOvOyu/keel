package execution

import (
	"encoding/json"
	"testing"
)

func TestJournalCursorRequiresFullResyncWhenOutsideRetainedRange(t *testing.T) {
	for _, c := range []struct {
		after, min, max int64
		want            bool
	}{{0, 40, 100, false}, {40, 40, 100, false}, {75, 40, 100, false}, {39, 40, 100, true}, {101, 40, 100, true}, {0, 0, 0, false}, {1, 0, 0, true}} {
		if got := cursorRequiresResync(c.after, c.min, c.max); got != c.want {
			t.Fatalf("cursor %+v got %v", c, got)
		}
	}
}
func TestJournalSeverityPreservesUnknownAndFailedReceipts(t *testing.T) {
	for _, c := range []struct{ kind, payload, level string }{{"attempt.result_unknown", `{}`, "warning"}, {"execution.authorization_blocked", `{}`, "warning"}, {"attempt.receipt_applied", `{"result":"failed"}`, "error"}, {"attempt.receipt_applied", `{"result":"success"}`, "info"}} {
		if level := JournalSeverity(Event{Type: c.kind, Payload: json.RawMessage(c.payload)}); level != c.level {
			t.Fatalf("%s: %s", c.kind, level)
		}
	}
}

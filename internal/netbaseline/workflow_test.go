package netbaseline

import (
	"strings"
	"testing"
)

func TestPushApprovalAndTransitions(t *testing.T) {
	hash := strings.Repeat("a", 64)
	signatures := []Signature{{UserID: "ne", Role: "NE", ReviewHash: hash}, {UserID: "de", Role: "DE", ReviewHash: hash}}
	if ApprovalQuorum("executor", hash, false, signatures) != nil {
		t.Fatal("ordinary independent signatures rejected")
	}
	if ApprovalQuorum("executor", hash, true, signatures) == nil {
		t.Fatal("critical operation admitted without NH")
	}
	signatures = append(signatures, Signature{UserID: "nh", Role: "NH", ReviewHash: hash})
	if ApprovalQuorum("executor", hash, true, signatures) != nil {
		t.Fatal("critical signatures rejected")
	}
	signatures[2].UserID = "ne"
	if ApprovalQuorum("executor", hash, true, signatures) == nil {
		t.Fatal("one account counted twice")
	}
	signatures[2].UserID = "executor"
	if ApprovalQuorum("executor", hash, true, signatures) == nil {
		t.Fatal("self approval admitted")
	}
	for _, pair := range [][2]string{{"CREATED", "EXECUTING"}, {"READY", "EXECUTING"}, {"REJECTED", "PRECHECKING"}, {"PARTIAL", "EXECUTING"}, {"ARCHIVED", "CREATED"}} {
		if TransitionAllowed(pair[0], pair[1]) {
			t.Fatal("forbidden state transition", pair)
		}
	}
	if !TransitionAllowed("BLOCKED", "PRECHECKING") || !TransitionAllowed("VERIFYING", "PARTIAL") || !TransitionAllowed("ROLLING_BACK", "ROLLED_BACK") {
		t.Fatal("valid recovery transition rejected")
	}
}

package execution

import "testing"

func TestRerunSelectionExcludesUnknownAndIncompleteTargets(t *testing.T) {
	snapshot := Snapshot{Targets: []Target{{ID: "a", Name: "known-failure"}, {ID: "b", Name: "unknown"}, {ID: "c", Name: "missing"}, {ID: "d", Name: "successful"}}, Flow: FlowVersion{Definition: Definition{Tasks: []Task{{ID: "one"}, {ID: "two"}}}}}
	states := []targetTaskState{{"a", "one", "failed"}, {"a", "two", "skipped"}, {"b", "one", "unknown"}, {"b", "two", "failed"}, {"c", "one", "success"}, {"d", "one", "success"}, {"d", "two", "success"}}
	selected, excluded, e := selectRerunTargets(snapshot, states, "failed", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(selected) != 1 || selected[0].ID != "a" || len(excluded) != 3 {
		t.Fatalf("selection %+v excluded %+v", selected, excluded)
	}
	selected, excluded, e = selectRerunTargets(snapshot, states, "targets", []string{"b"})
	if e != nil || len(selected) != 0 || len(excluded) != 1 || excluded[0].Reason != "result_unknown_requires_reconciliation" {
		t.Fatalf("unknown target %+v %+v %v", selected, excluded, e)
	}
	selected, _, e = selectRerunTargets(snapshot, states, "targets", []string{"d"})
	if e != nil || len(selected) != 1 || selected[0].ID != "d" {
		t.Fatal("explicit manual rerun of a settled successful target was lost")
	}
	if _, _, e = selectRerunTargets(snapshot, states, "targets", []string{"foreign"}); e == nil {
		t.Fatal("target outside frozen source admitted")
	}
}

func TestRerunInputBindsExactSelectionAndReason(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	valid := RerunInput{Mode: "targets", TargetIDs: []string{id}, ExpectedVersion: 2, SelectionHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ChangeReason: " review again "}
	normalized, e := normalizeRerunInput(valid, true)
	if e != nil || normalized.ChangeReason != "review again" {
		t.Fatalf("normalization: %+v %v", normalized, e)
	}
	for _, mutate := range []func(*RerunInput){
		func(in *RerunInput) { in.Mode = "all" }, func(in *RerunInput) { in.TargetIDs = []string{id, id} }, func(in *RerunInput) { in.TargetIDs = nil }, func(in *RerunInput) { in.Mode = "failed" }, func(in *RerunInput) { in.SelectionHash = "invalid" }, func(in *RerunInput) { in.ExpectedVersion = 0 }, func(in *RerunInput) { in.ChangeReason = "  " },
	} {
		in := valid
		mutate(&in)
		if _, e := normalizeRerunInput(in, true); e == nil {
			t.Fatalf("invalid request accepted: %+v", in)
		}
	}
}
func TestRerunRejectsActiveAndMissingEvidence(t *testing.T) {
	snapshot := Snapshot{Targets: []Target{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Flow: FlowVersion{Definition: Definition{Tasks: []Task{{ID: "one"}}}}}
	targets, excluded, e := selectRerunTargets(snapshot, []targetTaskState{{"a", "one", "running"}, {"c", "one", "skipped"}}, "targets", []string{"a", "b", "c"})
	if e != nil || len(targets) != 1 || targets[0].ID != "c" || len(excluded) != 2 {
		t.Fatalf("unexpected selection %+v %+v %v", targets, excluded, e)
	}
}

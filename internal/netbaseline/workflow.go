package netbaseline

import "errors"

var PushStates = []string{"CREATED", "PRECHECKING", "BLOCKED", "READY", "APPROVED", "REJECTED", "EXECUTING", "VERIFYING", "COMPLETED", "PARTIAL", "ROLLING_BACK", "ROLLED_BACK", "ARCHIVED"}

// All transitions are business transitions; a receipt or a browser boolean is
// never sufficient to bypass checks, signatures or post-change readback.
func TransitionAllowed(from, to string) bool {
	edges := map[string][]string{
		"CREATED": {"PRECHECKING"}, "PRECHECKING": {"BLOCKED", "READY"}, "BLOCKED": {"PRECHECKING"}, "READY": {"APPROVED", "REJECTED", "BLOCKED"}, "APPROVED": {"EXECUTING", "BLOCKED", "REJECTED"}, "EXECUTING": {"VERIFYING", "ROLLING_BACK", "PARTIAL"}, "VERIFYING": {"COMPLETED", "PARTIAL", "ROLLING_BACK"}, "ROLLING_BACK": {"ROLLED_BACK", "PARTIAL"}, "COMPLETED": {"ARCHIVED"}, "PARTIAL": {"ARCHIVED"}, "ROLLED_BACK": {"ARCHIVED"}, "REJECTED": {"ARCHIVED"},
	}
	for _, candidate := range edges[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

type Signature struct {
	UserID     string `json:"user_id"`
	Role       string `json:"role"`
	ReviewHash string `json:"review_hash"`
	Comment    string `json:"comment"`
}

func ApprovalQuorum(executor, reviewHash string, critical bool, signatures []Signature) error {
	if executor == "" || !digest.MatchString(reviewHash) {
		return errors.New("invalid approval snapshot")
	}
	seenUsers := map[string]bool{}
	roles := map[string]bool{}
	for _, s := range signatures {
		if s.UserID == executor || s.UserID == "" || seenUsers[s.UserID] || s.ReviewHash != reviewHash {
			return errors.New("approval identity or snapshot is invalid")
		}
		switch s.Role {
		case "NE", "DE", "NH":
		default:
			return errors.New("invalid network signature role")
		}
		seenUsers[s.UserID] = true
		roles[s.Role] = true
	}
	if !roles["NE"] || !roles["DE"] || critical && !roles["NH"] {
		return errors.New("network approval quorum is incomplete")
	}
	return nil
}
func HasCriticalDiff(differences []Difference) bool {
	for _, d := range differences {
		if d.Severity == "Critical" {
			return true
		}
	}
	return false
}

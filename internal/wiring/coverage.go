package wiring

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"
)

// PortProof identifies original immutable evidence for ONE physical port. It
// never refreshes a retained observation's timestamp when another port reruns.
type PortProof struct {
	PhysicalPortID string       `json:"physical_port_id"`
	ParentID       string       `json:"parent_id"`
	Step2ID        string       `json:"step2_id"`
	Step3ID        string       `json:"step3_id"`
	CommandID      string       `json:"command_id"`
	RawSHA256      string       `json:"raw_sha256"`
	ScopeHash      string       `json:"scope_hash"`
	PolicyHash     string       `json:"policy_hash"`
	HostOrigin     string       `json:"host_origin"`
	SwitchOrigin   string       `json:"switch_origin"`
	Expected       ExpectedPort `json:"expected"`
	Step2          string       `json:"step2"`
	Step3          string       `json:"step3"`
	Findings       []Finding    `json:"findings"`
	ObservedAt     time.Time    `json:"observed_at"`
	ValidUntil     time.Time    `json:"valid_until"`
}
type Coverage struct {
	Version        string      `json:"version"`
	ParentID       string      `json:"parent_id"`
	ScopeHash      string      `json:"scope_hash"`
	ExpectedHash   string      `json:"expected_hash"`
	PolicyHash     string      `json:"policy_hash"`
	Step2          string      `json:"step2"`
	Step3          string      `json:"step3"`
	Proofs         []PortProof `json:"proofs"`
	MissingPortIDs []string    `json:"missing_port_ids"`
	Findings       []Finding   `json:"findings"`
	ValidUntil     *time.Time  `json:"valid_until"`
	SourceHash     string      `json:"source_hash"`
}

// ComposeCoverage assembles the entire original Step 1 port denominator from
// latest per-port ORIGINAL proofs. An expired/new BLOCKED observation supersedes
// an older PASS. Untouched ports keep their own original expiry and provenance.
// Current G1/topology/network identities are checked separately by the owner.
func ComposeCoverage(expected Expectation, parent, policy string, candidates []PortProof, now time.Time) (*Coverage, error) {
	if expected.Validate() != nil || !entityID.MatchString(parent) || !scopeDigest.MatchString(policy) || now.IsZero() || len(candidates) > 2*MaxPorts {
		return nil, ErrCaptureRejected
	}
	full := map[string]ExpectedPort{}
	for _, p := range expected.Ports {
		full[p.PhysicalPortID] = p
	}
	latest := map[string]PortProof{}
	for _, p := range candidates {
		// Other Step 1 versions are historical and cannot contribute coverage here.
		if p.ParentID != parent {
			continue
		}
		want, exists := full[p.PhysicalPortID]
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(p.Expected)
		validVerdict := func(v string) bool { return v == "PASS" || v == "FAIL" || v == "BLOCKED" }
		if !exists || !bytes.Equal(a, b) || p.ScopeHash != expected.ScopeHash || !entityID.MatchString(p.Step2ID) || !entityID.MatchString(p.CommandID) || !scopeDigest.MatchString(p.RawSHA256) || !scopeDigest.MatchString(p.PolicyHash) || !validVerdict(p.Step2) || !validVerdict(p.Step3) || p.Step2 != "PASS" && p.Step3 != "BLOCKED" || p.Step3 != "BLOCKED" && !entityID.MatchString(p.Step3ID) || p.ObservedAt.IsZero() || p.ValidUntil.IsZero() || p.ObservedAt.After(now.Add(5*time.Second)) || p.HostOrigin != "real" && p.HostOrigin != "sim" || p.SwitchOrigin != "device" && p.SwitchOrigin != "protocol_fixture" {
			return nil, ErrCaptureRejected
		}
		if p.Step2 == "PASS" && !entityID.MatchString(p.Step3ID) {
			return nil, ErrCaptureRejected
		}
		for _, f := range p.Findings {
			if f.PhysicalPortID != "" && f.PhysicalPortID != p.PhysicalPortID {
				return nil, ErrCaptureRejected
			}
		}
		prior, seen := latest[p.PhysicalPortID]
		if seen && p.CommandID == prior.CommandID {
			a, _ := json.Marshal(prior)
			b, _ := json.Marshal(p)
			if !bytes.Equal(a, b) {
				return nil, ErrCaptureRejected
			}
		}
		if !seen || p.ObservedAt.After(prior.ObservedAt) || p.ObservedAt.Equal(prior.ObservedAt) && p.CommandID > prior.CommandID {
			latest[p.PhysicalPortID] = p
		}
	}
	raw, _ := json.Marshal(expected)
	out := &Coverage{Version: "keel.wiring-coverage/v1", ParentID: parent, ScopeHash: expected.ScopeHash, ExpectedHash: Hash(raw), PolicyHash: policy, Step2: "PASS", Step3: "PASS", Proofs: []PortProof{}, MissingPortIDs: []string{}, Findings: []Finding{}}
	merge := func(dst *string, v string) {
		if v == "FAIL" || *dst == "PASS" {
			*dst = v
		}
	}
	ids := make([]string, 0, len(full))
	for id := range full {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p, ok := latest[id]
		if !ok {
			out.MissingPortIDs = append(out.MissingPortIDs, id)
			merge(&out.Step2, "BLOCKED")
			out.Findings = append(out.Findings, Finding{Step: 2, PhysicalPortID: id, CableID: full[id].CableID, Verdict: "BLOCKED", Code: "port_original_evidence_required"})
			continue
		}
		// Preserve the original proof verbatim. Derived current blockers belong to
		// this coverage snapshot, never to rewritten historical observations.
		out.Proofs = append(out.Proofs, p)
		until := p.ValidUntil
		if out.ValidUntil == nil || until.Before(*out.ValidUntil) {
			out.ValidUntil = &until
		}
		if p.HostOrigin != "real" || p.SwitchOrigin != "device" || p.PolicyHash != policy || !p.ValidUntil.After(now) {
			merge(&out.Step2, "BLOCKED")
			out.Findings = append(out.Findings, Finding{Step: 2, PhysicalPortID: id, CableID: full[id].CableID, Verdict: "BLOCKED", Code: "port_evidence_source_policy_or_expiry_invalid"})
			continue
		}
		merge(&out.Step2, p.Step2)
		merge(&out.Step3, p.Step3)
		out.Findings = append(out.Findings, p.Findings...)
	}
	if out.Step2 != "PASS" {
		out.Step3 = "BLOCKED"
	}
	hashInput, _ := json.Marshal(out)
	out.SourceHash = Hash(hashInput)
	return out, nil
}

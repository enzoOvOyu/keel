// Package fabricdelivery validates immutable network evidence references and
// stage admission. It never collects devices, changes configuration or invents
// approvals; adapters provide their original conclusions and provenance.
package fabricdelivery

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

const Version = "network-delivery/v1"

//go:embed network-delivery.schema.json
var JSONSchema []byte
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Reference struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	ObjectID       string     `json:"object_id"`
	Version        string     `json:"version"`
	SHA256         string     `json:"sha256"`
	EvidenceSource string     `json:"evidence_source"`
	ObservedAt     *time.Time `json:"observed_at,omitempty"`
	ValidUntil     *time.Time `json:"valid_until,omitempty"`
}
type Object struct {
	ID             string          `json:"item_uid"`
	Category       string          `json:"category"`
	Serial         string          `json:"serial,omitempty"`
	ParentID       string          `json:"parent_uid,omitempty"`
	PortName       string          `json:"port_name,omitempty"`
	PortIndex      string          `json:"port_index,omitempty"`
	PortRole       string          `json:"port_role,omitempty"`
	ModelVersion   string          `json:"model_version_ref,omitempty"`
	ModelRef       string          `json:"model_ref,omitempty"`
	EvidenceSource string          `json:"evidence_source,omitempty"`
	EndpointA      string          `json:"endpoint_a_uid,omitempty"`
	EndpointB      string          `json:"endpoint_b_uid,omitempty"`
	Pod            string          `json:"pod,omitempty"`
	Role           string          `json:"role,omitempty"`
	Rail           string          `json:"rail,omitempty"`
	Position       json.RawMessage `json:"position,omitempty"`
	Status         string          `json:"status"`
	BOMRef         string          `json:"bom_ref"`
	AssetRef       string          `json:"asset_ref"`
	SampleRefs     []string        `json:"sample_refs"`
	State          string          `json:"state"`
	Source         string          `json:"source"`
	Details        json.RawMessage `json:"details,omitempty"`
}
type Stage struct {
	ID            string   `json:"id"`
	State         string   `json:"state"`
	Reasons       []string `json:"reasons"`
	EvidenceRefs  []string `json:"evidence_refs"`
	OriginalState string   `json:"original_state,omitempty"`
	WaiverID      string   `json:"waiver_id,omitempty"`
}
type Waiver struct {
	ID             string    `json:"id"`
	Stage          string    `json:"stage"`
	ScopeHash      string    `json:"scope_hash"`
	Approved       bool      `json:"approved"`
	ApprovalRef    string    `json:"approval_ref"`
	Reason         string    `json:"reason"`
	Compensation   string    `json:"compensation"`
	ValidUntil     time.Time `json:"valid_until"`
	EvidenceSource string    `json:"evidence_source"`
}
type Acceptance struct {
	ID              string    `json:"id"`
	ScopeHash       string    `json:"scope_hash"`
	ApprovalRef     string    `json:"approval_ref"`
	CMDBSnapshotRef string    `json:"cmdb_snapshot_ref"`
	AcceptedAt      time.Time `json:"accepted_at"`
	EvidenceSource  string    `json:"evidence_source"`
}
type Review struct {
	Acceptance *Acceptance `json:"acceptance"`
	Waivers    []Waiver    `json:"waivers"`
	Code       string      `json:"code"`
}
type Facts struct {
	referenceIndex     map[string]int
	ProjectID          string          `json:"project_id"`
	PackageID          string          `json:"package_id"`
	Stages             []Stage         `json:"stages"`
	References         []Reference     `json:"references"`
	Objects            []Object        `json:"objects"`
	Addresses          json.RawMessage `json:"addresses"`
	ConfigurationIndex json.RawMessage `json:"configuration_index"`
	RuleVersions       []string        `json:"rule_versions"`
	HardBlocks         []string        `json:"hard_blocks"`
	ValidUntil         time.Time       `json:"valid_until"`
}
type Preflight struct {
	Version              string    `json:"version"`
	ProjectID            string    `json:"project_id"`
	PackageID            string    `json:"package_id"`
	ScopeHash            string    `json:"scope_hash"`
	PreflightHash        string    `json:"preflight_hash"`
	ReadinessAt          time.Time `json:"readiness_at"`
	ValidUntil           time.Time `json:"valid_until"`
	TTLSeconds           int       `json:"ttl_seconds"`
	State                string    `json:"state"`
	DeliveryAllowed      bool      `json:"delivery_allowed"`
	ConsistencyCheckRate float64   `json:"consistency_check_rate"`
	CheckedReferences    int       `json:"checked_references"`
	TotalReferences      int       `json:"total_references"`
	ObjectCount          int       `json:"object_count"`
	Stages               []Stage   `json:"stages"`
	IntegrityErrors      []string  `json:"integrity_errors"`
	Review               Review    `json:"review"`
}
type Artifact struct {
	Version            string          `json:"version"`
	ID                 string          `json:"id"`
	Revision           int             `json:"revision"`
	Kind               string          `json:"kind"`
	Delivery           bool            `json:"is_delivery"`
	Notice             string          `json:"notice"`
	GeneratedAt        time.Time       `json:"generated_at"`
	ProjectID          string          `json:"project_id"`
	PackageID          string          `json:"package_id"`
	Preflight          Preflight       `json:"preflight"`
	References         []Reference     `json:"references"`
	Objects            []Object        `json:"objects"`
	Addresses          json.RawMessage `json:"addresses"`
	ConfigurationIndex json.RawMessage `json:"configuration_index"`
	RuleVersions       []string        `json:"rule_versions"`
}

// AddReference deduplicates exact immutable references without a quadratic scan.
// Conflicting payloads for one identity are retained as an unwaivable blocker.
func (f *Facts) AddReference(r Reference) {
	if f.referenceIndex == nil {
		f.referenceIndex = make(map[string]int, len(f.References))
		for i, old := range f.References {
			f.referenceIndex[old.ID] = i
		}
	}
	if i, exists := f.referenceIndex[r.ID]; exists {
		old := f.References[i]
		if old.SHA256 != r.SHA256 || old.EvidenceSource != r.EvidenceSource {
			f.HardBlocks = append(f.HardBlocks, "conflicting_reference:"+r.ID)
		}
		return
	}
	f.referenceIndex[r.ID] = len(f.References)
	f.References = append(f.References, r)
}

func Digest(value any) string {
	b, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var normalized any
	if decoder.Decode(&normalized) != nil {
		return ""
	}
	b, e = json.Marshal(normalized)
	if e != nil {
		return ""
	}
	return BytesDigest(b)
}
func BytesDigest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func sortedFacts(f Facts) Facts {
	f.References = append([]Reference{}, f.References...)
	sort.Slice(f.References, func(i, j int) bool { return f.References[i].ID < f.References[j].ID })
	f.Objects = append([]Object{}, f.Objects...)
	sort.Slice(f.Objects, func(i, j int) bool { return f.Objects[i].ID < f.Objects[j].ID })
	f.RuleVersions = append([]string{}, f.RuleVersions...)
	sort.Strings(f.RuleVersions)
	f.Stages = append([]Stage{}, f.Stages...)
	sort.Slice(f.Stages, func(i, j int) bool { return f.Stages[i].ID < f.Stages[j].ID })
	for i := range f.Stages {
		f.Stages[i].Reasons = append([]string{}, f.Stages[i].Reasons...)
		sort.Strings(f.Stages[i].Reasons)
		f.Stages[i].EvidenceRefs = append([]string{}, f.Stages[i].EvidenceRefs...)
		sort.Strings(f.Stages[i].EvidenceRefs)
	}
	f.HardBlocks = append([]string{}, f.HardBlocks...)
	sort.Strings(f.HardBlocks)
	return f
}

// ScopeHash excludes wall clock and N-S8 itself: a pre-existing acceptance may
// bind N-S0..7 evidence without requiring the export being generated as input.
func ScopeHash(f Facts) string {
	f = sortedFacts(f)
	f.ValidUntil = time.Time{}
	stages := []Stage{}
	for _, s := range f.Stages {
		if s.ID != "N-S8" {
			stages = append(stages, s)
		}
	}
	f.Stages = stages
	return Digest(f)
}
func Evaluate(f Facts, review Review, now time.Time) Preflight {
	f = sortedFacts(f)
	out := Preflight{Version: Version, ProjectID: f.ProjectID, PackageID: f.PackageID, ScopeHash: ScopeHash(f), ReadinessAt: now, ValidUntil: f.ValidUntil, TTLSeconds: 30, State: "PASS", DeliveryAllowed: true, Stages: []Stage{}, IntegrityErrors: append([]string{}, f.HardBlocks...), ObjectCount: len(f.Objects), TotalReferences: len(f.References), Review: review}
	refs := map[string]Reference{}
	objects := map[string]Object{}
	for _, r := range f.References {
		if r.EvidenceSource == "protocol_fixture" || r.EvidenceSource == "sim" {
			out.IntegrityErrors = append(out.IntegrityErrors, "non_device_evidence:"+r.ID)
		}
		if r.ValidUntil != nil && !r.ValidUntil.After(now) {
			out.IntegrityErrors = append(out.IntegrityErrors, "expired_reference:"+r.ID)
		}
		if r.ValidUntil != nil && r.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = *r.ValidUntil
		}
		if r.ObservedAt != nil && r.ObservedAt.After(now.Add(5*time.Second)) {
			out.IntegrityErrors = append(out.IntegrityErrors, "future_reference:"+r.ID)
		}
		valid := r.ID != "" && r.Kind != "" && r.ObjectID != "" && r.Version != "" && r.EvidenceSource != "" && hashPattern.MatchString(r.SHA256)
		if _, exists := refs[r.ID]; exists {
			valid = false
			out.IntegrityErrors = append(out.IntegrityErrors, "duplicate_reference:"+r.ID)
		}
		if !valid {
			out.IntegrityErrors = append(out.IntegrityErrors, "invalid_reference:"+r.ID)
		} else {
			out.CheckedReferences++
		}
		refs[r.ID] = r
	}
	for _, o := range f.Objects {
		if _, exists := objects[o.ID]; exists || o.ID == "" {
			out.IntegrityErrors = append(out.IntegrityErrors, "duplicate_or_missing_object:"+o.ID)
		}
		objects[o.ID] = o
	}
	for _, o := range f.Objects {
		for _, id := range append([]string{o.BOMRef, o.AssetRef}, o.SampleRefs...) {
			out.TotalReferences++
			if _, ok := refs[id]; !ok {
				out.IntegrityErrors = append(out.IntegrityErrors, "object_reference_missing:"+o.ID+":"+id)
			} else {
				out.CheckedReferences++
			}
		}
		if o.ModelRef != "" {
			out.TotalReferences++
			if _, ok := refs[o.ModelRef]; !ok {
				out.IntegrityErrors = append(out.IntegrityErrors, "model_reference_missing:"+o.ID)
			} else {
				out.CheckedReferences++
			}
		}
		for _, id := range []string{o.ParentID, o.EndpointA, o.EndpointB} {
			if id != "" {
				out.TotalReferences++
				if _, ok := objects[id]; !ok {
					out.IntegrityErrors = append(out.IntegrityErrors, "object_endpoint_missing:"+o.ID+":"+id)
				} else {
					out.CheckedReferences++
				}
			}
		}
	}

	stages := map[string]Stage{}
	for _, stage := range f.Stages {
		if _, exists := stages[stage.ID]; exists {
			out.IntegrityErrors = append(out.IntegrityErrors, "duplicate_stage:"+stage.ID)
		}
		stages[stage.ID] = stage
	}
	for i := 0; i <= 8; i++ {
		id := fmt.Sprintf("N-S%d", i)
		stage, ok := stages[id]
		if !ok {
			stage = Stage{ID: id, State: "BLOCKED", Reasons: []string{"stage_evidence_missing"}, EvidenceRefs: []string{}}
		}
		if stage.State == "PASS" && len(stage.EvidenceRefs) == 0 && id != "N-S8" {
			stage.State = "BLOCKED"
			stage.Reasons = append(stage.Reasons, "stage_evidence_missing")
		}
		if stage.Reasons == nil {
			stage.Reasons = []string{}
		}
		if stage.EvidenceRefs == nil {
			stage.EvidenceRefs = []string{}
		}
		if id == "N-S8" {
			a := review.Acceptance
			if a != nil && a.ID != "" && a.ScopeHash == out.ScopeHash && a.ApprovalRef != "" && a.CMDBSnapshotRef != "" && a.EvidenceSource == "cmdb" && !a.AcceptedAt.IsZero() && !a.AcceptedAt.After(now.Add(5*time.Second)) {
				stage = Stage{ID: id, State: "PASS", Reasons: []string{}, EvidenceRefs: []string{"cmdb:" + a.ID}}
			} else {
				stage = Stage{ID: id, State: "BLOCKED", Reasons: []string{"existing_cmdb_acceptance_required"}, EvidenceRefs: []string{}}
			}
		}
		for _, refID := range stage.EvidenceRefs {
			out.TotalReferences++
			if id == "N-S8" && review.Acceptance != nil && refID == "cmdb:"+review.Acceptance.ID {
				out.CheckedReferences++
				continue
			}
			r, exists := refs[refID]
			if !exists {
				stage.State = "BLOCKED"
				stage.Reasons = append(stage.Reasons, "stage_reference_missing")
				out.IntegrityErrors = append(out.IntegrityErrors, "stage_reference_missing:"+id+":"+refID)
			} else {
				out.CheckedReferences++
				if r.ValidUntil != nil && !r.ValidUntil.After(now) {
					stage.State = "BLOCKED"
					stage.Reasons = append(stage.Reasons, "stage_evidence_stale")
				}
			}
		}
		if stage.State != "PASS" {
			for _, w := range review.Waivers {
				if w.ID != "" && w.Stage == id && w.ScopeHash == out.ScopeHash && w.Approved && w.ApprovalRef != "" && w.Reason != "" && w.Compensation != "" && w.ValidUntil.After(now) && w.EvidenceSource == "cmdb" {
					stage.OriginalState = stage.State
					stage.State = "WAIVED"
					stage.WaiverID = w.ID
					break
				}
			}
		}
		if stage.State != "PASS" && stage.State != "WAIVED" {
			out.DeliveryAllowed = false
		}
		out.Stages = append(out.Stages, stage)
	}
	if out.TotalReferences > 0 {
		out.ConsistencyCheckRate = float64(out.CheckedReferences) / float64(out.TotalReferences)
	}
	if len(out.IntegrityErrors) > 0 || out.ConsistencyCheckRate != 1 || len(f.Objects) == 0 || !out.ValidUntil.After(now) {
		out.DeliveryAllowed = false
	}
	if !out.DeliveryAllowed {
		out.State = "BLOCKED"
	} else {
		for _, s := range out.Stages {
			if s.State == "WAIVED" {
				out.State = "WAIVED"
				break
			}
		}
	}
	out.PreflightHash = Digest(struct {
		Scope  string
		Stages []Stage
		Review Review
		Errors []string
	}{out.ScopeHash, out.Stages, review, out.IntegrityErrors})
	return out
}
func Build(id string, revision int, kind string, f Facts, p Preflight, now time.Time) (*Artifact, []byte, error) {
	if kind != "draft" && kind != "delivery" {
		return nil, nil, errors.New("kind must be draft or delivery")
	}
	if id == "" || revision < 1 || p.ScopeHash != ScopeHash(f) || p.ProjectID != f.ProjectID || p.PackageID != f.PackageID {
		return nil, nil, errors.New("artifact scope or revision is invalid")
	}
	verified := Evaluate(f, p.Review, now)
	if verified.PreflightHash != p.PreflightHash || verified.DeliveryAllowed != p.DeliveryAllowed {
		return nil, nil, errors.New("preflight was not derived from these exact facts and approvals")
	}
	if kind == "delivery" && (!verified.DeliveryAllowed || !verified.ValidUntil.After(now)) {
		return nil, nil, errors.New("formal delivery is blocked by current evidence")
	}
	f = sortedFacts(f)
	notice := "DRAFT EVIDENCE ONLY: NOT A NETWORK DELIVERY OR ACCEPTANCE"
	if kind == "delivery" {
		notice = "Network delivery baseline; waived conclusions retain their original state and reasons."
	}
	out := &Artifact{Version: Version, ID: id, Revision: revision, Kind: kind, Delivery: kind == "delivery", Notice: notice, GeneratedAt: now, ProjectID: f.ProjectID, PackageID: f.PackageID, Preflight: verified, References: f.References, Objects: f.Objects, Addresses: f.Addresses, ConfigurationIndex: f.ConfigurationIndex, RuleVersions: f.RuleVersions}
	b, e := json.Marshal(out)
	return out, b, e
}

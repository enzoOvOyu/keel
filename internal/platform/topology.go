package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/pcitopology"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

type TopologyValidateInput struct {
	ScopeHash string `json:"scope_hash"`
	Reason    string `json:"reason"`
}
type TopologyDiff struct {
	ID      string           `json:"id"`
	Detail  pcitopology.Diff `json:"detail"`
	IssueID string           `json:"issue_id"`
}
type PciTopologySnapshot struct {
	ID                 string                 `json:"id"`
	ProjectID          string                 `json:"project_id"`
	AssetID            string                 `json:"asset_id"`
	MachineID          string                 `json:"machine_id"`
	Version            int64                  `json:"version"`
	ModelVersionID     string                 `json:"model_version_id"`
	PrecollectID       string                 `json:"precollect_id"`
	G1ID               string                 `json:"g1_id"`
	ScopeHash          string                 `json:"scope_hash"`
	Origin             string                 `json:"origin"`
	RawSHA256          string                 `json:"raw_sha256"`
	Snapshot           *pcitopology.Snapshot  `json:"snapshot,omitempty"`
	Evaluation         pcitopology.Evaluation `json:"evaluation"`
	Verdict            string                 `json:"verdict"`
	ValidUntil         time.Time              `json:"valid_until"`
	CreatedAt          time.Time              `json:"created_at"`
	Scope              *TopologyScope         `json:"scope,omitempty"`
	Diffs              []TopologyDiff         `json:"diffs"`
	Validity           string                 `json:"validity"`
	EffectiveVerdict   string                 `json:"effective_verdict"`
	InvalidationReason string                 `json:"invalidation_reason"`
}

const topologyFields = `id,project_id,asset_id,machine_id,version,model_version_id,precollect_id,g1_id,scope_hash,origin,raw_sha256,evaluation,verdict,valid_until,created_at`

func scanTopology(row scanner) (*PciTopologySnapshot, error) {
	var out PciTopologySnapshot
	var evaluation []byte
	e := row.Scan(&out.ID, &out.ProjectID, &out.AssetID, &out.MachineID, &out.Version, &out.ModelVersionID, &out.PrecollectID, &out.G1ID, &out.ScopeHash, &out.Origin, &out.RawSHA256, &evaluation, &out.Verdict, &out.ValidUntil, &out.CreatedAt)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(evaluation, &out.Evaluation); e != nil {
		return nil, e
	}
	out.Diffs = []TopologyDiff{}
	out.Validity = "unchecked"
	out.EffectiveVerdict = "BLOCKED"
	return &out, nil
}

func (s *Store) ValidateTopology(ctx context.Context, a *AuthSession, project, asset string, in TopologyValidateInput, key, trace string) (*PciTopologySnapshot, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.ScopeHash) != 64 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current scope, reason and Idempotency-Key are required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if !slices.Contains([]string{"Admin", "DE", "DL", "SRE"}, role) {
		return nil, ErrForbidden
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	identity := digest("topology.validate:" + project + ":" + asset + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	var priorHash string
	var prior []byte
	e = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, identity).Scan(&priorHash, &prior)
	if e == nil {
		if priorHash != requestHash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "This key was used for another topology request")
		}
		var ref struct {
			ID string `json:"id"`
		}
		if e = json.Unmarshal(prior, &ref); e != nil {
			return nil, e
		}
		out, e := s.topologyDetailTx(ctx, tx, project, asset, ref.ID)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	scope, report, e := s.topologyScopeTx(ctx, tx, project, asset, true)
	if e != nil {
		return nil, e
	}
	if scope.ScopeHash != in.ScopeHash {
		return nil, apiError(409, "TOPOLOGY_SCOPE_CHANGED", "Refresh the current topology scope")
	}
	if !scope.Ready || report == nil {
		return nil, apiError(409, scope.Code, "Topology prerequisites are incomplete")
	}
	// Reverse-read the immutable original object before consuming its projection.
	var object, version string
	var size int64
	if e = tx.QueryRowContext(ctx, `SELECT raw_key,raw_version,raw_size FROM validation_precollect_records WHERE id=$1`, scope.PrecollectID).Scan(&object, &version, &size); e != nil {
		return nil, e
	}
	if s.MediaObjects == nil {
		return nil, apiError(409, "TOPOLOGY_RAW_REQUIRED", "Original versioned capture unavailable")
	}
	reader, e := s.MediaObjects.Open(ctx, object, version)
	if e != nil {
		return nil, apiError(409, "TOPOLOGY_RAW_REQUIRED", "Original versioned capture unavailable")
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, precollect.MaxBundleBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != size || precollect.Hash(raw) != scope.PrecollectSHA256 {
		return nil, apiError(409, "TOPOLOGY_RAW_CORRUPT", "Original capture failed digest verification")
	}
	bundle, e := precollect.Decode(raw, time.Now())
	if e != nil {
		return nil, e
	}
	original, e := precollect.Normalize(bundle)
	if e != nil {
		return nil, e
	}
	if string(networkJSON(original.Topology)) != string(networkJSON(report.Topology)) {
		return nil, apiError(409, "TOPOLOGY_PROJECTION_MISMATCH", "Topology differs from retained original capture")
	}
	evaluation := pcitopology.Evaluate(original.Topology, scope.Expectation, scope.Bindings, original.Origin == "real")
	var id string
	e = tx.QueryRowContext(ctx, `SELECT id FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 AND scope_hash=$3`, project, asset, scope.ScopeHash).Scan(&id)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if errors.Is(e, sql.ErrNoRows) {
		id = RandomID()
		var revision int64
		if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(version),0)+1 FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&revision); e != nil {
			return nil, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO pci_topology_snapshots(id,project_id,asset_id,machine_id,version,model_version_id,precollect_id,g1_id,scope_hash,scope,origin,raw_sha256,snapshot,evaluation,verdict,valid_until,reason,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,now())`, id, project, asset, scope.MachineID, revision, scope.ModelVersionID, scope.PrecollectID, scope.G1ID, scope.ScopeHash, networkJSON(scope), original.Origin, scope.PrecollectSHA256, networkJSON(original.Topology), networkJSON(evaluation), evaluation.Verdict, scope.ValidUntil, in.Reason, a.User.ID)
		if e != nil {
			return nil, e
		}
		for i, d := range evaluation.Diffs {
			if d.Category == "" {
				continue
			}
			var issue any
			if d.Verdict == "FAIL" {
				issue, e = topologyIssueTx(ctx, tx, a, scope, original.Origin, id, d, trace)
				if e != nil {
					return nil, e
				}
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO topology_diffs(id,snapshot_id,ordinal,category,detail,issue_id) VALUES($1,$2,$3,$4,$5,$6)`, RandomID(), id, i, d.Category, networkJSON(d), issue); e != nil {
				return nil, e
			}
		}
		if e = appendAudit(ctx, tx, a.User.ID, project, "topology.validate", id, trace, map[string]any{"scope_hash": scope.ScopeHash, "g1_id": scope.G1ID, "precollect_id": scope.PrecollectID, "verdict": evaluation.Verdict, "reason": in.Reason}); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := s.topologyDetailTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func topologyIssueTx(ctx context.Context, tx *sql.Tx, a *AuthSession, scope *TopologyScope, origin, snapshot string, d pcitopology.Diff, trace string) (string, error) {
	findingKey := digest(d.Category + ":" + d.Target)
	var id string
	e := tx.QueryRowContext(ctx, `SELECT issue_id FROM topology_issue_bindings WHERE project_id=$1 AND asset_id=$2 AND model_version_id=$3 AND origin=$4 AND finding_key=$5`, scope.ProjectID, scope.AssetID, scope.ModelVersionID, origin, findingKey).Scan(&id)
	if e == nil {
		item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return "", e
		}
		if item.State == "closed" {
			item, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state='reopened',version=version+1,updated_at=now() WHERE id=$1 RETURNING `+issueColumns, id))
			if e != nil {
				return "", e
			}
			if e = issueEventTx(ctx, tx, a, item, "auto_reopen", trace, map[string]any{"snapshot_id": snapshot, "finding": d}); e != nil {
				return "", e
			}
		}
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	anchors := []IssueAnchor{{Kind: "machine", ID: scope.MachineID}}
	for _, b := range scope.Bindings {
		if b.Key == d.Target {
			anchors = append(anchors, IssueAnchor{Kind: "component", ID: b.ComponentID})
		}
	}
	item, e := insertIssueTx(ctx, tx, a, scope.ProjectID, CreateIssueInput{Title: "T7 " + d.Category + ": " + d.Target, Category: "T7", Source: "topology", Phase: "delivery", Anchors: anchors, Evidence: IssueEvidence{ObservedAt: time.Now().UTC(), SourceVersion: scope.ModelVersionID, Expected: string(networkJSON(d.Expected)), Actual: string(networkJSON(d.Actual))}}, "topology", trace, map[string]any{"snapshot_id": snapshot, "precollect_id": scope.PrecollectID, "g1_id": scope.G1ID, "origin": origin, "finding": d})
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO topology_issue_bindings(issue_id,project_id,asset_id,machine_id,model_version_id,origin,finding_key) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.ID, scope.ProjectID, scope.AssetID, scope.MachineID, scope.ModelVersionID, origin, findingKey); e != nil {
		return "", e
	}
	return item.ID, nil
}

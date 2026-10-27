package platform

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkTemplateRollbackInput struct {
	ExpectedHeadID   string `json:"expected_head_id"`
	ExpectedHeadHash string `json:"expected_head_hash"`
	Reason           string `json:"reason"`
}

func latestNetworkTemplateTx(ctx context.Context, tx *sql.Tx, project, name string) (*NetworkTemplateVersion, error) {
	v, e := scanNetworkTemplate(tx.QueryRowContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE project_id=$1 AND name=$2 ORDER BY version DESC LIMIT 1", project, name))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	return v, e
}
func loadNetworkTemplateTx(ctx context.Context, tx *sql.Tx, project, id string) (*NetworkTemplateVersion, error) {
	v, e := scanNetworkTemplate(tx.QueryRowContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE project_id=$1 AND id=$2", project, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, e
}

// The caller holds beginControl's mutation lock, so imports and rollback draft
// creation share a serialized family version allocation and a reviewed head.
func insertNetworkTemplateTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, input NetworkTemplateInput, head *NetworkTemplateVersion, rollback string) (*NetworkTemplateVersion, error) {
	v := &NetworkTemplateVersion{ID: RandomID(), ProjectID: project, Name: input.Name, Version: 1, State: "draft", SourceRef: input.SourceRef, Definition: input.Definition, ContentHash: netbaseline.ContentHash(input.Definition), CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), RollbackFromID: rollback, ChangeReason: input.ChangeReason}
	if head != nil {
		v.Version = head.Version + 1
		v.ParentVersionID = head.ID
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO network_template_versions(id,project_id,name,version,state,source_ref,definition,content_hash,created_by,created_at,updated_at,parent_version_id,rollback_from_id,change_reason) VALUES($1,$2,$3,$4,'draft',$5,$6,$7,$8,$9,$9,nullif($10,''),nullif($11,''),$12)`, v.ID, project, v.Name, v.Version, v.SourceRef, networkJSON(v.Definition), v.ContentHash, v.CreatedBy, v.CreatedAt, v.ParentVersionID, v.RollbackFromID, v.ChangeReason)
	return v, e
}
func (s *Store) RollbackNetworkTemplate(ctx context.Context, a *AuthSession, p *ProjectSummary, id string, input NetworkTemplateRollbackInput, trace string) (*NetworkTemplateVersion, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(input.ExpectedHeadID) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(input.ExpectedHeadHash) || input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		return nil, apiError(400, "NETWORK_TEMPLATE_ROLLBACK_INVALID", "Review the current family head and provide a rollback reason")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:template:edit"); e != nil {
		return nil, e
	}
	target, e := loadNetworkTemplateTx(ctx, tx, p.ID, id)
	if e != nil {
		return nil, e
	}
	head, e := latestNetworkTemplateTx(ctx, tx, p.ID, target.Name)
	if e != nil {
		return nil, e
	}
	if head == nil || head.ID != input.ExpectedHeadID || head.ContentHash != input.ExpectedHeadHash {
		return nil, apiError(409, "NETWORK_TEMPLATE_HEAD_CHANGED", "The template family changed; review the current head")
	}
	if (target.State != "released" && target.State != "deprecated") || target.Version >= head.Version {
		return nil, apiError(409, "NETWORK_TEMPLATE_ROLLBACK_TARGET_INVALID", "Select a historical released or deprecated version in this template family")
	}
	v, e := insertNetworkTemplateTx(ctx, tx, a, p.ID, NetworkTemplateInput{Name: target.Name, SourceRef: target.SourceRef, Definition: target.Definition, ChangeReason: input.Reason}, head, target.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.template.rollback", v.ID, trace, map[string]any{"parent_version_id": head.ID, "rollback_from_id": target.ID, "content_hash": v.ContentHash, "reason": input.Reason, "state": "draft"}); e != nil {
		return nil, e
	}
	return v, tx.Commit()
}

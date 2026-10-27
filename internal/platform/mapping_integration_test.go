package platform

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Isolated SQL/control-plane evidence only, not a hardware or real-G2 PASS.
func TestMappingAbsentEvidenceCannotPublishOrApprovePostgres(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated disposable database absent")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing non-disposable database")
	}
	ctx := context.Background()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMigrations(ctx); e != nil {
		t.Fatal(e)
	}
	s.WiringEnabled = true
	suffix := RandomID()
	username := "mapping-" + suffix
	var existing bool
	if e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&existing); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateUser(ctx, username, username, "mapping-fixture-password-2026", true, !existing); e != nil {
		t.Fatal(e)
	}
	pre, _, e := s.NewPreSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, actor, e := s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: username, Password: "mapping-fixture-password-2026", Method: "local"}, "198.51.100.181", "mapping-fixture")
	if e != nil {
		t.Fatal(e)
	}
	project, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Mapping fixture", Slug: "mapping-" + suffix}, suffix, "mapping-fixture")
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Other mapping fixture", Slug: "mapping-other-" + suffix}, "other-"+suffix, "mapping-fixture")
	if e != nil {
		t.Fatal(e)
	}
	machine, asset := RandomID(), RandomID()
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,$2,now(),now())`, machine, "MAPPING-"+suffix); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, asset, project.ID, machine, "mapping-host"); e != nil {
		t.Fatal(e)
	}
	preview, e := s.PreviewMapping(ctx, actor, project.ID, asset)
	if e != nil {
		t.Fatal(e)
	}
	if preview.Ready || preview.Verdict != "BLOCKED" || preview.BaseScopeHash != preview.ScopeHash {
		t.Fatal("missing G2/topology passed")
	}
	in := MappingPublishInput{ScopeHash: preview.ScopeHash, Reason: "record missing evidence"}
	result, e := s.PublishMapping(ctx, actor, project.ID, asset, in, "mapping-missing", "mapping-fixture")
	if e != nil {
		t.Fatal(e)
	}
	if result.Revision != nil {
		t.Fatal("missing physical evidence published G3")
	}
	again, e := s.PublishMapping(ctx, actor, project.ID, asset, in, "mapping-missing", "mapping-fixture")
	if e != nil || again.AssessmentID != result.AssessmentID {
		t.Fatal("assessment idempotency failed", e)
	}
	in.Reason = "different request"
	if _, e = s.PublishMapping(ctx, actor, project.ID, asset, in, "mapping-missing", "mapping-fixture"); e == nil {
		t.Fatal("idempotency accepted changed request")
	}
	if _, e = s.RequestMappingCorrection(ctx, actor, project.ID, asset, MappingCorrectionInput{ScopeHash: preview.ScopeHash, Reason: "cannot invent physical port", Changes: []MappingNameChange{{RandomID(), "nic-imagined"}}}, "mapping-correction", "mapping-fixture"); e == nil {
		t.Fatal("logical correction manufactured an unknown physical port")
	}
	if _, e = s.DecideMappingCorrection(ctx, actor, project.ID, asset, RandomID(), MappingCorrectionDecisionInput{Approve: true, Reason: "no proposal exists"}, "mapping-decision", "mapping-fixture"); e == nil {
		t.Fatal("approved nonexistent correction")
	}
	if _, e = s.GetMappingAssessment(ctx, actor, other.ID, asset, result.AssessmentID); e == nil {
		t.Fatal("cross-project assessment exposed")
	}
	history, e := s.ListMappingHistory(ctx, actor, project.ID, asset, Pagination{Page: 1, PageSize: 10})
	if e != nil || history.Total != 1 {
		t.Fatal("assessment history invalid", e)
	}
	fleet, e := s.MappingFleet(ctx, actor, project.ID, "", Pagination{Page: 1, PageSize: 10})
	if e != nil || fleet.Total != 1 || fleet.Items[0].Revision != nil || fleet.Items[0].State != "unbuilt" {
		t.Fatal("unbuilt asset missing or falsely mapped", e)
	}
	rails, e := s.MappingFleet(ctx, actor, project.ID, "r1", Pagination{Page: 1, PageSize: 10})
	if e != nil || rails.Total != 0 {
		t.Fatal("rail view invented published bindings", e)
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE mapping_assessments SET reason='rewrite history' WHERE id=$1`, result.AssessmentID); e == nil {
		t.Fatal("assessment was mutable")
	}
	var revisions int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM mapping_revisions WHERE asset_id=$1`, asset).Scan(&revisions); e != nil || revisions != 0 {
		t.Fatal("blocked assessment created mapping revision", e)
	}
}

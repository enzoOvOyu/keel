package platform

import (
	"context"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"os"
	"testing"
)

func TestZTPMockKeaDurableStageCommit(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-ztp-kea-password"
	if _, err := s.CreateUser(ctx, "ztp-kea-admin", "ZTP Kea Admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "ztp-kea-admin", password, "198.51.100.95")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Kea mock", Slug: "kea-mock"}, "kea-mock-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	media, err := s.RegisterMedia(ctx, actor, project, RegisterMediaInput{Type: "ztp_script", Name: "Isolation", Version: "isolation-v1", Sha256: fabricboot.IsolationScriptDigest(), Size: int64(len(fabricboot.IsolationScriptV1)), Arch: "noarch", OsScope: "cumulus-linux-5.x", ManifestRef: "generated:isolation-v1"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := fabricboot.DHCPOptions{BatchRef: "rack-a", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=rack-a&stage=N-S4&policy=%s", project.ID, media.ID), Option143URIs: []string{"https://boot.example.invalid"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
	req := fabricboot.DORARequest{BatchRef: "rack-a", StageRef: "N-S4", Interface: "eth0", LinkIP: "192.0.2.1", VendorClass: "cumulus-linux x86_64"}
	first, err := s.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || first.Outcome != "no_offer" {
		t.Fatalf("offer before staging: %+v %v", first, err)
	}
	plan, err := s.StageZTPMockKeaPlan(ctx, actor, project, cfg, "test")
	if err != nil || plan.StagedHash == "" || plan.ActiveHash != "" || plan.Version != 1 {
		t.Fatalf("staging: %+v %v", plan, err)
	}
	stagedOnly, err := s.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || stagedOnly.Outcome != "no_offer" {
		t.Fatalf("staging activated options: %+v %v", stagedOnly, err)
	}
	if _, err = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: cfg.BatchRef, StageRef: cfg.StageRef, StagedHash: "wrong", ExpectedVersion: plan.Version}, "test"); err == nil {
		t.Fatal("wrong hash committed")
	}
	if _, err = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: cfg.BatchRef, StageRef: cfg.StageRef, StagedHash: plan.StagedHash, ExpectedVersion: plan.Version}, "test"); err == nil {
		t.Fatal("unapproved script activated")
	} else {
		assertErrorCode(t, err, "ZTP_SCRIPT_NOT_APPROVED")
	}
	if _, err = s.TransitionMedia(ctx, actor, project, media.ID, "verified", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateUser(ctx, "ztp-kea-reviewer", "ZTP Kea Reviewer", password, true, false); err != nil {
		t.Fatal(err)
	}
	_, reviewer := testLogin(t, s, "ztp-kea-reviewer", password, "198.51.100.94")
	if _, err = s.TransitionMedia(ctx, reviewer, project, media.ID, "approved", "test"); err != nil {
		t.Fatal(err)
	}
	active, err := s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: cfg.BatchRef, StageRef: cfg.StageRef, StagedHash: plan.StagedHash, ExpectedVersion: plan.Version}, "test")
	if err != nil || active.ActiveHash != plan.StagedHash || active.StagedHash != "" || active.Version != 2 {
		t.Fatalf("commit: %+v %v", active, err)
	}
	// A fresh Store instance must see the committed plan, unlike the pure in-memory fixture.
	reopened, err := Open(ctx, os.Getenv("KEEL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	offered, err := reopened.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || offered.Outcome != "ack" || offered.ConfigHash != active.ActiveHash {
		t.Fatalf("durable committed offer: %+v %v", offered, err)
	}
	req.Interface = "swp1"
	frontPanel, err := reopened.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || frontPanel.Outcome != "no_offer" || frontPanel.Option239URL != "" {
		t.Fatalf("front panel got bootstrap options: %+v %v", frontPanel, err)
	}
	req.Interface = "eth0"
	cfg.Option239URL = fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=rack-a&stage=N-S4&policy=%s&rev=2", project.ID, media.ID)
	replacement, err := s.StageZTPMockKeaPlan(ctx, actor, project, cfg, "test")
	if err != nil || replacement.Version != 3 || replacement.ActiveHash != active.ActiveHash {
		t.Fatalf("replacement staging: %+v %v", replacement, err)
	}
	unchanged, err := reopened.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || unchanged.ConfigHash != active.ActiveHash {
		t.Fatalf("candidate changed active DORA: %+v %v", unchanged, err)
	}
	if _, err = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: cfg.BatchRef, StageRef: cfg.StageRef, StagedHash: replacement.StagedHash, ExpectedVersion: 2}, "test"); err == nil {
		t.Fatal("stale version committed")
	}
	otherProject, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Other Kea", Slug: "other-kea"}, "other-kea-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	req.ProjectID = project.ID // path project must override a forged body project ID.
	crossProject, err := reopened.SimulateZTPDORA(ctx, actor, otherProject, req)
	if err != nil || crossProject.Outcome != "no_offer" {
		t.Fatalf("project scope leaked: %+v %v", crossProject, err)
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, media.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	afterRetire, err := reopened.SimulateZTPDORA(ctx, actor, project, req)
	if err != nil || afterRetire.Outcome != "no_offer" || afterRetire.Reason != "bootstrap_artifact_unavailable" {
		t.Fatalf("retired script still advertised: %+v %v", afterRetire, err)
	}
}

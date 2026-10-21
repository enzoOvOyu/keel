package platform

import (
	"context"
	"fmt"
	"testing"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func TestSZTP143ApprovedIsolationAndAmbiguousRoute(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-sztp-password"
	for i, name := range []string{"sztp-uploader", "sztp-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, uploader := testLogin(t, s, "sztp-uploader", password, "198.51.100.120")
	_, reviewer := testLogin(t, s, "sztp-reviewer", password, "198.51.100.121")
	project, err := s.CreateProject(ctx, uploader, CreateProjectInput{Name: "SZTP Test", Slug: "sztp-test"}, "sztp-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	media, err := s.RegisterMedia(ctx, uploader, project, RegisterMediaInput{Type: "ztp_script", Name: "Isolation", Version: "isolation-v1", Sha256: fabricboot.IsolationScriptDigest(), Size: int64(len(fabricboot.IsolationScriptV1)), Arch: "noarch", OsScope: "cumulus-linux-5.x", ManifestRef: "generated:isolation-v1"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	stage := func(batch string) {
		t.Helper()
		cfg := fabricboot.DHCPOptions{BatchRef: batch, StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=%s&stage=N-S4&policy=%s", project.ID, batch, media.ID), Option143URIs: []string{"https://boot.example.invalid"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
		plan, e := s.StageZTPMockKeaPlan(ctx, uploader, project, cfg, "test")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.CommitZTPMockKeaPlan(ctx, uploader, project, ZTPMockKeaCommitInput{BatchRef: batch, StageRef: "N-S4", StagedHash: plan.StagedHash, ExpectedVersion: plan.Version}, "test"); e != nil {
			t.Fatal(e)
		}
	}
	stage("rack-a")
	call := func(ip, host string, signed bool) (*SZTPOnboarding, error) {
		return s.ServeZTP143(ctx, "UNKNOWN-SW-143", ip, host, "test", signed)
	}
	if _, err = call("192.0.2.8", "boot.example.invalid", false); err == nil {
		t.Fatal("unapproved isolation script served")
	}
	if _, err = s.TransitionMedia(ctx, uploader, project, media.ID, "verified", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, media.ID, "approved", "test"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ip, host string
		signed   bool
	}{
		{"198.51.100.5", "boot.example.invalid", false},
		{"192.0.2.8", "other.example.invalid", false},
		{"192.0.2.8", "boot.example.invalid", true},
	} {
		if _, e := call(tc.ip, tc.host, tc.signed); e == nil {
			t.Fatalf("unauthorized SZTP request accepted: %+v", tc)
		}
	}
	served, err := call("192.0.2.8", "boot.example.invalid", false)
	if err != nil || served.IdentityVerdict != "isolated" || served.ProjectID != project.ID || len(served.CMS) == 0 {
		t.Fatalf("approved isolation onboarding: %+v %v", served, err)
	}
	stage("rack-b")
	if _, err = call("192.0.2.8", "boot.example.invalid", false); err == nil {
		t.Fatal("ambiguous active bootstrap route accepted")
	}
	var count int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE action='ztp.boot143.serve' AND project_id=$1`, project.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("SZTP serve audit: %d %v", count, err)
	}
}

package platform

import (
	"context"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"strings"
	"testing"
)

func TestZTP239ApprovedIsolationScript(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-ztp-boot-password"
	for i, name := range []string{"boot-uploader", "boot-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, uploader := testLogin(t, s, "boot-uploader", password, "198.51.100.96")
	_, reviewer := testLogin(t, s, "boot-reviewer", password, "198.51.100.97")
	project, err := s.CreateProject(ctx, uploader, CreateProjectInput{Name: "Boot 239", Slug: "boot-239"}, "boot-239-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	media, err := s.RegisterMedia(ctx, uploader, project, RegisterMediaInput{Type: "ztp_script", Name: "Cumulus isolation", Version: "isolation-v1", Sha256: fabricboot.IsolationScriptDigest(), Size: int64(len(fabricboot.IsolationScriptV1)), Arch: "noarch", OsScope: "cumulus-linux-5.x", ManifestRef: "generated:isolation-v1"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf("project=%s&batch=rack-a&stage=N-S4&policy=%s", project.ID, media.ID)
	cfg := fabricboot.DHCPOptions{BatchRef: "rack-a", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: "https://boot.example.invalid/api/v1/boot/ztp?" + query, Option143URIs: []string{"https://boot.example.invalid/sztp"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
	plan, err := s.StageZTPMockKeaPlan(ctx, uploader, project, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitZTPMockKeaPlan(ctx, uploader, project, ZTPMockKeaCommitInput{BatchRef: cfg.BatchRef, StageRef: cfg.StageRef, StagedHash: plan.StagedHash, ExpectedVersion: plan.Version}, "test"); err != nil {
		t.Fatal(err)
	}
	claim := ZTP239Claim{Serial: "UNKNOWN-SW-1", BaseMAC: "aa:bb:cc:dd:ee:01", MgmtMAC: "aa:bb:cc:dd:ee:02", ProductName: "SN5610", Version: "5.0.0", Build: "5.0.0"}
	serve := func(ip, q string) (*ZTP239Script, error) {
		return s.ServeZTP239(ctx, project.ID, "rack-a", "N-S4", media.ID, ip, "https", "boot.example.invalid", "/api/v1/boot/ztp", q, "test", claim)
	}
	if _, err = serve("192.0.2.8", query); err == nil {
		t.Fatal("unapproved ZTP script served")
	}
	if _, err = s.TransitionMedia(ctx, uploader, project, media.ID, "verified", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionMedia(ctx, uploader, project, media.ID, "approved", "test"); err == nil {
		t.Fatal("uploader self-approved script")
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, media.ID, "approved", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = serve("198.51.100.5", query); err == nil {
		t.Fatal("non-management source received script")
	}
	if _, err = s.ServeZTP239(ctx, project.ID, "rack-a", "N-S4", media.ID, "192.0.2.8", "http", "boot.example.invalid", "/api/v1/boot/ztp", query, "test", claim); err == nil {
		t.Fatal("HTTPS option served over HTTP")
	}
	if _, err = s.ServeZTP239(ctx, project.ID, "rack-a", "N-S4", media.ID, "192.0.2.8", "https", "other.example.invalid", "/api/v1/boot/ztp", query, "test", claim); err == nil {
		t.Fatal("wrong boot host served script")
	}
	if _, err = serve("192.0.2.8", query+"&serial=UNKNOWN-SW-1"); err == nil {
		t.Fatal("identity URL parameter accepted")
	}
	script, err := serve("192.0.2.8", query)
	if err != nil || script.Body != fabricboot.IsolationScriptV1 || script.SHA256 != fabricboot.IsolationScriptDigest() || script.IdentityVerdict != "isolated" {
		t.Fatalf("approved isolation script: %+v %v", script, err)
	}
	if !strings.HasPrefix(script.Body, "#!/bin/bash\n# CUMULUS-AUTOPROVISIONING") || !strings.HasSuffix(script.Body, "\nexit 0\n") {
		t.Fatal("invalid Cumulus script contract")
	}
	var auditCount int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE action='ztp.boot239.serve' AND project_id=$1`, project.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("device fetch audit: %d %v", auditCount, err)
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, media.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = serve("192.0.2.8", query); err == nil {
		t.Fatal("retired script still served")
	}
}

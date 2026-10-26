package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func TestZTP136MockPlanDUIDAndNineStageIsolationPostgres(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	suffix := RandomID()[:8]
	password := "test-only-ztp136-password"
	user, err := s.CreateUser(ctx, "ztp136-"+suffix, "IPv6 fixture", password, true, true)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, user.Username, password, "198.51.100.190")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "IPv6 mock", Slug: "ztp136-" + suffix}, "ztp136-project-"+suffix, "test")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	profile, pkg, sw := RandomID(), RandomID(), RandomID()
	serial := "IPV6-MOCK-" + suffix
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'v1','approved',$3,$4)`, profile, project.ID, actor.User.ID, now)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'ipv6-fixture','bom-import/v1','v1',$3,'design_institute','{}',1,'committed',$4,$5)`, pkg, project.ID, "sha256:"+strings.Repeat("a", 64), actor.User.ID, now)
	exec(`INSERT INTO bom_items(item_uid,category,serial,position,pod,role,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'{"dc":"dc1","rack":"r1","idx":"1"}','p1','leaf','design_bom',$3,$4,$4)`, sw, serial, project.ID, now)
	exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,'ipv6-mock',$3,$3)`, sw, serial, now)
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,'{"values":{"credential_batch_ref":"ipv6-cred-v1"}}',$4)`, pkg, sw, project.ID, now)
	exec(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,'ipv6-cred-v1','switch','poc','secret/test/ipv6','fixture',$2,$3)`, project.ID, pkg, now)
	media, err := s.RegisterMedia(ctx, actor, project, RegisterMediaInput{Type: "ztp_script", Name: "Isolation", Version: "v1", Sha256: fabricboot.IsolationScriptDigest(), Size: int64(len(fabricboot.IsolationScriptV1)), Arch: "noarch", OsScope: "cumulus-linux-5.x", ManifestRef: "generated:isolation-v1"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	attachTestIsolationMedia(t, s, actor, project, media.ID)
	_, err = s.TransitionMedia(ctx, actor, project, media.ID, "verified", "test")
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := s.CreateUser(ctx, "ztp136-reviewer-"+suffix, "Reviewer", password, true, false)
	if err != nil {
		t.Fatal(err)
	}
	_, review := testLogin(t, s, reviewer.Username, password, "198.51.100.191")
	_, err = s.TransitionMedia(ctx, review, project, media.ID, "approved", "test")
	if err != nil {
		t.Fatal(err)
	}
	const duid = "00:03:00:01:02:00:00:00:00:01"
	config := fabricboot.DHCPOptions{BatchRef: "rack-a", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: fmt.Sprintf("https://boot.example/api/v1/boot/ztp?project=%s&batch=rack-a&stage=N-S4&policy=%s", project.ID, media.ID), Option143URIs: []string{"https://boot.example"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}, DHCP6: &fabricboot.DHCP6MockOptions{ManagementCIDR: "2001:db8:1::/64", Bindings: []fabricboot.KeaDHCP6Reservation{{SwitchID: sw, DUID: duid, Address: "2001:db8:1::10", Hostname: "leaf-01"}}}}
	request := fabricboot.DORARequest{BatchRef: "rack-a", StageRef: "N-S4", Interface: "eth0", LinkIP: "2001:db8:1::1", DUID: duid}
	staged, err := s.StageZTPMockKeaPlan(ctx, actor, project, config, "test")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.SimulateZTPDORA(ctx, actor, project, request)
	if err != nil || before.Outcome != "no_offer" {
		t.Fatal("uncommitted offer visible", before, err)
	}
	_, err = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: "rack-a", StageRef: "N-S4", StagedHash: staged.StagedHash, ExpectedVersion: staged.Version}, "test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "rack-a", BootChannel: "136", Identity: ZTPIdentityInput{Serial: serial}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	finished, err := s.RunZTPSimSession(ctx, actor, project, session.ID, ZTPSimRunInput{ExpectedVersion: session.StateVersion, DORA: &request, CredentialVerdict: "reachable"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "managed" || finished.Mode != "sim" || finished.BootChannel != "136" {
		t.Fatal("IPv6 simulated chain not complete", finished)
	}
	has136 := false
	for _, event := range finished.Events {
		if strings.HasPrefix(event.EvidenceRef, "mock-sztp136:") {
			has136 = true
		}
	}
	if !has136 {
		t.Fatal("IPv6 fetch lacks protocol-labelled evidence")
	}
	var observations int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM ztp_device_observations WHERE project_id=$1`, project.ID).Scan(&observations); err != nil || observations != 0 {
		t.Fatal("simulation became device evidence", observations, err)
	}
	config.DHCP6.Bindings[0].SwitchID = RandomID()
	if _, err = s.StageZTPMockKeaPlan(ctx, actor, project, config, "test"); err == nil {
		t.Fatal("foreign BOM target accepted")
	}
}

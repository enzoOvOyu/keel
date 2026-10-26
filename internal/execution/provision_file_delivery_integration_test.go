package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

// Signed-format/live-DB software composition only. A full server HTTP write is
// neither a bootable-image assertion nor proof a physical client received it.
func TestProvisionInstallationFileDeliveryPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("exact_full_response_deduplicated_without_state_or_trust_promotion", func(t *testing.T) {
		f := liveProvisionBoot(t)
		peer := netip.MustParseAddr("192.0.2.2")
		before := f.read(t)
		handle, err := f.authority.AuthorizeFile(ctx, f.scope.Scope, f.installation.GrantID, peer, "rootfs")
		mustAgent(t, err)
		artifact, err := f.authority.OpenFile(ctx, f.scope.Scope, handle, "rootfs")
		mustAgent(t, err)
		artifact.File.Close()
		delivery := provisionboot.FileDelivery{Resource: "rootfs", SHA256: artifact.SHA256, Bytes: artifact.Size, ObjectVersion: artifact.ObjectVersion}
		for _, mode := range []string{"HEAD", "range", "conditional", "complete"} {
			method := http.MethodGet
			if mode == "HEAD" {
				method = http.MethodHead
			}
			req := httptest.NewRequest(method, f.scope.Scope.Origin+"/boot/"+handle.ID+"/rootfs", nil)
			req.RemoteAddr = "192.0.2.2:43210"
			if mode == "range" {
				req.Header.Set("Range", "bytes=0-0")
			}
			if mode == "conditional" {
				req.Header.Set("If-None-Match", `"sha256:`+artifact.SHA256+`"`)
			}
			response := httptest.NewRecorder()
			(&provisionboot.Gateway{Scope: f.scope.Scope, Authority: f.authority}).ServeHTTP(response, req)
			var count int
			mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.file_served'`, f.installation.ID).Scan(&count))
			if mode == "complete" {
				if response.Code != 200 || count != 1 {
					t.Fatal("complete response evidence missing", response.Code, count)
				}
			} else if count != 0 {
				t.Fatal("partial/conditional response created evidence", mode, count)
			}
		}
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				failures <- f.authority.RecordFileServed(ctx, f.scope.Scope, handle, peer, delivery)
			}()
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			mustAgent(t, err)
		}
		var count int
		var raw []byte
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*),min(payload::text)::jsonb FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.file_served'`, f.installation.ID).Scan(&count, &raw))
		var payload map[string]any
		mustAgent(t, json.Unmarshal(raw, &payload))
		if count != 1 || len(payload) != 5 || payload["full_response"] != true || payload["server_write_only"] != true {
			t.Fatal("deduplication or narrow server evidence failed", count, string(raw))
		}
		after := f.read(t)
		if after.StateVersion != before.StateVersion || after.State != before.State {
			t.Fatal("delivery changed installation authority")
		}
		progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
		mustAgent(t, err)
		if progress.Stages[2].Status != "reported" || progress.Stages[4].Status != "pending" || progress.Stages[6].Status != "pending" {
			t.Fatal("server write was promoted to boot or SSH")
		}
		for _, change := range []func(*provisionboot.FileDelivery){
			func(d *provisionboot.FileDelivery) { d.SHA256 = strings.Repeat("0", 64) },
			func(d *provisionboot.FileDelivery) { d.Bytes++ },
			func(d *provisionboot.FileDelivery) { d.ObjectVersion = "other" },
			func(d *provisionboot.FileDelivery) { d.Resource = "../rootfs" },
		} {
			wrong := delivery
			change(&wrong)
			if f.authority.RecordFileServed(ctx, f.scope.Scope, handle, peer, wrong) == nil {
				t.Fatal("wrong frozen delivery identity admitted")
			}
		}
		if f.authority.RecordFileServed(ctx, f.scope.Scope, handle, netip.MustParseAddr("198.51.100.22"), delivery) == nil {
			t.Fatal("unapproved peer admitted")
		}
		_, err = f.store.ChangeProvisionBootScope(ctx, f.actor, f.project, f.scope.ID, "revoke", platform.ProvisionBootScopeActionInput{ExpectedVersion: f.scope.StateVersion, Reason: "withdraw before delivery evidence"}, "test")
		mustAgent(t, err)
		if f.authority.RecordFileServed(ctx, f.scope.Scope, handle, peer, delivery) == nil {
			t.Fatal("revoked authority admitted repeated evidence")
		}
	})
}

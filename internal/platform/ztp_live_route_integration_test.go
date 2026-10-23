package platform

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Called against the same disposable DB and TLS fixture as the browser/worker
// chain, after the worker has produced a real config and DORA receipt.
func TestLive239WorkerRoute(t *testing.T) {
	expected := os.Getenv("KEEL_ROUTE_EXPECT")
	if expected == "" {
		t.Skip("isolated worker route scenario not configured")
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	u, e := url.Parse(dsn)
	if e != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "keel_test_") {
		t.Fatal("disposable database required")
	}
	ctx := context.Background()
	store, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	store.FabricProbes, e = fabricboot.LoadProbeSuite(os.Getenv("KEEL_FABRIC_PROBES_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	job, e := scanKeaDeployment(store.DB.QueryRowContext(ctx, "SELECT "+keaDeploymentFields+" FROM ztp_kea_deployments WHERE state='boot_ready' ORDER BY created_at DESC LIMIT 1"))
	if e != nil {
		t.Fatal(e)
	}
	if len(job.Spec.Bindings) == 0 {
		t.Fatal("missing device bindings")
	}
	binding := job.Spec.Bindings[0]
	var serial string
	if e = store.DB.QueryRowContext(ctx, "SELECT serial_number FROM switches WHERE id=$1", binding.SwitchID).Scan(&serial); e != nil {
		t.Fatal(e)
	}
	var baseMAC string
	if e = store.DB.QueryRowContext(ctx, "SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='base'", binding.SwitchID).Scan(&baseMAC); e != nil {
		t.Fatal(e)
	}
	boot, _ := url.Parse(job.Spec.Options.Option239URL)
	claim := ZTP239Claim{Serial: serial, MgmtMAC: binding.MAC, BaseMAC: baseMAC, ProductName: "fixture", Version: "5.9", Build: "fixture"}
	call := func(source string) (*ZTP239Script, error) {
		return store.ServeZTP239(ctx, job.ProjectID, job.Spec.Options.BatchRef, "N-S4", boot.Query().Get("policy"), source, "https", boot.Host, boot.Path, boot.RawQuery, "live-route-test", claim)
	}
	if _, e = call("198.51.100.22"); e == nil {
		t.Fatal("foreign source admitted")
	}
	out, e := call(binding.IP)
	if expected == "ready" {
		if e != nil || out == nil || out.IdentityVerdict != "matched" {
			t.Fatalf("live route did not return matched identity: error=%v", e)
		}
	} else if e == nil {
		t.Fatalf("%s route incorrectly admitted", expected)
	}
}

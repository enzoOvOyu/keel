package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

func TestProvisionBootAuthorityExactCurrentNetworkBinding(t *testing.T) {
	scope := provisionboot.Scope{ID: strings.Repeat("1", 32), ProjectID: strings.Repeat("2", 32), Interface: "boot0", Server: netip.MustParseAddr("192.0.2.1"), Clients: netip.MustParsePrefix("192.0.2.0/24"), Origin: "https://boot.example.test", KeaNetworks: []netip.Prefix{}}
	raw, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	current := &ProvisionBootScope{ID: scope.ID, ProjectID: scope.ProjectID, Scope: scope, ScopeSHA256: digest(string(raw))}
	peer := &provisionCallbackPeer{scope: scope, peer: netip.MustParseAddr("192.0.2.2")}
	if err = checkProvisionCallbackPeer(peer, current); err != nil {
		t.Fatal(err)
	}
	changed := *peer
	changed.scope.Origin = "https://other.example.test"
	if checkProvisionCallbackPeer(&changed, current) == nil {
		t.Fatal("listener scope differs from current approval")
	}
	changed = *peer
	changed.peer = netip.MustParseAddr("198.51.100.2")
	if checkProvisionCallbackPeer(&changed, current) == nil {
		t.Fatal("outside peer admitted")
	}
	if checkProvisionCallbackPeer(peer, nil) == nil {
		t.Fatal("missing current scope admitted")
	}
}

type provisionCountingObjects struct {
	MediaObjectStore
	opens int
}

func (s *provisionCountingObjects) Open(ctx context.Context, key, version string) (io.ReadCloser, error) {
	s.opens++
	return s.MediaObjectStore.Open(ctx, key, version)
}

func TestProvisionBootDetachedHashUsesLiveGuardAndFinalGate(t *testing.T) {
	ctx := context.Background()
	raw := []byte("small pinned artifact still needs post-hash current authority")
	objects := &provisionCountingObjects{MediaObjectStore: &testMediaObjects{data: map[string][]byte{"fixed:v1": raw}}}
	s := &Store{MediaObjects: objects}
	m := &MediaItemSummary{ID: RandomID(), Sha256: digest(string(raw)), Size: int64(len(raw))}
	checks, finished := 0, 0
	check := func() error { checks++; return nil }
	finish := func() error { finished++; return nil }
	stage, err := s.stagePinnedMedia(ctx, m, "fixed", "v1", m.Sha256, m.Size, check, finish)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.close()
	if checks != 1 || finished != 1 || objects.opens != 1 {
		t.Fatal("missing guard/final gate or unnecessary reads", checks, finished, objects.opens)
	}
	denied := errors.New("current lease revoked")
	_, err = s.stagePinnedMedia(ctx, m, "fixed", "v1", m.Sha256, m.Size, func() error { return denied }, finish)
	if !errors.Is(err, denied) || objects.opens != 2 {
		t.Fatal("revocation retried or transformed into artifact corruption", err, objects.opens)
	}
	_, err = s.stagePinnedMedia(ctx, m, "fixed", "v1", m.Sha256, m.Size, check, func() error { return denied })
	if !errors.Is(err, denied) || objects.opens != 3 {
		t.Fatal("authority changed after hashing was ignored", err, objects.opens)
	}
	got, err := io.ReadAll(stage.file)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("pinned staged bytes changed", err)
	}
}

func TestProvisionBootAuthorityUnavailableHasNoPermissiveDefault(t *testing.T) {
	a := &ProvisionBootAuthority{}
	if _, err := a.LookupBoot(context.Background(), provisionboot.Scope{}, provisionboot.Client{}); err == nil {
		t.Fatal("empty boot authority allowed")
	}
	if _, err := a.AuthorizeFile(context.Background(), provisionboot.Scope{}, "", netip.Addr{}, "rootfs"); err == nil {
		t.Fatal("empty file authority allowed")
	}
	if _, err := a.EnrollBoot(context.Background(), provisionboot.Scope{}, "", netip.Addr{}, "", provisionboot.EnrollmentSubmission{}); err == nil {
		t.Fatal("empty callback authority allowed")
	}
}

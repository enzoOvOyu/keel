package platform

import (
	"bytes"
	"context"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"io"
	"os"
	"sync"
	"testing"
)

// Only the object-store transport is mocked here; DB, auth and lifecycle are real.
type testMediaObjects struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *testMediaObjects) Put(ctx context.Context, key string, r io.Reader, size int64) (string, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key+":v1"] = b
	return "v1", nil
}
func (s *testMediaObjects) Open(ctx context.Context, key, version string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[key+":"+version]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte{}, b...))), nil
}
func attachTestIsolationMedia(t *testing.T, s *Store, a *AuthSession, p *ProjectSummary, id string) {
	t.Helper()
	if s.MediaObjects == nil {
		s.MediaObjects = &testMediaObjects{}
	}
	if _, e := s.AttachMedia(context.Background(), a, p, id, bytes.NewBufferString(fabricboot.IsolationScriptV1), "test"); e != nil {
		t.Fatal(e)
	}
}
func TestMediaArtifactsLifecycleAndIntegrity(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	const password = "test-only-media-private-password"
	for i, name := range []string{"media-uploader", "media-reviewer"} {
		if _, e := s.CreateUser(ctx, name, name, password, true, i == 0); e != nil {
			t.Fatal(e)
		}
	}
	_, uploader := testLogin(t, s, "media-uploader", password, "198.51.100.120")
	_, reviewer := testLogin(t, s, "media-reviewer", password, "198.51.100.121")
	p, e := s.CreateProject(ctx, uploader, CreateProjectInput{Name: "Media integrity", Slug: "media-integrity"}, "media-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	input := RegisterMediaInput{Type: "tool", Name: "test-package", Version: "v1", Arch: "noarch"}
	m, e := s.IngestMedia(ctx, uploader, p, input, bytes.NewBufferString("trusted bytes"), "test")
	if e != nil || !m.ArtifactStored {
		t.Fatalf("ingest %+v %v", m, e)
	}
	if _, e = s.TransitionMedia(ctx, uploader, p, m.ID, "verified", "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TransitionMedia(ctx, uploader, p, m.ID, "approved", "test"); e == nil {
		t.Fatal("self approval admitted")
	}
	// Corrupt storage under the pinned version, without changing registered metadata.
	objects.mu.Lock()
	for key := range objects.data {
		objects.data[key] = []byte("tampered bytes")
	}
	objects.mu.Unlock()
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "approved", "test"); e == nil {
		t.Fatal("tampered verified artifact approved")
	} else {
		assertErrorCode(t, e, "MEDIA_DIGEST_MISMATCH")
	}
	objects.mu.Lock()
	for key := range objects.data {
		objects.data[key] = []byte("trusted bytes")
	}
	objects.mu.Unlock()
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "approved", "test"); e == nil {
		t.Fatal("restored quarantined source approved")
	} else {
		assertErrorCode(t, e, "MEDIA_ARTIFACT_QUARANTINED")
	}
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
	input.Version = "replacement-v1"
	m, e = s.IngestMedia(ctx, uploader, p, input, bytes.NewBufferString("trusted bytes"), "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.TransitionMedia(ctx, uploader, p, m.ID, "verified", "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "approved", "test"); e != nil {
		t.Fatal(e)
	}
	f, _, e := s.DownloadMedia(ctx, reviewer, p, m.ID, "test")
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(f)
	name := f.Name()
	f.Close()
	os.Remove(name)
	if e != nil || string(body) != "trusted bytes" {
		t.Fatalf("read %q %v", body, e)
	}
	if _, e = s.DB.ExecContext(ctx, "UPDATE media_artifacts SET object_version='replacement' WHERE media_id=$1", m.ID); e == nil {
		t.Fatal("immutable binding changed")
	}
	objects.mu.Lock()
	for key := range objects.data {
		objects.data[key] = []byte("tampered bytes")
	}
	objects.mu.Unlock()
	if f, _, e = s.DownloadMedia(ctx, reviewer, p, m.ID, "test"); e == nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatal("corrupted approved file served")
	} else {
		assertErrorCode(t, e, "MEDIA_DIGEST_MISMATCH")
	}
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.DownloadMedia(ctx, reviewer, p, m.ID, "test"); e == nil {
		t.Fatal("retired media distributed")
	}
	input.Version = "v2"
	input.Sha256 = digest("trusted bytes")
	input.Size = 13
	input.ManifestRef = "untrusted:nonempty"
	bare, e := s.RegisterMedia(ctx, uploader, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.TransitionMedia(ctx, uploader, p, bare.ID, "verified", "test"); e == nil {
		t.Fatal("manifest string passed as byte evidence")
	} else {
		assertErrorCode(t, e, "MEDIA_ARTIFACT_MISSING")
	}
	if _, e = s.AttachMedia(ctx, reviewer, p, bare.ID, bytes.NewBufferString("trusted bytes"), "test"); e == nil {
		t.Fatal("reviewer replaced uploader")
	}
	if _, e = s.AttachMedia(ctx, uploader, p, bare.ID, bytes.NewBufferString("wrong"), "test"); e == nil {
		t.Fatal("wrong digest attached")
	}
	if _, e = s.AttachMedia(ctx, uploader, p, bare.ID, bytes.NewBufferString("trusted bytes"), "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AttachMedia(ctx, uploader, p, bare.ID, bytes.NewBufferString("trusted bytes"), "test"); e == nil {
		t.Fatal("artifact replaced")
	}
}

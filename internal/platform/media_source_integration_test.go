package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/mediasource"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMediaSourceFetch(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	s.MediaObjects = &testMediaObjects{}
	const password = "source-integration-password"
	for i, name := range []string{"source-owner", "source-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "source-owner", password, "198.51.100.135")
	_, reviewer := testLogin(t, s, "source-reviewer", password, "198.51.100.136")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Source project", Slug: "source-project"}, "source-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	payload := []byte("trusted source fixture bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/corrupt":
			w.Write([]byte("wrong source bytes"))
		default:
			w.Write(payload)
		}
	}))
	defer server.Close()
	fetcher, err := mediasource.New(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	register := func(name, path string) *MediaItemSummary {
		t.Helper()
		m, e := s.RegisterMedia(ctx, owner, project, RegisterMediaInput{Type: "tool", Name: name, Version: "v1", Arch: "noarch", Sha256: hex.EncodeToString(digest[:]), Size: int64(len(payload)), ManifestRef: server.URL + path}, "test")
		if e != nil {
			t.Fatal(e)
		}
		return m
	}
	m := register("source-positive", "/artifact")
	_, err = s.FetchMediaSource(ctx, owner, project, m.ID, "test-disabled")
	assertErrorCode(t, err, "MEDIA_SOURCE_UNAVAILABLE")
	if hits.Load() != 0 {
		t.Fatal("disabled source reached network")
	}
	s.MediaSources = fetcher
	_, err = s.FetchMediaSource(ctx, reviewer, project, m.ID, "test-not-uploader")
	assertErrorCode(t, err, "MEDIA_UPLOADER_REQUIRED")
	if hits.Load() != 0 {
		t.Fatal("non-uploader reached network")
	}
	fetched, err := s.FetchMediaSource(ctx, owner, project, m.ID, "test-fetch")
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.ArtifactStored || fetched.State != "uploaded" || fetched.ArtifactQuarantined || hits.Load() != 1 {
		t.Fatalf("fetch changed approval or missing bytes: %+v", fetched)
	}
	_, err = s.FetchMediaSource(ctx, owner, project, m.ID, "test-duplicate")
	assertErrorCode(t, err, "MEDIA_ARTIFACT_IMMUTABLE")
	if hits.Load() != 1 {
		t.Fatal("bound source fetched twice")
	}
	var uploads, fetches int
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='media.upload' AND target_id=$1", m.ID).Scan(&uploads); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='media.source.fetch' AND target_id=$1", m.ID).Scan(&fetches); err != nil || uploads != 1 || fetches != 1 {
		t.Fatal("source pinning audit missing")
	}
	for _, state := range []string{"verified", "approved"} {
		if _, err = s.TransitionMedia(ctx, reviewer, project, m.ID, state, "test"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, path, code string
		expected         int32
	}{{"corrupt", "/corrupt", "MEDIA_DIGEST_MISMATCH", 3}, {"missing", "/missing", "MEDIA_SOURCE_UNAVAILABLE", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			item := register(tc.name, tc.path)
			before := hits.Load()
			_, e := s.FetchMediaSource(ctx, owner, project, item.ID, "test-negative")
			assertErrorCode(t, e, tc.code)
			if hits.Load()-before != tc.expected {
				t.Fatal("unbounded external source retries")
			}
			var bound, quarantine int
			if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM media_artifacts WHERE media_id=$1", item.ID).Scan(&bound); e != nil {
				t.Fatal(e)
			}
			if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM media_artifact_integrity WHERE media_id=$1", item.ID).Scan(&quarantine); e != nil || bound != 0 || quarantine != 0 {
				t.Fatal("untrusted source created immutable byte evidence")
			}
		})
	}
	// URLs entering immutable metadata cannot carry credentials or bearer queries.
	for i, source := range []string{"http://user:secret@mirror.example/file", "https://mirror.example/file?token=secret", "https://mirror.example/file#fragment", "HTTPS://user:secret@mirror.example/file"} {
		_, err = s.RegisterMedia(ctx, owner, project, RegisterMediaInput{Type: "tool", Name: fmt.Sprintf("bad-url-%d", i), Version: "v1", Arch: "noarch", Sha256: hex.EncodeToString(digest[:]), Size: int64(len(payload)), ManifestRef: source}, "test")
		assertErrorCode(t, err, "VALIDATION_ERROR")
	}
	blocked := register("outside-policy", "/artifact")
	otherFetcher, err := mediasource.New("https://other.example", false)
	if err != nil {
		t.Fatal(err)
	}
	s.MediaSources = otherFetcher
	before := hits.Load()
	_, err = s.FetchMediaSource(ctx, owner, project, blocked.ID, "test-origin")
	assertErrorCode(t, err, "MEDIA_SOURCE_REJECTED")
	if hits.Load() != before {
		t.Fatal("rejected origin reached network")
	}
	if strings.Contains(fmt.Sprint(err), server.URL) {
		t.Fatal("fetch error leaked source internals")
	}
}

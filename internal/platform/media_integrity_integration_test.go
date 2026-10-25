package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedMediaReads struct {
	base    *testMediaObjects
	mu      sync.Mutex
	results []string
	opens   int
}

func (s *scriptedMediaReads) Put(ctx context.Context, key string, r io.Reader, size int64) (string, error) {
	return s.base.Put(ctx, key, r, size)
}
func (s *scriptedMediaReads) Open(ctx context.Context, key, version string) (io.ReadCloser, error) {
	s.mu.Lock()
	index := s.opens
	s.opens++
	mode := "ok"
	if index < len(s.results) {
		mode = s.results[index]
	}
	s.mu.Unlock()
	switch mode {
	case "transport":
		return nil, errors.New("test-only transport unavailable")
	case "corrupt":
		return io.NopCloser(strings.NewReader("corrupt object payload")), nil
	case "read-error":
		return io.NopCloser(failingMediaReader{}), nil
	}
	return s.base.Open(ctx, key, version)
}

type failingMediaReader struct{}

func (failingMediaReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestMediaIntegrityRetriesAndQuarantine(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	const password = "integrity-test-password"
	for i, name := range []string{"integrity-owner", "integrity-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "integrity-owner", password, "198.51.100.133")
	_, reviewer := testLogin(t, s, "integrity-reviewer", password, "198.51.100.134")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Integrity project", Slug: "integrity-project"}, "integrity-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(name string, approve bool, deps ...*MediaItemSummary) *MediaItemSummary {
		t.Helper()
		s.MediaObjects = objects
		refs := []MediaArtifactDependency{}
		for _, dep := range deps {
			refs = append(refs, MediaArtifactDependency{MediaID: dep.ID, SHA256: dep.Sha256})
		}
		item, e := s.IngestMedia(ctx, owner, project, RegisterMediaInput{Type: "tool", Name: name, Version: "v1", Arch: "noarch", Deps: map[string]any{"artifacts": refs}}, bytes.NewBufferString(name+" trusted bytes"), "test")
		if e != nil {
			t.Fatal(e)
		}
		if approve {
			for _, state := range []string{"verified", "approved"} {
				if _, e = s.TransitionMedia(ctx, reviewer, project, item.ID, state, "test"); e != nil {
					t.Fatal(e)
				}
			}
		}
		return item
	}
	quarantine := func(id string) bool {
		t.Helper()
		var q bool
		if e := s.DB.QueryRowContext(ctx, "SELECT quarantined_at IS NOT NULL FROM media_artifact_integrity WHERE media_id=$1", id).Scan(&q); e != nil {
			t.Fatal(e)
		}
		return q
	}
	download := func(item *MediaItemSummary, code string) {
		t.Helper()
		file, _, e := s.DownloadMedia(ctx, owner, project, item.ID, "integrity-download")
		if file != nil {
			file.Close()
			os.Remove(file.Name())
		}
		if code != "" {
			assertErrorCode(t, e, code)
		} else if e != nil {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct {
		name    string
		results []string
		code    string
		opens   int
	}{
		{"transport-recovers", []string{"transport", "transport", "ok"}, "", 3},
		{"digest-recovers", []string{"corrupt", "corrupt", "ok"}, "", 3},
		{"transport-bounded", []string{"transport", "transport", "transport", "ok"}, "MEDIA_OBJECT_UNAVAILABLE", 3},
		{"read-bounded", []string{"read-error", "read-error", "read-error", "ok"}, "MEDIA_READ_FAILED", 3},
		{"mixed-failures", []string{"corrupt", "transport", "corrupt"}, "MEDIA_DIGEST_MISMATCH", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := upload(tc.name, true)
			reader := &scriptedMediaReads{base: objects, results: tc.results}
			s.MediaObjects = reader
			download(item, tc.code)
			if reader.opens != tc.opens {
				t.Fatalf("unbounded read attempts: %d", reader.opens)
			}
			if quarantine(item.ID) {
				t.Fatal("transient/incomplete observations quarantined source")
			}
			s.MediaObjects = objects
			download(item, "")
		})
	}
	item := upload("permanently-corrupt", true)
	reader := &scriptedMediaReads{base: objects, results: []string{"corrupt", "corrupt", "corrupt"}}
	s.MediaObjects = reader
	download(item, "MEDIA_DIGEST_MISMATCH")
	if reader.opens != 3 || !quarantine(item.ID) {
		t.Fatal("failure did not persist after rollback")
	}
	var state string
	var audits, downloads int
	if err = s.DB.QueryRowContext(ctx, "SELECT state FROM media_items WHERE id=$1", item.ID).Scan(&state); err != nil || state != "approved" {
		t.Fatalf("approval lifecycle rewritten: %s %v", state, err)
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='media.quarantined' AND target_id=$1", item.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("quarantine audit %d %v", audits, err)
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='media.download' AND target_id=$1", item.ID).Scan(&downloads); err != nil || downloads != 0 {
		t.Fatal("failed distribution wrote success audit")
	}
	for _, query := range []string{"UPDATE media_artifact_integrity SET quarantined_at=NULL,reason=NULL,observed_sha256=NULL,observed_size=NULL,mismatch_count=0 WHERE media_id=$1", "DELETE FROM media_artifact_integrity WHERE media_id=$1"} {
		if _, err = s.DB.ExecContext(ctx, query, item.ID); err == nil {
			t.Fatal("sticky quarantine evidence was cleared")
		}
	}
	download(item, "MEDIA_ARTIFACT_QUARANTINED")
	if reader.opens != 3 {
		t.Fatal("quarantined object was retried")
	}
	s.MediaObjects = objects
	download(item, "MEDIA_ARTIFACT_QUARANTINED")
	listed, err := s.ListMedia(ctx, owner, project, Pagination{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range listed.Items {
		if m.ID == item.ID {
			found = m.ArtifactQuarantined
		}
	}
	if !found {
		t.Fatal("quarantine hidden from scoped media list")
	}
	ready, err := s.ListMediaFiltered(ctx, owner, project, Pagination{Page: 1, PageSize: 100}, MediaListFilter{State: "approved", StoredOnly: true, CompatibleArch: "noarch"})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range ready.Items {
		if candidate.ID == item.ID || candidate.ArtifactQuarantined || !candidate.ArtifactStored {
			t.Fatal("stored source filter offered a quarantined or absent object")
		}
	}

	evidence, evidenceErr := s.GetMediaQuarantineEvidence(ctx, owner, project, item.ID)
	if evidenceErr != nil || evidence.ExpectedSHA256 != item.Sha256 || evidence.ExpectedSize != item.Size || evidence.ObservedSHA256 == item.Sha256 || evidence.MismatchCount != 3 || evidence.QuarantinedAt.IsZero() || evidence.ObjectVersion == "" || evidence.Reason != "digest_mismatch" {
		t.Fatal("missing immutable quarantine observation", evidenceErr)
	}
	// Evidence remains readable without any configured object-storage connection.
	s.MediaObjects = nil
	repeated, evidenceErr := s.GetMediaQuarantineEvidence(ctx, owner, project, item.ID)
	if evidenceErr != nil || *repeated != *evidence {
		t.Fatal("evidence required a fresh storage read or changed")
	}
	_, evidenceErr = s.GetMediaQuarantineEvidence(ctx, owner, nil, item.ID)
	assertErrorCode(t, evidenceErr, "NOT_FOUND")
	s.MediaObjects = objects
	if _, err = s.TransitionMedia(ctx, reviewer, project, item.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	// A failed verification also records integrity outside its rolled-back state change.
	pending := upload("pending-corrupt", false)
	s.MediaObjects = &scriptedMediaReads{base: objects, results: []string{"corrupt", "corrupt", "corrupt"}}
	_, err = s.TransitionMedia(ctx, reviewer, project, pending.ID, "verified", "integrity-verify")
	assertErrorCode(t, err, "MEDIA_DIGEST_MISMATCH")
	if !quarantine(pending.ID) {
		t.Fatal("verification rollback discarded quarantine")
	}
	// Corruption of a transitive source quarantines that source, not its healthy parent.
	leaf := upload("transitive-leaf", true)
	middle := upload("transitive-middle", true, leaf)
	parent := upload("transitive-parent", true, middle)
	var key, version string
	if err = s.DB.QueryRowContext(ctx, "SELECT object_key,object_version FROM media_artifacts WHERE media_id=$1", leaf.ID).Scan(&key, &version); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	saved := append([]byte{}, objects.data[key+":"+version]...)
	objects.data[key+":"+version] = []byte("corrupt transitive")
	objects.mu.Unlock()
	s.MediaObjects = objects
	download(parent, "MEDIA_DIGEST_MISMATCH")
	if !quarantine(leaf.ID) || quarantine(parent.ID) || quarantine(middle.ID) {
		t.Fatal("quarantined the wrong dependency")
	}
	objects.mu.Lock()
	objects.data[key+":"+version] = saved
	objects.mu.Unlock()
	download(parent, "MEDIA_DEPENDENCY_QUARANTINED")
	_, err = s.RegisterMedia(ctx, owner, project, RegisterMediaInput{Type: "tool", Name: "blocked-new-ref", Version: "v1", Arch: "noarch", Sha256: parent.Sha256, Size: parent.Size, Deps: map[string]any{"artifacts": []MediaArtifactDependency{{MediaID: leaf.ID, SHA256: leaf.Sha256}}}}, "test")
	assertErrorCode(t, err, "MEDIA_DEPENDENCY_QUARANTINED")
	// Context cancellation never retries or claims source corruption.
	cancelItem := upload("canceled", true)
	reader = &scriptedMediaReads{base: objects, results: []string{"transport", "ok"}}
	s.MediaObjects = reader
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, _, err = s.DownloadMedia(short, owner, project, cancelItem.ID, "test-cancel")
	if err == nil || reader.opens > 1 || quarantine(cancelItem.ID) {
		t.Fatalf("canceled read retried/quarantined: opens=%d error=%v", reader.opens, err)
	}
	// A successful verifier holds the integrity gate through its enclosing
	// admission commit; a later corruption observation waits before quarantining.
	concurrent := upload("concurrent-admission", true)
	admission, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer admission.Rollback()
	if e = s.VerifyCatalogArtifactsTx(ctx, admission, []CatalogSelection{{Media: &CatalogMedia{ID: concurrent.ID, SHA256: concurrent.Sha256, ObjectVersion: "v1"}}}); e != nil {
		t.Fatal(e)
	}
	observed := make(chan struct{})
	recorded := make(chan error, 1)
	go func() {
		readTx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			recorded <- e
			return
		}
		metadata, e := mediaDependencyRecord(ctx, readTx, concurrent.ID)
		if e != nil {
			readTx.Rollback()
			recorded <- e
			return
		}
		corruptReader := &Store{MediaObjects: &scriptedMediaReads{base: objects, results: []string{"corrupt", "corrupt", "corrupt"}}}
		_, e = corruptReader.checkedMediaFile(ctx, readTx, &metadata)
		readTx.Rollback()
		close(observed)
		recorded <- s.RecordMediaIntegrityFailure(ctx, e, owner.User.ID, "test-concurrent-quarantine")
	}()
	select {
	case <-observed:
	case e := <-recorded:
		t.Fatalf("read failed before observation: %v", e)
	case <-time.After(3 * time.Second):
		t.Fatal("corruption observation timed out")
	}
	select {
	case e := <-recorded:
		t.Fatalf("quarantine bypassed admission lock: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	if quarantine(concurrent.ID) {
		t.Fatal("quarantine committed before admission released its gate")
	}
	admission.Rollback()
	select {
	case e := <-recorded:
		assertErrorCode(t, e, "MEDIA_DIGEST_MISMATCH")
	case <-time.After(3 * time.Second):
		t.Fatal("quarantine blocked after admission release")
	}
	if !quarantine(concurrent.ID) {
		t.Fatal("quarantine missing after concurrent gate release")
	}
	// Distinct immutable registration is the recovery path, never an automatic unquarantine.
	replacement := upload(fmt.Sprintf("new-reviewed-source-%s", item.ID[:8]), true)
	download(replacement, "")
}

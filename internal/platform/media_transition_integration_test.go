package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMediaConditionalTransitions(t *testing.T) {
	s := integrationStore(t)
	s.MediaObjects = &testMediaObjects{} // Object transport only; transactions and authorization use real PostgreSQL.
	ctx := context.Background()
	const password = "isolated-media-transition-password"
	for i, name := range []string{"transition-owner", "transition-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "transition-owner", password, "198.51.100.140")
	_, reviewer := testLogin(t, s, "transition-reviewer", password, "198.51.100.141")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Media transitions", Slug: "media-transitions"}, "media-transitions", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []*ProjectSummary{project, nil} {
		t.Run(s.scopeKeyFor(scope), func(t *testing.T) {
			payload := []byte("immutable transition evidence")
			original, err := s.RegisterMedia(ctx, owner, scope, RegisterMediaInput{Type: "tool", Name: "transition-tool", Version: "v1", Arch: "noarch", Sha256: fmt.Sprintf("%x", sha256.Sum256(payload)), Size: int64(len(payload))}, "test")
			if err != nil {
				t.Fatal(err)
			}
			zero := time.Time{}
			if _, err = s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "retired", ExpectedUpdatedAt: &zero}, "test"); err == nil {
				t.Fatal("zero precondition accepted")
			} else {
				assertErrorCode(t, err, "VALIDATION_ERROR")
			}
			attached, err := s.AttachMedia(ctx, owner, scope, original.ID, bytes.NewReader(payload), "test")
			if err != nil || !attached.UpdatedAt.After(original.UpdatedAt) {
				t.Fatalf("attachment must advance version: %+v %v", attached, err)
			}
			var persisted time.Time
			if err = s.DB.QueryRowContext(ctx, "SELECT updated_at FROM media_items WHERE id=$1", original.ID).Scan(&persisted); err != nil || !persisted.Equal(attached.UpdatedAt) {
				t.Fatalf("attachment response not persisted: %v %v", persisted, err)
			}
			if _, err = s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "retired", ExpectedUpdatedAt: &original.UpdatedAt}, "stale-attachment"); err == nil {
				t.Fatal("confirmation predating file attachment succeeded")
			} else {
				assertErrorCode(t, err, "MEDIA_CONFLICT")
			}
			type result struct {
				item *MediaItemSummary
				err  error
			}
			results := make(chan result, 2)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					item, e := s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "verified", ExpectedUpdatedAt: &attached.UpdatedAt}, "concurrent-verify")
					results <- result{item, e}
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			var verified *MediaItemSummary
			conflicts := 0
			for r := range results {
				if r.err == nil {
					if verified != nil {
						t.Fatal("two conditional transitions committed")
					}
					verified = r.item
				} else {
					assertErrorCode(t, r.err, "MEDIA_CONFLICT")
					conflicts++
				}
			}
			if verified == nil || conflicts != 1 || !verified.UpdatedAt.After(attached.UpdatedAt) || !verified.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("conditional verify: %+v conflicts=%d", verified, conflicts)
			}
			if _, err = s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "approved", ExpectedUpdatedAt: &verified.UpdatedAt}, "self-approve"); err == nil {
				t.Fatal("uploader approved own media")
			} else {
				assertErrorCode(t, err, "MEDIA_SELF_APPROVAL")
			}
			if _, err = s.TransitionMediaChecked(ctx, reviewer, scope, original.ID, MediaTransitionInput{State: "approved", ExpectedUpdatedAt: &attached.UpdatedAt}, "stale-approve"); err == nil {
				t.Fatal("stale approval succeeded")
			} else {
				assertErrorCode(t, err, "MEDIA_CONFLICT")
			}
			approved, err := s.TransitionMediaChecked(ctx, reviewer, scope, original.ID, MediaTransitionInput{State: "approved", ExpectedUpdatedAt: &verified.UpdatedAt}, "fresh-approve")
			if err != nil || approved.ApprovedBy != reviewer.User.ID || !approved.UpdatedAt.After(verified.UpdatedAt) {
				t.Fatalf("fresh approval: %+v %v", approved, err)
			}
			if _, err = s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "retired", ExpectedUpdatedAt: &verified.UpdatedAt}, "stale-retire"); err == nil {
				t.Fatal("stale retirement succeeded")
			} else {
				assertErrorCode(t, err, "MEDIA_CONFLICT")
			}
			if err = s.DB.QueryRowContext(ctx, "SELECT updated_at FROM media_items WHERE id=$1", original.ID).Scan(&persisted); err != nil || !persisted.Equal(approved.UpdatedAt) {
				t.Fatalf("failed request changed version or response mismatches persistence: %v %v", persisted, err)
			}
			retired, err := s.TransitionMediaChecked(ctx, owner, scope, original.ID, MediaTransitionInput{State: "retired", ExpectedUpdatedAt: &approved.UpdatedAt}, "fresh-retire")
			if err != nil || retired.State != "retired" || !retired.UpdatedAt.After(approved.UpdatedAt) || !retired.CreatedAt.Equal(original.CreatedAt) || retired.Sha256 != original.Sha256 {
				t.Fatalf("fresh retirement: %+v %v", retired, err)
			}
			if _, err = s.TransitionMedia(ctx, reviewer, scope, original.ID, "approved", "legacy-no-resurrection"); err == nil {
				t.Fatal("legacy caller resurrected retired media")
			} else {
				assertErrorCode(t, err, "MEDIA_TRANSITION_INVALID")
			}
			var audits int
			if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE target_id=$1 AND action IN('media.verified','media.approved','media.retired')`, original.ID).Scan(&audits); err != nil || audits != 3 {
				t.Fatalf("failed transitions added success audits: %d %v", audits, err)
			}
		})
	}
}

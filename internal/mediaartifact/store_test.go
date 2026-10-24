package mediaartifact

import (
	"context"
	"github.com/minio/minio-go/v7"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRejectUnsafeMediaConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://store.example.invalid", "https://user:secret@store.example.invalid", "https://store.example.invalid/path", "https://store.example.invalid?token=secret"} {
		if _, e := New(endpoint, "access", "secret", "media", false); e == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}
func TestRealMinIOVersionPinning(t *testing.T) {
	endpoint := os.Getenv("KEEL_TEST_MEDIA_ENDPOINT")
	if endpoint == "" {
		t.Skip("isolated MinIO not configured")
	}
	s, e := New(endpoint, os.Getenv("KEEL_MEDIA_ACCESS_KEY"), os.Getenv("KEEL_MEDIA_SECRET_KEY"), "media", true)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = s.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	first, e := s.Put(ctx, "version-pinning", strings.NewReader("original"), 8)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Put(ctx, "version-pinning", strings.NewReader("changed"), 7)
	if e != nil || first == second {
		t.Fatal("version did not change", e)
	}
	body, e := s.Open(ctx, "version-pinning", first)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(body)
	body.Close()
	if e != nil || string(raw) != "original" {
		t.Fatalf("pinned version lost: %q %v", raw, e)
	}
	if e = s.client.RemoveObject(ctx, s.bucket, "version-pinning", minio.RemoveObjectOptions{VersionID: first}); e != nil {
		t.Fatal(e)
	}
	body, e = s.Open(ctx, "version-pinning", first)
	if e == nil {
		_, e = io.ReadAll(body)
		body.Close()
	}
	if e == nil {
		t.Fatal("missing pinned version fell back to latest")
	}
	if e = s.client.SetBucketVersioning(ctx, s.bucket, minio.BucketVersioningConfiguration{Status: "Suspended"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Put(ctx, "unversioned", strings.NewReader("x"), 1); e == nil {
		t.Fatal("unversioned bucket accepted")
	}
	if e = s.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
}

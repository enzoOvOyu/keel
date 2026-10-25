// Package mediaartifact stores private, version-pinned media bytes in MinIO.
package mediaartifact

import (
	"context"
	"errors"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/url"
	"os"
	"strings"
)

type Store struct {
	client *minio.Client
	bucket string
}

// Load is optional only when all storage configuration is absent. Partial or
// unsafe configuration fails startup; credentials never enter error messages.
func Load() (*Store, error) {
	endpoint, access, secret := os.Getenv("KEEL_MEDIA_ENDPOINT"), os.Getenv("KEEL_MEDIA_ACCESS_KEY"), os.Getenv("KEEL_MEDIA_SECRET_KEY")
	if endpoint == "" && access == "" && secret == "" {
		return nil, nil
	}
	return New(endpoint, access, secret, os.Getenv("KEEL_MEDIA_BUCKET"), os.Getenv("KEEL_ENV") == "development")
}
func New(endpoint, access, secret, bucket string, development bool) (*Store, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(development && u.Scheme == "http")) || access == "" || secret == "" {
		return nil, errors.New("invalid media storage configuration; HTTPS and credentials required outside development")
	}
	if bucket == "" {
		bucket = "media"
	}
	if len(bucket) < 3 || len(bucket) > 63 || strings.ContainsAny(bucket, "/\\:") {
		return nil, errors.New("invalid media bucket")
	}
	c, e := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: u.Scheme == "https", Region: "us-east-1", MaxRetries: 1})
	if e != nil {
		return nil, errors.New("invalid media storage client configuration")
	}
	return &Store{client: c, bucket: bucket}, nil
}

// Initialize is an explicit operator action; the API never provisions buckets.
func (s *Store) Initialize(ctx context.Context) error {
	exists, e := s.client.BucketExists(ctx, s.bucket)
	if e != nil {
		return errors.New("media bucket unavailable")
	}
	if !exists {
		if e = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); e != nil {
			return errors.New("media bucket creation failed")
		}
	}
	if e = s.client.SetBucketVersioning(ctx, s.bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); e != nil {
		return errors.New("media bucket versioning failed")
	}
	return nil
}
func (s *Store) Put(ctx context.Context, key string, body io.Reader, size int64) (string, error) {
	cfg, e := s.client.GetBucketVersioning(ctx, s.bucket)
	if e != nil || !cfg.Enabled() {
		return "", errors.New("media bucket must have versioning enabled")
	}
	info, e := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: "application/octet-stream", SendContentMd5: true})
	if e != nil || info.VersionID == "" || info.VersionID == "null" {
		return "", errors.New("versioned media upload failed")
	}
	return info.VersionID, nil
}
func (s *Store) Open(ctx context.Context, key, version string) (io.ReadCloser, error) {
	if version == "" || version == "null" {
		return nil, errors.New("media object version is required")
	}
	object, e := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{VersionID: version})
	if e != nil {
		return nil, errors.New("media object unavailable")
	}
	stat, e := object.Stat()
	if e != nil || stat.VersionID != version {
		object.Close()
		return nil, errors.New("pinned media object unavailable")
	}
	return object, nil
}

package bmc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type testRawSink struct {
	objects map[string][]byte
	fail    bool
	version string
}

func (s *testRawSink) StoreRaw(_ context.Context, target Target, path string, body []byte) (RawReference, error) {
	if s.fail {
		return RawReference{}, errors.New("unavailable")
	}
	h := sha256.Sum256(body)
	digest := hex.EncodeToString(h[:])
	key := RawKey(target, path, digest)
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = append([]byte(nil), body...)
	version := s.version
	if version == "" {
		version = "version-1"
	}
	return RawReference{Key: key, Version: version, SHA256: digest, Size: int64(len(body))}, nil
}
func testTarget() Target {
	return Target{RequestID: strings.Repeat("1", 32), ProjectID: strings.Repeat("2", 32), AssetID: strings.Repeat("3", 32), MachineID: strings.Repeat("4", 32), AssetVersion: 1, Serial: "SN-001", CredentialBatchRef: "batch-1", CredentialVersion: "v1"}
}
func TestSimCollectorPreservesIdentityOriginAndEvidence(t *testing.T) {
	target := testTarget()
	reader, e := NewSimReader(target)
	if e != nil {
		t.Fatal(e)
	}
	sink := &testRawSink{}
	c := Collector{Reader: reader, Raw: sink, Config: DefaultConfig()}
	out, e := c.Collect(context.Background(), target, "L1")
	if e != nil {
		t.Fatal(e)
	}
	if e = out.Validate(); e != nil {
		t.Fatal(e)
	}
	if out.State != "completed" || !out.IdentityMatched || out.Origin != "sim" || len(out.Resources) != 5 {
		t.Fatalf("unexpected sim evidence: %#v", out)
	}
	for _, resource := range out.Resources {
		if _, ok := sink.objects[resource.Raw.Key]; !ok {
			t.Fatal("resource lacks retained raw")
		}
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), `"Health":"OK"`) || strings.Contains(string(raw), `"PowerState":"On"`) {
		t.Fatal("identity fixture invented healthy hardware")
	}
	out, e = c.Collect(context.Background(), target, "L3")
	if e != nil {
		t.Fatal(e)
	}
	if out.State != "partial" || out.Capabilities["Processors"] != "unknown" || out.Validate() != nil {
		t.Fatalf("missing hardware became completed: %#v", out)
	}
}
func TestCollectorPagingDeduplicatesAndRetainsPartialFailures(t *testing.T) {
	target := testTarget()
	reader, _ := NewSimReader(target)
	reader.documents["/redfish/v1/Systems"] = []byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/sim"}],"Members@odata.nextLink":"/redfish/v1/Systems?$skip=1"}`)
	reader.documents["/redfish/v1/Systems?$skip=1"] = []byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/sim"}],"Members@odata.nextLink":"/redfish/v1/Systems"}`)
	reader.failures["/redfish/v1/Chassis"] = Failure{Class: "timeout", Code: "request_timeout", Retryable: true}
	out, e := (&Collector{Reader: reader, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil {
		t.Fatal(e)
	}
	if !out.IdentityMatched || out.State != "partial" || out.Capabilities["Chassis"] != "partial" || len(out.Resources) != 5 {
		t.Fatalf("unexpected paged collection: %#v", out)
	}
	if e = out.Validate(); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, r := range out.Resources {
		if seen[r.Path] {
			t.Fatal("duplicate resource")
		}
		seen[r.Path] = true
	}
}
func TestCollectorFailsClosedOnLimitsStorageAndAuthentication(t *testing.T) {
	for _, kind := range []string{"limit", "storage", "null-version", "authentication", "unsupported-version", "identity"} {
		t.Run(kind, func(t *testing.T) {
			target := testTarget()
			reader, _ := NewSimReader(target)
			sink := &testRawSink{}
			cfg := DefaultConfig()
			switch kind {
			case "limit":
				cfg.MaxResources = 2
			case "storage":
				sink.fail = true
			case "null-version":
				sink.version = "null"
			case "authentication":
				reader.failures["/redfish/v1/"] = Failure{Class: "authentication_failed", Code: "credential_expired", Retryable: true}
			case "unsupported-version":
				reader.documents["/redfish/v1/"] = []byte(`{"RedfishVersion":"0.9.0"}`)
			case "identity":
				target.Serial = "OTHER"
			}
			out, e := (&Collector{Reader: reader, Raw: sink, Config: cfg}).Collect(context.Background(), target, "L1")
			if e != nil {
				t.Fatal(e)
			}
			if out.State == "completed" || len(out.Failures) == 0 {
				t.Fatalf("failure became complete: %#v", out)
			}
			if e = out.Validate(); e != nil {
				t.Fatal(e)
			}
			if kind == "authentication" && len(sink.objects) > 0 {
				t.Fatal("authentication failed after storing resources")
			}
		})
	}
}
func TestSnapshotRejectsTamperingAndNonDurableEvidence(t *testing.T) {
	target := testTarget()
	reader, _ := NewSimReader(target)
	out, e := (&Collector{Reader: reader, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil {
		t.Fatal(e)
	}
	original := out.Resources[0]
	out.Resources[0].Data = json.RawMessage(`{"tampered":true}`)
	if out.Validate() == nil {
		t.Fatal("accepted changed structured data")
	}
	out.Resources[0] = original
	out.Resources[0].Raw.Version = "null"
	if out.Validate() == nil {
		t.Fatal("accepted unversioned raw")
	}
	out.Resources[0] = original
	out.Resources[0].Raw.Key = "other-project/raw"
	if out.Validate() == nil {
		t.Fatal("accepted foreign raw reference")
	}
}
func TestCanonicalDigestPreservesRawVersusStructuredIdentity(t *testing.T) {
	if CanonicalDigest([]byte(` {"b":2,"a":1} `)) != CanonicalDigest([]byte(`{"a":1,"b":2}`)) {
		t.Fatal("JSONB normalization changed identity")
	}
	if CanonicalDigest([]byte(`{} {}`)) != "" {
		t.Fatal("accepted trailing JSON")
	}
}

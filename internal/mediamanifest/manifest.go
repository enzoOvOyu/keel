// Package mediamanifest signs platform-approved artifact metadata. A platform
// signature is neither a vendor source signature nor a live authorization grant.
package mediamanifest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"io"
	"net/url"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"
)

const Schema = "keel.media-manifest/v1"
const MaxBytes = 16 << 20
const MaxArtifactBytes int64 = 1 << 30
const signingDomain = "Keel platform media manifest v1\x00"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Dependency struct {
	MediaID string `json:"media_id"`
	SHA256  string `json:"sha256"`
}
type Artifact struct {
	OSBoot        *mediaboot.Profile `json:"os_boot,omitempty"`
	MediaID       string             `json:"media_id"`
	ScopeKey      string             `json:"scope_key"`
	URI           string             `json:"uri"`
	Type          string             `json:"type"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Arch          string             `json:"arch"`
	SHA256        string             `json:"sha256"`
	Size          int64              `json:"size"`
	ObjectVersion string             `json:"object_version"`
	ApprovedBy    string             `json:"approved_by"`
	ApprovedAt    time.Time          `json:"approved_at"`
	Dependencies  []Dependency       `json:"dependencies"`
	Compatibility json.RawMessage    `json:"compatibility,omitempty"`
}
type Payload struct {
	Schema     string     `json:"schema"`
	KeyID      string     `json:"key_id"`
	RootID     string     `json:"root_id"`
	ExportedAt time.Time  `json:"exported_at"`
	Artifacts  []Artifact `json:"artifacts"`
}
type Envelope struct {
	Algorithm string  `json:"algorithm"`
	Payload   Payload `json:"payload"`
	Signature string  `json:"signature"`
}
type Signer struct{ private ed25519.PrivateKey }

func keyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}
func (s *Signer) KeyID() string { return keyID(s.private.Public().(ed25519.PublicKey)) }
func (s *Signer) Sign(payload Payload) (*Envelope, error) {
	if s == nil || len(s.private) != ed25519.PrivateKeySize {
		return nil, errors.New("manifest signing is not configured")
	}
	payload.Schema = Schema
	payload.KeyID = s.KeyID()
	if err := validate(payload); err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil || len(data) > MaxBytes {
		return nil, errors.New("manifest payload exceeds its encoding limit")
	}
	if err = validateManifestJSONKeys(data, "payload"); err != nil {
		return nil, err
	}
	signature := ed25519.Sign(s.private, append([]byte(signingDomain), data...))
	return &Envelope{Algorithm: "ed25519", Payload: payload, Signature: base64.StdEncoding.EncodeToString(signature)}, nil
}

// Verify uses an independently provisioned trusted key. The manifest does not
// provide its own trust root, and historical signatures do not bypass live gates.
func Verify(raw []byte, trusted ed25519.PublicKey) (*Envelope, error) {
	if len(raw) > MaxBytes || len(trusted) != ed25519.PublicKeySize {
		return nil, errors.New("invalid manifest or trusted public key")
	}
	if err := uniqueJSONKeys(raw); err != nil {
		return nil, err
	}
	var out Envelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, errors.New("invalid manifest document")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("only one manifest document is allowed")
	}
	if out.Algorithm != "ed25519" || out.Payload.Schema != Schema || out.Payload.KeyID != keyID(trusted) {
		return nil, errors.New("manifest schema, algorithm or trusted key does not match")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(out.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid manifest signature")
	}
	data, err := json.Marshal(out.Payload)
	if err != nil || !ed25519.Verify(trusted, append([]byte(signingDomain), data...), signature) {
		return nil, errors.New("manifest signature verification failed")
	}
	if err = validate(out.Payload); err != nil {
		return nil, err
	}
	return &out, nil
}
func validate(payload Payload) error {
	if payload.Schema != Schema || !digestPattern.MatchString(payload.KeyID) || !idPattern.MatchString(payload.RootID) || payload.ExportedAt.IsZero() || len(payload.Artifacts) == 0 || len(payload.Artifacts) > 129 {
		return errors.New("invalid manifest identity or artifact count")
	}
	nodes := map[string]Artifact{}
	previous := ""
	for _, node := range payload.Artifacts {
		scopeOK := node.ScopeKey == "global" || (len(node.ScopeKey) == 40 && node.ScopeKey[:8] == "project:" && idPattern.MatchString(node.ScopeKey[8:]))
		if !idPattern.MatchString(node.MediaID) || node.MediaID <= previous || !scopeOK || !digestPattern.MatchString(node.SHA256) || node.Size < 0 || node.Size > MaxArtifactBytes || node.ObjectVersion == "" || node.ObjectVersion == "null" || !idPattern.MatchString(node.ApprovedBy) || node.ApprovedAt.IsZero() || node.Name == "" || node.Version == "" || !typePattern.MatchString(node.Type) || utf8.RuneCountInString(node.Name) > 120 || utf8.RuneCountInString(node.Version) > 120 || len(node.ObjectVersion) > 4096 || (node.Arch != "amd64" && node.Arch != "arm64" && node.Arch != "noarch") || len(node.Dependencies) > 32 || node.Dependencies == nil {
			return errors.New("invalid, duplicate or unsorted manifest artifact")
		}
		uri := "media://" + node.Type + "/" + url.PathEscape(node.Name) + "/" + url.PathEscape(node.Version)
		if node.URI != uri || len(node.Compatibility) > 64<<10 || (len(node.Compatibility) > 0 && (!json.Valid(node.Compatibility) || node.Compatibility[0] != '{')) {
			return errors.New("invalid manifest URI or compatibility metadata")
		}
		previous = node.MediaID
		nodes[node.MediaID] = node
	}
	visited := map[string]bool{}
	active := map[string]bool{}
	heights := map[string]int{}
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if depth > 16 || depth+heights[id] > 16 {
			return errors.New("manifest dependency depth exceeds 16")
		}
		if active[id] {
			return errors.New("manifest dependency cycle")
		}
		if visited[id] {
			return nil
		}
		node, ok := nodes[id]
		if !ok {
			return errors.New("manifest has a missing dependency")
		}
		active[id] = true
		seen := map[string]bool{}
		lastID := ""
		for _, edge := range node.Dependencies {
			child, exists := nodes[edge.MediaID]
			if !exists || seen[edge.MediaID] || edge.MediaID <= lastID || child.SHA256 != edge.SHA256 || (child.ScopeKey != "global" && child.ScopeKey != node.ScopeKey) || (child.Arch != "noarch" && child.Arch != node.Arch) {
				return errors.New("invalid manifest dependency identity, scope or architecture")
			}
			lastID = edge.MediaID
			seen[edge.MediaID] = true
			if err := visit(edge.MediaID, depth+1); err != nil {
				return err
			}
			if height := heights[edge.MediaID] + 1; height > heights[id] {
				heights[id] = height
			}
		}

		direct := make(map[string]mediaboot.Node, len(node.Dependencies))
		for _, edge := range node.Dependencies {
			child := nodes[edge.MediaID]
			direct[child.MediaID] = mediaboot.Node{ID: child.MediaID, Type: child.Type, Arch: child.Arch}
		}
		if err := node.OSBoot.ValidateRoles(mediaboot.Node{ID: node.MediaID, Type: node.Type, Arch: node.Arch}, direct); err != nil {
			return err
		}
		delete(active, id)
		visited[id] = true
		return nil
	}
	if err := visit(payload.RootID, 0); err != nil {
		return err
	}
	if len(visited) != len(nodes) {
		return errors.New("manifest contains unreachable artifacts")
	}
	return nil
}
func Sort(artifacts []Artifact) {
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].MediaID < artifacts[j].MediaID })
	for i := range artifacts {
		if artifacts[i].Dependencies == nil {
			artifacts[i].Dependencies = []Dependency{}
		}
		sort.Slice(artifacts[i].Dependencies, func(a, b int) bool {
			return artifacts[i].Dependencies[a].MediaID < artifacts[i].Dependencies[b].MediaID
		})
	}
}
func identityError(id string) error {
	return fmt.Errorf("artifact bytes do not match manifest identity %s", id)
}

// Exact schema field names avoid encoding/json's case-insensitive struct aliases.
// Arbitrary compatibility metadata keys remain case-sensitive and signed as given.
var manifestJSONFields = map[string]map[string]string{
	"envelope":   {"algorithm": "", "payload": "payload", "signature": ""},
	"payload":    {"schema": "", "key_id": "", "root_id": "", "exported_at": "", "artifacts": "artifact"},
	"artifact":   {"media_id": "", "scope_key": "", "uri": "", "type": "", "name": "", "version": "", "arch": "", "sha256": "", "size": "", "object_version": "", "approved_by": "", "approved_at": "", "dependencies": "dependency", "compatibility": "", "os_boot": "os_boot"},
	"os_boot":    {"schema": "", "installer": "", "kernel_media_id": "", "initrd_media_id": "", "template_media_id": ""},
	"dependency": {"media_id": "", "sha256": ""},
}

// Reject duplicate or aliased schema keys before typed decoding. Every verifier
// must see the same object values before signature validation.
func uniqueJSONKeys(raw []byte) error { return validateManifestJSONKeys(raw, "envelope") }

func validateManifestJSONKeys(raw []byte, rootShape string) error {
	if !utf8.Valid(raw) {
		return errors.New("manifest JSON must be valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int, string) error
	value = func(depth int, shape string) error {
		if depth > 64 {
			return errors.New("manifest JSON nesting exceeds its limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return errors.New("invalid manifest JSON")
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return errors.New("invalid manifest JSON")
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("invalid manifest JSON key")
				}
				if seen[name] {
					return errors.New("duplicate manifest JSON key")
				}
				childShape := ""
				if fields, strict := manifestJSONFields[shape]; strict {
					var allowed bool
					childShape, allowed = fields[name]
					if !allowed {
						return errors.New("unknown or noncanonical manifest field name")
					}
				}
				seen[name] = true
				if err = value(depth+1, childShape); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err = value(depth+1, shape); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid manifest JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0, rootShape); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("only one manifest JSON document is allowed")
	}
	return nil
}

// Package auditevidence seals a canonical hash index for independent custody.
// Audit payloads and signing keys are never included in the exported evidence.
package auditevidence

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"hash"
	"io"
	"regexp"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
)

const Schema = "keel.audit-evidence/v1"
const MaxRows = 1_000_000
const MaxIndexBytes = 128 << 20
const signingDomain = "Keel audit evidence v1\x00"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Entry struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}
type Header struct {
	Schema      string    `json:"schema"`
	SnapshotID  string    `json:"snapshot_id"`
	KeyID       string    `json:"key_id"`
	ExportedAt  time.Time `json:"exported_at"`
	Rows        int       `json:"rows"`
	IndexBytes  int64     `json:"index_bytes"`
	IndexSHA256 string    `json:"index_sha256"`
}
type Envelope struct {
	Algorithm string `json:"algorithm"`
	Payload   Header `json:"payload"`
	Signature string `json:"signature"`
}
type Signer struct{ private ed25519.PrivateKey }

func (s *Signer) Close() {
	if s != nil {
		clear(s.private)
		s.private = nil
	}
}
func GenerateKeyFiles(private, public string) (string, error) {
	return mediamanifest.GenerateKeyFiles(private, public)
}
func LoadPublicKey(path string) (ed25519.PublicKey, error) { return mediamanifest.LoadPublicKey(path) }
func LoadSigner(path string) (*Signer, error) {
	raw, err := agentpki.ReadFile(path, 8192, true)
	if err != nil {
		return nil, errors.New("protected audit signing key unavailable")
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid audit signing key")
	}
	defer clear(block.Bytes)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid audit signing key")
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("audit signing requires Ed25519")
	}
	return &Signer{private: private}, nil
}
func keyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}
func validHeader(h Header) bool {
	return h.Schema == Schema && idPattern.MatchString(h.SnapshotID) && hashPattern.MatchString(h.KeyID) && hashPattern.MatchString(h.IndexSHA256) && !h.ExportedAt.IsZero() && h.Rows >= 0 && h.Rows <= MaxRows && h.IndexBytes >= 0 && h.IndexBytes <= MaxIndexBytes
}

type IndexWriter struct {
	output   io.Writer
	hash     hash.Hash
	rows     int
	bytes    int64
	previous string
}

func NewIndexWriter(output io.Writer) *IndexWriter {
	return &IndexWriter{output: output, hash: sha256.New()}
}
func (w *IndexWriter) Add(entry Entry) error {
	if w == nil || w.output == nil {
		return errors.New("audit index writer unavailable")
	}
	if !idPattern.MatchString(entry.ID) || entry.ID <= w.previous || !hashPattern.MatchString(entry.SHA256) || w.rows >= MaxRows {
		return errors.New("audit index requires bounded unique sorted identities and hashes")
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if w.bytes+int64(len(raw)) > MaxIndexBytes {
		return errors.New("audit index exceeds size limit")
	}
	n, err := w.output.Write(raw)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	w.hash.Write(raw)
	w.bytes += int64(n)
	w.rows++
	w.previous = entry.ID
	return nil
}
func (w *IndexWriter) Seal(s *Signer, snapshot string, at time.Time) (*Envelope, error) {
	if s == nil || len(s.private) != ed25519.PrivateKeySize {
		return nil, errors.New("audit signer unavailable")
	}
	h := Header{Schema: Schema, SnapshotID: snapshot, KeyID: keyID(s.private.Public().(ed25519.PublicKey)), ExportedAt: at.UTC(), Rows: w.rows, IndexBytes: w.bytes, IndexSHA256: hex.EncodeToString(w.hash.Sum(nil))}
	if !validHeader(h) {
		return nil, errors.New("invalid audit evidence header")
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(s.private, append([]byte(signingDomain), raw...))
	return &Envelope{Algorithm: "ed25519", Payload: h, Signature: base64.StdEncoding.EncodeToString(signature)}, nil
}

// Verify requires an independently supplied key and a canonical seal. visit is
// read-only; no result is trusted unless the entire bounded index also verifies.
func Verify(index io.Reader, rawSeal []byte, trusted ed25519.PublicKey, visit func(Entry) error) (*Header, error) {
	if index == nil || len(rawSeal) > 16<<10 || len(trusted) != ed25519.PublicKeySize {
		return nil, errors.New("invalid audit evidence size or trust key")
	}
	var seal Envelope
	decoder := json.NewDecoder(bytes.NewReader(rawSeal))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&seal) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid audit seal")
	}
	canonical, err := json.Marshal(seal)
	if err != nil || !bytes.Equal(bytes.TrimSpace(rawSeal), canonical) || seal.Algorithm != "ed25519" || !validHeader(seal.Payload) || seal.Payload.KeyID != keyID(trusted) {
		return nil, errors.New("noncanonical or untrusted audit seal")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(seal.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid audit signature")
	}
	canonical, err = json.Marshal(seal.Payload)
	if err != nil || !ed25519.Verify(trusted, append([]byte(signingDomain), canonical...), signature) {
		return nil, errors.New("audit evidence signature mismatch")
	}
	limited := &io.LimitedReader{R: index, N: MaxIndexBytes + 1}
	digest := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(limited, digest))
	scanner.Buffer(make([]byte, 256), 512)
	count := 0
	previous := ""
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry Entry
		if json.Unmarshal(line, &entry) != nil || !idPattern.MatchString(entry.ID) || entry.ID <= previous || !hashPattern.MatchString(entry.SHA256) || count >= MaxRows {
			return nil, errors.New("invalid audit index entry")
		}
		exact, e := json.Marshal(entry)
		if e != nil || !bytes.Equal(line, exact) {
			return nil, errors.New("noncanonical audit index entry")
		}
		previous = entry.ID
		count++
		if visit != nil {
			if err = visit(entry); err != nil {
				return nil, err
			}
		}
	}
	if scanner.Err() != nil || limited.N <= 0 || count != seal.Payload.Rows || MaxIndexBytes+1-limited.N != seal.Payload.IndexBytes || hex.EncodeToString(digest.Sum(nil)) != seal.Payload.IndexSHA256 {
		return nil, errors.New("audit index is incomplete or its digest differs")
	}
	return &seal.Payload, nil
}

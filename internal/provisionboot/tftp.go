package provisionboot

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/netip"
	"strings"
	"time"
)

// TFTP is limited to the small approved iPXE bootstrap firmware. OS media,
// user-data and all later traffic must use the authenticated HTTP gateway.
// RFC1350 fixed 512-byte octet blocks avoid extension negotiation ambiguity.
type TFTPTransfer struct {
	scope     Scope
	authority FileAuthority
	grant     Grant
	peer      netip.Addr
	resource  string
	artifact  Artifact
	ctx       context.Context
	block     uint16
	last      []byte
	final     bool
	closed    bool
	guard     *fileGuard
}

func ParseTFTPRequest(packet []byte) (string, string, error) {
	if len(packet) < 12 || len(packet) > 512 || binary.BigEndian.Uint16(packet[:2]) != 1 {
		return "", "", ErrDenied
	}
	// Refuse writes, path aliases, malformed extension lists and non-octet modes.
	parts := bytes.Split(packet[2:], []byte{0})
	if len(parts) < 3 || len(parts) > 11 || len(parts)%2 != 1 || len(parts[len(parts)-1]) != 0 || !strings.EqualFold(string(parts[1]), "octet") {
		return "", "", ErrDenied
	}
	// RFC2347 allows an unnegotiated server to ignore options and send DATA
	// immediately at 512 bytes. Never echo untrusted blksize/tsize/timeout values.
	seen := map[string]bool{}
	for i := 2; i < len(parts)-1; i += 2 {
		key := strings.ToLower(string(parts[i]))
		value := parts[i+1]
		if key == "" || len(key) > 32 || len(value) == 0 || len(value) > 64 || seen[key] {
			return "", "", ErrDenied
		}
		seen[key] = true
		for _, r := range key {
			if r < 'a' || r > 'z' {
				return "", "", ErrDenied
			}
		}
		for _, v := range value {
			if v < 33 || v > 126 {
				return "", "", ErrDenied
			}
		}
	}
	filename := string(parts[0])
	if strings.HasPrefix(filename, "/") {
		return "", "", ErrDenied
	}
	id, resource, err := resourcePath("/" + filename)
	if err != nil || (resource != "ipxe.efi" && resource != "undionly.kpxe") {
		return "", "", ErrDenied
	}
	return id, resource, nil
}
func OpenTFTP(ctx context.Context, scope Scope, authority FileAuthority, peer netip.Addr, request []byte) (*TFTPTransfer, error) {
	if scope.Validate() != nil || authority == nil || !scope.CheckPeer(peer) {
		return nil, ErrDenied
	}
	id, resource, err := ParseTFTPRequest(request)
	if err != nil {
		return nil, err
	}
	grant, err := authority.AuthorizeFile(ctx, scope, id, peer, resource)
	if err != nil || grant.ID != id || grant.Validate(scope, time.Now()) != nil {
		return nil, ErrDenied
	}
	artifact, err := authority.OpenFile(ctx, scope, grant, resource)
	if err != nil {
		if artifact.File != nil {
			artifact.File.Close()
		}
		return nil, ErrDenied
	}
	if artifact.File == nil {
		return nil, ErrDenied
	}
	guard := &fileGuard{ctx: ctx, scope: scope, authority: authority, grant: grant, peer: peer, resource: resource}
	if err = verifyArtifact(ctx, artifact, resource, guard); err != nil {
		artifact.File.Close()
		return nil, err
	}
	return &TFTPTransfer{scope: scope, authority: authority, grant: grant, peer: peer, resource: resource, artifact: artifact, ctx: ctx, guard: guard}, nil
}
func (t *TFTPTransfer) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	return t.artifact.File.Close()
}
func (t *TFTPTransfer) allowed() bool { return !t.closed && t.guard != nil && t.guard.check() }

// Next ignores a duplicate ACK and advances exactly once for a matching ACK.
// Duplicate ACKs must not cause DATA retransmission (RFC1123 section 4.2.3.1). A nil ACK starts at block 1. A nil DATA after the
// final ACK closes the file. Transport callers own bounded timeout/retry policy.
func (t *TFTPTransfer) Next(ack []byte) ([]byte, error) {
	if t == nil || !t.allowed() {
		return nil, ErrDenied
	}
	if t.block == 0 {
		if ack != nil {
			return nil, ErrDenied
		}
	} else {
		if len(ack) != 4 || binary.BigEndian.Uint16(ack[:2]) != 4 {
			return nil, ErrDenied
		}
		block := binary.BigEndian.Uint16(ack[2:])
		if block == t.block-1 {
			return nil, nil
		}
		if block != t.block {
			return nil, ErrDenied
		}
		if t.final {
			t.Close()
			return nil, nil
		}
	}
	if t.block == 65535 {
		return nil, ErrDenied
	}
	t.block++
	packet := make([]byte, 516)
	binary.BigEndian.PutUint16(packet[:2], 3)
	binary.BigEndian.PutUint16(packet[2:4], t.block)
	n, err := io.ReadFull(contextReader{t.ctx, t.artifact.File}, packet[4:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, ErrDenied
	}
	t.final = n < 512
	t.last = packet[:4+n]
	return append([]byte{}, t.last...), nil
}

func (t *TFTPTransfer) Done() bool { return t.closed }

// Retry is driven only by a bounded transport timeout, never a duplicate ACK.
func (t *TFTPTransfer) Retry() ([]byte, error) {
	if t == nil || !t.allowed() || len(t.last) == 0 {
		return nil, ErrDenied
	}
	return append([]byte{}, t.last...), nil
}

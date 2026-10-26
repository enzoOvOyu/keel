package taskrunner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

type TargetConfig struct {
	ProvisionOnly       bool       `json:"provision_only,omitempty"`
	ProjectID           string     `json:"project_id"`
	MachineID           string     `json:"machine_id"`
	Address             string     `json:"address"`
	User                string     `json:"user"`
	HostKey             string     `json:"host_key"`
	CredentialRef       string     `json:"credential_ref"`
	CredentialVersion   string     `json:"credential_version,omitempty"`
	CredentialExpiresAt *time.Time `json:"credential_expires_at,omitempty"`
	PrivateKeyFile      string     `json:"private_key_file"`
	CredentialProvider  string     `json:"credential_provider,omitempty"`
}

// CredentialProvider is the control-plane secret boundary. Implementations must
// resolve exact versions and reject revoked/expired credentials without fallback.
type CredentialProvider interface {
	Resolve(context.Context, string, string, string) (ssh.Signer, time.Time, error)
}

// CredentialMetadata contains references only. Private keys and target addresses
// remain exclusively in the operator-owned configuration.
type CredentialMetadata struct {
	Ref       string     `json:"credential_ref"`
	Version   string     `json:"credential_version"`
	ExpiresAt *time.Time `json:"credential_expires_at,omitempty"`
}

// InstallCredential is a public, immutable selection approved for one target.
type InstallCredential struct {
	ID        string    `json:"id"`
	Ref       string    `json:"credential_ref"`
	Version   string    `json:"credential_version"`
	ExpiresAt time.Time `json:"expires_at"`
	PublicKey string    `json:"public_key"`
}

// CheckActiveCredential does not substitute a credential: the operator's normal
// target binding must already select the exact proven vault key. Temporary file
// credentials with matching labels are deliberately insufficient.
func (s *SSH) CheckActiveCredential(ctx context.Context, project, machine string, credential InstallCredential) error {
	ep, err := s.currentEndpoint(ctx, project+":"+machine)
	if err != nil {
		return ErrRejected
	}
	c := ep.config
	if c.CredentialProvider != "vault" || c.PrivateKeyFile != "" || c.CredentialRef != credential.Ref || c.CredentialVersion != credential.Version || c.CredentialExpiresAt == nil || !c.CredentialExpiresAt.Equal(credential.ExpiresAt) {
		return ErrRejected
	}
	public, err := installPublicIdentity(credential.PublicKey, project)
	if err != nil || ssh.FingerprintSHA256(public) != ssh.FingerprintSHA256(ep.signer.PublicKey()) {
		return ErrRejected
	}
	return nil
}

func (s *SSH) CheckInstallCredential(project, machine string, credential InstallCredential) error {
	_, _, err := s.installEndpoint(context.Background(), project, machine, credential)
	return err
}

func (s *SSH) installEndpoint(ctx context.Context, project, machine string, credential InstallCredential) (endpoint, string, error) {
	ep, err := s.currentEndpoint(ctx, project+":"+machine)
	if err != nil {
		return endpoint{}, "", ErrRejected
	}
	public, err := installPublicIdentity(credential.PublicKey, project)
	if err != nil {
		return endpoint{}, "", ErrRejected
	}
	ep.config.CredentialProvider = "vault"
	ep.config.PrivateKeyFile = ""
	ep.config.CredentialRef = credential.Ref
	ep.config.CredentialVersion = credential.Version
	ep.config.CredentialExpiresAt = &credential.ExpiresAt
	signer, err := s.resolveSigner(ctx, ep.config)
	if err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != ssh.FingerprintSHA256(public) {
		return endpoint{}, "", ErrRejected
	}
	ep.signer = signer
	raw, _ := json.Marshal(credential)
	hash := sha256.Sum256(raw)
	return ep, project + ":" + machine + ":domain:" + hex.EncodeToString(hash[:]), nil
}

// QueryInstalledCredential always opens a fresh connection using the newly
// installed key. It can only query an existing install command, never run work.
func (s *SSH) QueryInstalledCredential(ctx context.Context, r Request, credential InstallCredential) (Observation, error) {
	if r.Action != "query" || r.Command.Operation != "trust.install" || r.Command.PublicKey != credential.PublicKey {
		return Observation{}, ErrRejected
	}
	ep, id, err := s.installEndpoint(ctx, r.Command.ProjectID, r.Command.MachineID, credential)
	if err != nil {
		return Observation{}, ErrRejected
	}
	defer func() {
		s.mu.Lock()
		if old := s.pool[id]; old != nil {
			delete(s.pool, id)
			old.Close()
		}
		s.mu.Unlock()
	}()
	// Do not reuse an authenticated session: proof requires this key to be
	// accepted by a new SSH handshake after authorized_keys was updated.
	s.mu.Lock()
	if old := s.pool[id]; old != nil {
		delete(s.pool, id)
		old.Close()
	}
	s.mu.Unlock()
	return s.doEndpoint(ctx, r, id, ep)
}

var credentialVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// IsValidHostKey accepts the canonical OpenSSH SHA256 fingerprint only.
func IsValidHostKey(value string) bool {
	if !strings.HasPrefix(value, "SHA256:") || len(value) != 50 {
		return false
	}
	pin, err := base64.RawStdEncoding.DecodeString(value[7:])
	return err == nil && len(pin) == 32 && base64.RawStdEncoding.EncodeToString(pin) == value[7:]
}

type config struct {
	Targets []TargetConfig `json:"targets"`
}
type endpoint struct {
	config  TargetConfig
	signer  ssh.Signer
	binding Binding
}
type SSH struct {
	mu          sync.Mutex
	targets     map[string]endpoint
	pool        map[string]*ssh.Client
	closed      bool
	credentials CredentialProvider
}

func readPrivate(path string, max int64) ([]byte, error) {
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrRejected
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Geteuid()) || st.Size > max {
		return nil, ErrRejected
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrRejected
	}
	return b, nil
}

// Load is opt-in. The file is an operator-owned project/machine allowlist;
// secrets and addresses cannot be supplied by an execution request.
func Load(path string) (*SSH, error) {
	return LoadWithCredentials(path, nil)
}

func LoadWithCredentials(path string, credentials CredentialProvider) (*SSH, error) {
	if path == "" {
		return nil, nil
	}
	b, e := readPrivate(path, 128*1024)
	if e != nil {
		return nil, ErrRejected
	}
	var c config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return nil, ErrRejected
	}
	var extra any
	if d.Decode(&extra) != io.EOF || len(c.Targets) == 0 || len(c.Targets) > 128 {
		return nil, ErrRejected
	}
	s := &SSH{targets: map[string]endpoint{}, pool: map[string]*ssh.Client{}, credentials: credentials}
	for _, t := range c.Targets {
		if t.CredentialExpiresAt != nil {
			// PostgreSQL authorization snapshots use microsecond precision.
			// Normalize before hashing so equivalent configured instants and
			// persisted metadata remain exactly comparable.
			expires := t.CredentialExpiresAt.UTC().Truncate(time.Microsecond)
			t.CredentialExpiresAt = &expires
		}
		ip, port, e := net.SplitHostPort(t.Address)
		n, pe := strconv.Atoi(port)
		// Explicit IPs prevent DNS changes silently retargeting a queued command.
		if !idPattern.MatchString(t.ProjectID) || !idPattern.MatchString(t.MachineID) || e != nil || pe != nil || n < 1 || n > 65535 || net.ParseIP(ip) == nil || net.ParseIP(ip).IsUnspecified() || net.ParseIP(ip).IsMulticast() || t.User != "keel-ctrl" {
			return nil, ErrRejected
		}
		id := t.ProjectID + ":" + t.MachineID
		if _, ok := s.targets[id]; ok {
			return nil, ErrRejected
		}
		if t.ProvisionOnly {
			// A newly installed host has no previous pin or access credential.
			// This entry allowlists coordinates only. The authenticated first-
			// boot plan supplies the exact new pin and approved vault identity.
			if credentials == nil || t.HostKey != "" || t.CredentialProvider != "" || t.CredentialRef != "" || t.CredentialVersion != "" || t.CredentialExpiresAt != nil || t.PrivateKeyFile != "" {
				return nil, ErrRejected
			}
			s.targets[id] = endpoint{config: t}
			continue
		}
		if len(t.CredentialRef) < 1 || len(t.CredentialRef) > 64 || strings.TrimSpace(t.CredentialRef) != t.CredentialRef || !IsValidHostKey(t.HostKey) {
			return nil, ErrRejected
		}
		if (t.CredentialVersion != "" && !credentialVersionPattern.MatchString(t.CredentialVersion)) || (t.CredentialExpiresAt != nil && (t.CredentialExpiresAt.IsZero() || !t.CredentialExpiresAt.After(time.Now()) || t.CredentialVersion == "")) {
			return nil, ErrRejected
		}
		signer, e := s.resolveSigner(context.Background(), t)
		if e != nil {
			return nil, ErrRejected
		}
		// Include public credential identity: rotation invalidates old snapshots.
		identity := struct {
			Target    TargetConfig
			PublicKey string
		}{t, ssh.FingerprintSHA256(signer.PublicKey())}
		identity.Target.PrivateKeyFile = ""
		raw, _ := json.Marshal(identity)
		hash := sha256.Sum256(raw)
		ep := endpoint{t, signer, Binding{t.HostKey, hex.EncodeToString(hash[:])}}
		s.targets[id] = ep
	}
	return s, nil
}

func (s *SSH) resolveSigner(ctx context.Context, t TargetConfig) (ssh.Signer, error) {
	if t.CredentialProvider != "" {
		if t.CredentialProvider != "vault" || s.credentials == nil || t.PrivateKeyFile != "" || t.CredentialVersion == "" || t.CredentialExpiresAt == nil {
			return nil, ErrRejected
		}
		work, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		signer, expiry, err := s.credentials.Resolve(work, t.ProjectID, t.CredentialRef, t.CredentialVersion)
		if err != nil || signer == nil || !expiry.Equal(*t.CredentialExpiresAt) || !expiry.After(time.Now()) || signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
			return nil, ErrRejected
		}
		return signer, nil
	}
	key, err := readPrivate(t.PrivateKeyFile, 64*1024)
	if err != nil {
		return nil, ErrRejected
	}
	defer clear(key)
	private, err := ssh.ParseRawPrivateKey(key)
	if err != nil {
		return nil, ErrRejected
	}
	if _, ok := private.(ed25519.PrivateKey); !ok {
		return nil, ErrRejected
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return nil, ErrRejected
	}
	return signer, nil
}

func (s *SSH) Binding(project, machine string) (Binding, error) {
	e, err := s.currentEndpoint(context.Background(), project+":"+machine)
	if err != nil {
		return Binding{}, ErrRejected
	}
	return e.binding, nil
}

// HasTarget identifies a configured real transport even after its credential
// expires or the pool closes. Mock trust actions must not bless such targets.
func (s *SSH) HasTarget(project, machine string) bool {
	if s == nil {
		return false
	}
	_, ok := s.targets[project+":"+machine]
	return ok
}

func (s *SSH) CredentialMetadata(project, machine string) (CredentialMetadata, error) {
	ep, err := s.currentEndpoint(context.Background(), project+":"+machine)
	if err != nil {
		return CredentialMetadata{}, ErrRejected
	}
	metadata := CredentialMetadata{Ref: ep.config.CredentialRef, Version: ep.config.CredentialVersion}
	if ep.config.CredentialExpiresAt != nil {
		expires := *ep.config.CredentialExpiresAt
		metadata.ExpiresAt = &expires // Do not expose a mutable configuration pointer.
	}
	return metadata, nil
}

func (s *SSH) currentEndpoint(ctx context.Context, id string) (endpoint, error) {
	if s == nil {
		return endpoint{}, ErrRejected
	}
	ep, ok := s.targets[id]
	if !ok || ep.config.ProvisionOnly {
		return endpoint{}, ErrRejected
	}
	// Recheck provider authority before using even an existing SSH connection.
	// No database operation runs while holding the pool mutex.
	invalid := false
	if ep.config.CredentialProvider != "" {
		signer, err := s.resolveSigner(ctx, ep.config)
		invalid = err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != ssh.FingerprintSHA256(ep.signer.PublicKey())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || invalid || credentialExpired(ep) {
		if c := s.pool[id]; c != nil {
			delete(s.pool, id)
			c.Close()
		}
		return endpoint{}, ErrRejected
	}
	return ep, nil
}

func credentialExpired(ep endpoint) bool {
	return ep.config.CredentialExpiresAt != nil && !time.Now().Before(*ep.config.CredentialExpiresAt)
}
func (s *SSH) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, c := range s.pool {
		c.Close()
	}
	s.pool = map[string]*ssh.Client{}
}
func (s *SSH) connection(ctx context.Context, id string, ep endpoint) (*ssh.Client, error) {
	if ctx.Err() != nil {
		return nil, ErrUnknown
	}
	s.mu.Lock()
	if s.closed || credentialExpired(ep) {
		if c := s.pool[id]; c != nil {
			delete(s.pool, id)
			c.Close()
		}
		s.mu.Unlock()
		return nil, ErrRejected
	}
	if c := s.pool[id]; c != nil {
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock() // A slow target must not serialize unrelated handshakes.
	config := &ssh.ClientConfig{User: ep.config.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(ep.signer)}, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if ssh.FingerprintSHA256(k) != ep.binding.HostKey {
			return ErrRejected
		}
		return nil
	}}
	dial := net.Dialer{Timeout: 5 * time.Second}
	conn, e := dial.DialContext(ctx, "tcp", ep.config.Address)
	if e != nil {
		return nil, ErrUnknown
	}
	deadline := time.Now().Add(5 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	conn.SetDeadline(deadline)
	stopCancellation := context.AfterFunc(ctx, func() { conn.Close() })
	cc, ch, req, e := ssh.NewClientConn(conn, ep.config.Address, config)
	if !stopCancellation() {
		conn.Close()
		return nil, ErrUnknown
	}
	if e != nil {
		conn.Close()
		return nil, ErrUnknown
	}
	conn.SetDeadline(time.Time{})
	c := ssh.NewClient(cc, ch, req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || credentialExpired(ep) {
		c.Close()
		return nil, ErrRejected
	}
	if prior := s.pool[id]; prior != nil {
		c.Close()
		return prior, nil
	}
	s.pool[id] = c
	return c, nil
}
func (s *SSH) discard(id string, c *ssh.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool[id] == c {
		delete(s.pool, id)
	}
	c.Close()
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16384 {
		return 0, ErrRejected
	}
	return b.Buffer.Write(p)
}

func (s *SSH) Do(ctx context.Context, r Request) (Observation, error) {
	var zero Observation
	if s == nil || r.Command.Validate() != nil || (r.Action != "run" && r.Action != "query" && r.Action != "cancel") {
		return zero, ErrRejected
	}
	id := r.Command.ProjectID + ":" + r.Command.MachineID
	ep, err := s.currentEndpoint(ctx, id)
	if err != nil {
		return zero, ErrRejected
	}
	return s.doEndpoint(ctx, r, id, ep)
}

func (s *SSH) doEndpoint(ctx context.Context, r Request, id string, ep endpoint) (Observation, error) {
	var zero Observation
	if r.Command.Validate() != nil || (r.Action != "run" && r.Action != "query" && r.Action != "cancel") {
		return zero, ErrRejected
	}
	work, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if ep.config.CredentialExpiresAt != nil {
		var stop context.CancelFunc
		work, stop = context.WithDeadline(work, *ep.config.CredentialExpiresAt)
		defer stop()
	}
	c, e := s.connection(work, id, ep)
	if e != nil {
		return zero, e
	}
	// All SSH operations, including opening a session, are bounded. A transport
	// error never creates a stopped receipt or causes an implicit replay.
	type response struct {
		o Observation
		e error
	}
	done := make(chan response, 1)
	go func() {
		if work.Err() != nil {
			done <- response{e: ErrUnknown}
			return
		}
		session, e := c.NewSession()
		if e != nil {
			done <- response{e: ErrUnknown}
			return
		}
		defer session.Close()
		if work.Err() != nil {
			done <- response{e: ErrUnknown}
			return
		}
		input, _ := json.Marshal(r)
		session.Stdin = bytes.NewReader(input)
		var out limitedOutput
		session.Stdout = &out
		session.Stderr = io.Discard
		// Constant operator-installed entrypoint; data travels only over stdin.
		if e = session.Run("/usr/local/libexec/keel-ssh-runner"); e != nil {
			done <- response{e: ErrUnknown}
			return
		}
		var o Observation
		d := json.NewDecoder(bytes.NewReader(out.Bytes()))
		d.DisallowUnknownFields()
		if d.Decode(&o) != nil {
			done <- response{e: ErrUnknown}
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF || o.Validate(r.Command) != nil {
			done <- response{e: ErrUnknown}
			return
		}
		done <- response{o: o}
	}()
	select {
	case <-work.Done():
		s.discard(id, c)
		return zero, ErrUnknown
	case v := <-done:
		if v.e != nil {
			s.discard(id, c)
		}
		return v.o, v.e
	}
}

var _ Runner = (*SSH)(nil)

package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"time"

	"golang.org/x/crypto/ssh"
)

// ProvisionSSHPlan uses the new authenticated first-boot host key. It never
// changes the operator's ordinary SSH endpoint or trusts its previous host pin.
type ProvisionSSHPlan struct {
	BindingSHA256 string            `json:"binding_sha256"`
	Peer          netip.Addr        `json:"peer"`
	Clients       netip.Prefix      `json:"clients"`
	HostKey       string            `json:"host_key"`
	Hostname      string            `json:"hostname"`
	Arch          string            `json:"arch"`
	Credential    InstallCredential `json:"credential"`
	ExpiresAt     time.Time         `json:"expires_at"`
}

func (p ProvisionSSHPlan) digest() string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ProvisionSSHProof cannot be manufactured by consumers: valid proof is created
// only after a fresh pinned connection with the selected domain vault signer and
// a validated terminal host.identity receipt from the existing helper.
// Success and queried terminal failure require separate explicit validators.
type ProvisionSSHProof struct {
	planSHA256  string
	observation Observation
	verifiedAt  time.Time
	queried     bool
}

func (p ProvisionSSHProof) Observation() Observation {
	// Return a deep copy so consumers cannot modify the sealed receipt map.
	b, _ := json.Marshal(p.observation)
	var o Observation
	_ = json.Unmarshal(b, &o)
	return o
}
func (p ProvisionSSHProof) validateTerminal(command TaskCommand, plan ProvisionSSHPlan, now time.Time) error {
	if p.planSHA256 != plan.digest() || p.verifiedAt.IsZero() || p.verifiedAt.After(now.Add(time.Second)) || now.Sub(p.verifiedAt) > time.Minute || !plan.ExpiresAt.After(now) || p.observation.Validate(command) != nil || !p.observation.Terminal() || p.observation.FinishedAt.After(p.verifiedAt.Add(time.Second)) {
		return ErrRejected
	}
	return nil
}
func (p ProvisionSSHProof) Validate(command TaskCommand, plan ProvisionSSHPlan, now time.Time) error {
	if p.validateTerminal(command, plan, now) != nil || p.observation.State != "success" || p.observation.Output["hostname"] != plan.Hostname || p.observation.Output["os"] != "linux" || p.observation.Output["arch"] != plan.Arch {
		return ErrRejected
	}
	return nil
}

// ValidateQueriedFailure proves a terminal failure read from the immutable
// helper journal by a real pinned query, not an inference from a timeout.
func (p ProvisionSSHProof) ValidateQueriedFailure(command TaskCommand, plan ProvisionSSHPlan, now time.Time) error {
	if !p.queried || p.validateTerminal(command, plan, now) != nil || p.observation.State != "failed" {
		return ErrRejected
	}
	return nil
}

func (s *SSH) provisionEndpoint(ctx context.Context, command TaskCommand, p ProvisionSSHPlan) (endpoint, string, error) {
	if s == nil || command.Validate() != nil || command.Operation != "host.identity" || command.TimeoutMS > 10000 || !IsValidHostKey(p.HostKey) || len(p.BindingSHA256) != 64 || !p.Peer.IsValid() || !p.Clients.IsValid() || !p.Clients.Contains(p.Peer) || !p.ExpiresAt.After(time.Now()) || p.Credential.ExpiresAt.Before(p.ExpiresAt) || p.Hostname == "" || (p.Arch != "amd64" && p.Arch != "arm64") {
		return endpoint{}, "", ErrRejected
	}
	if _, e := hex.DecodeString(p.BindingSHA256); e != nil {
		return endpoint{}, "", ErrRejected
	}
	s.mu.Lock()
	ep, ok := s.targets[command.ProjectID+":"+command.MachineID]
	closed := s.closed
	s.mu.Unlock()
	if !ok || closed || ep.config.User != "keel-ctrl" {
		return endpoint{}, "", ErrRejected
	}
	host, _, err := net.SplitHostPort(ep.config.Address)
	ip, e := netip.ParseAddr(host)
	// Network coordinates remain operator allowlisted and must match the direct
	// authenticated callback peer. A report cannot supply an arbitrary SSH URL.
	if err != nil || e != nil || ip != p.Peer || ep.config.ProjectID != command.ProjectID || ep.config.MachineID != command.MachineID {
		return endpoint{}, "", ErrRejected
	}
	public, err := installPublicIdentity(p.Credential.PublicKey, command.ProjectID)
	if err != nil {
		return endpoint{}, "", ErrRejected
	}
	ep.config.CredentialProvider = "vault"
	ep.config.PrivateKeyFile = ""
	ep.config.CredentialRef = p.Credential.Ref
	ep.config.CredentialVersion = p.Credential.Version
	ep.config.CredentialExpiresAt = &p.Credential.ExpiresAt
	ep.config.HostKey = p.HostKey
	// Resolve only the approved first-boot credential on this private copy;
	// the coordinates-only operator entry is never promoted into normal use.
	ep.config.ProvisionOnly = false
	signer, err := s.resolveSigner(ctx, ep.config)
	if err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != ssh.FingerprintSHA256(public) {
		return endpoint{}, "", ErrRejected
	}
	ep.signer = signer
	ep.binding = Binding{HostKey: p.HostKey, ConfigHash: p.digest()}
	return ep, command.ProjectID + ":" + command.MachineID + ":provision:" + command.CommandID + ":" + p.digest(), nil
}

func (s *SSH) VerifyProvisionIdentity(ctx context.Context, command TaskCommand, p ProvisionSSHPlan) (ProvisionSSHProof, error) {
	return s.provisionIdentity(ctx, command, p, "run")
}

// QueryProvisionIdentity recovers only the original immutable helper receipt.
// It never creates a new command or replays run after an uncertain outcome.
func (s *SSH) QueryProvisionIdentity(ctx context.Context, command TaskCommand, p ProvisionSSHPlan) (ProvisionSSHProof, error) {
	return s.provisionIdentity(ctx, command, p, "query")
}
func (s *SSH) provisionIdentity(ctx context.Context, command TaskCommand, p ProvisionSSHPlan, action string) (ProvisionSSHProof, error) {
	ep, id, err := s.provisionEndpoint(ctx, command, p)
	if err != nil {
		return ProvisionSSHProof{}, err
	}
	// Discard before and after, ensuring a real fresh handshake and no session
	// promotion into the pool used by ordinary SSH executions.
	closeSession := func() {
		s.mu.Lock()
		if old := s.pool[id]; old != nil {
			delete(s.pool, id)
			old.Close()
		}
		s.mu.Unlock()
	}
	closeSession()
	defer closeSession()
	work, cancel := context.WithDeadline(ctx, p.ExpiresAt)
	defer cancel()
	o, err := s.doEndpoint(work, Request{Action: action, Command: command}, id, ep)
	if err != nil {
		return ProvisionSSHProof{}, err
	}
	// Query the existing fenced read-only command once; never replay run on a
	// missing/ambiguous response. A controller restart leaves a repair event.
	queried := action == "query"
	if action == "run" && !o.Terminal() {
		queried = true
		o, err = s.doEndpoint(work, Request{Action: "query", Command: command}, id, ep)
	}
	if err != nil {
		return ProvisionSSHProof{}, err
	}
	proof := ProvisionSSHProof{planSHA256: p.digest(), observation: o, verifiedAt: time.Now().UTC(), queried: queried}
	if proof.validateTerminal(command, p, time.Now()) != nil {
		return ProvisionSSHProof{}, ErrUnknown
	}
	return proof, nil
}

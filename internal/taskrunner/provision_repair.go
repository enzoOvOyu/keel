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

// ProvisionBootstrap freezes only public operator-selected metadata. It never
// invents an old host pin or a temporary credential for provision-only entries.
type ProvisionBootstrap struct {
	Binding    Binding            `json:"binding"`
	Credential CredentialMetadata `json:"credential"`
}

func (s *SSH) ProvisionRepairBootstrap(project, machine string, p ProvisionSSHPlan) (ProvisionBootstrap, error) {
	var out ProvisionBootstrap
	if s == nil || !p.ExpiresAt.After(time.Now()) {
		return out, ErrRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ep, ok := s.targets[project+":"+machine]
	if !ok || s.closed || ep.config.ProvisionOnly || ep.signer == nil || credentialExpired(ep) || ep.config.User != "keel-ctrl" || ep.binding.HostKey != p.HostKey || ep.config.CredentialVersion == "" || ep.config.CredentialExpiresAt == nil || ep.config.CredentialExpiresAt.Before(p.ExpiresAt) {
		return out, ErrRejected
	}
	host, _, err := net.SplitHostPort(ep.config.Address)
	ip, e := netip.ParseAddr(host)
	if err != nil || e != nil || ip != p.Peer || !p.Clients.Contains(ip) {
		return out, ErrRejected
	}
	out.Binding = ep.binding
	expiry := *ep.config.CredentialExpiresAt
	out.Credential = CredentialMetadata{Ref: ep.config.CredentialRef, Version: ep.config.CredentialVersion, ExpiresAt: &expiry}
	return out, nil
}

type ProvisionKeyRepairProof struct {
	bootstrapSHA256 string
	newKeyHandshake bool
	queried         bool
	command         TaskCommand
	planSHA256      string
	observation     Observation
	verifiedAt      time.Time
}

func (p ProvisionKeyRepairProof) Observation() Observation {
	return ProvisionSSHProof{observation: p.observation}.Observation()
}
func (p ProvisionKeyRepairProof) Validate(c TaskCommand, plan ProvisionSSHPlan, now time.Time) error {
	public, err := installPublicIdentity(plan.Credential.PublicKey, c.ProjectID)
	if err != nil || !p.newKeyHandshake || p.command != c || c.Operation != "trust.install" || c.PublicKey != plan.Credential.PublicKey || !plan.ExpiresAt.After(now) || c.AuthorizationExpiresAt <= now.UnixMicro() || p.planSHA256 != plan.digest() || p.verifiedAt.IsZero() || p.verifiedAt.After(now.Add(time.Second)) || now.Sub(p.verifiedAt) > time.Minute || p.observation.Validate(c) != nil || p.observation.State != "success" || p.observation.FinishedAt.After(p.verifiedAt.Add(time.Second)) || p.observation.Output["key_installed"] != true || p.observation.Output["public_key_fingerprint"] != ssh.FingerprintSHA256(public) {
		return ErrRejected
	}
	return nil
}

// ValidateQueriedFailure accepts only a terminal receipt read over the frozen
// bootstrap binding. It never treats authentication failure as an install result.
func (p ProvisionKeyRepairProof) ValidateQueriedFailure(c TaskCommand, plan ProvisionSSHPlan, bootstrap ProvisionBootstrap, now time.Time) error {
	if !p.queried || p.newKeyHandshake || p.bootstrapSHA256 != provisionBootstrapSHA256(bootstrap) || p.command != c || c.Validate() != nil || c.Operation != "trust.install" || c.PublicKey != plan.Credential.PublicKey || !plan.ExpiresAt.After(now) || c.AuthorizationExpiresAt <= now.UnixMicro() || p.planSHA256 != plan.digest() || p.verifiedAt.IsZero() || p.verifiedAt.After(now.Add(time.Second)) || now.Sub(p.verifiedAt) > time.Minute || p.observation.Validate(c) != nil || p.observation.State != "failed" || p.observation.FinishedAt.After(p.verifiedAt.Add(time.Second)) {
		return ErrRejected
	}
	return nil
}
func provisionBootstrapSHA256(b ProvisionBootstrap) string {
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RepairProvisionKey issues trust.install at most once when explicitly told to
// run. Every successful result is re-read through a fresh handshake using the
// frozen domain key; timeouts never cause automatic mutation replay.
func (s *SSH) RepairProvisionKey(ctx context.Context, c TaskCommand, p ProvisionSSHPlan, bootstrap ProvisionBootstrap, mode string) (ProvisionKeyRepairProof, error) {
	if c.Validate() != nil || c.Operation != "trust.install" || c.PublicKey != p.Credential.PublicKey || c.AuthorizationExpiresAt <= time.Now().UnixMicro() || (mode != "run" && mode != "query") {
		return ProvisionKeyRepairProof{}, ErrRejected
	}
	work, cancel := context.WithDeadline(ctx, p.ExpiresAt)
	defer cancel()
	closeSession := func(id string) {
		s.mu.Lock()
		if old := s.pool[id]; old != nil {
			delete(s.pool, id)
			old.Close()
		}
		s.mu.Unlock()
	}
	current, bootstrapErr := s.ProvisionRepairBootstrap(c.ProjectID, c.MachineID, p)
	if bootstrapErr == nil && sameProvisionBootstrap(current, bootstrap) {
		ep, err := s.currentEndpoint(work, c.ProjectID+":"+c.MachineID)
		if err == nil && ep.binding == bootstrap.Binding {
			id := c.ProjectID + ":" + c.MachineID + ":provision-repair-bootstrap:" + c.CommandID
			closeSession(id)
			defer closeSession(id)
			if mode == "run" {
				_, _ = s.doEndpoint(work, Request{Action: "run", Command: c}, id, ep)
			}
			closeSession(id) // Receipt classification requires a fresh pinned query.
			o, e := s.doEndpoint(work, Request{Action: "query", Command: c}, id, ep)
			if e == nil {
				failed := ProvisionKeyRepairProof{command: c, planSHA256: p.digest(), bootstrapSHA256: provisionBootstrapSHA256(bootstrap), observation: o, verifiedAt: time.Now().UTC(), queried: true}
				if failed.ValidateQueriedFailure(c, p, bootstrap, time.Now()) == nil {
					return failed, nil
				}
			}
		} else if mode == "run" {
			return ProvisionKeyRepairProof{}, ErrRejected
		}
	} else if mode == "run" {
		return ProvisionKeyRepairProof{}, ErrRejected
	}
	probe := c
	probe.Operation = "host.identity"
	probe.PublicKey = ""
	probe.AuthorizationExpiresAt = 0
	ep, id, err := s.provisionEndpoint(work, probe, p)
	if err != nil {
		return ProvisionKeyRepairProof{}, err
	}
	id += ":installed-key-receipt"
	closeSession(id)
	defer closeSession(id)
	o, err := s.doEndpoint(work, Request{Action: "query", Command: c}, id, ep)
	if err != nil {
		return ProvisionKeyRepairProof{}, err
	}
	proof := ProvisionKeyRepairProof{newKeyHandshake: true, queried: true, command: c, planSHA256: p.digest(), observation: o, verifiedAt: time.Now().UTC()}
	if proof.Validate(c, p, time.Now()) != nil {
		return ProvisionKeyRepairProof{}, ErrUnknown
	}
	return proof, nil
}
func sameProvisionBootstrap(a, b ProvisionBootstrap) bool {
	return a.Binding == b.Binding && a.Credential.Ref == b.Credential.Ref && a.Credential.Version == b.Credential.Version && a.Credential.ExpiresAt != nil && b.Credential.ExpiresAt != nil && a.Credential.ExpiresAt.Equal(*b.Credential.ExpiresAt)
}

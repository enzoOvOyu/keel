package platform

import (
	"bytes"
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
)

type AgentCertificateRenewal struct {
	CredentialID   string    `json:"credential_id"`
	CertificatePEM string    `json:"certificate_pem"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Queries use the immutable enrollment expiry or the last issued leaf, while
// transport authorization always validates the specific presented certificate.
const agentCredentialExpirySQL = `GREATEST(c.expires_at,COALESCE((SELECT max(r.expires_at) FROM agent_certificate_renewals r WHERE r.credential_id=c.id),c.expires_at))`

// RenewAgentCertificate appends one same-key successor for a currently valid
// registered leaf. The credential row lock serializes every generation against
// owner revocation; the advisory lock serializes retries without lock upgrades.
func (s *Store) RenewAgentCertificate(ctx context.Context, authority *agentpki.Authority, cert *x509.Certificate, csr []byte) (*AgentCertificateRenewal, error) {
	if authority == nil || cert == nil {
		return nil, ErrAgentDenied
	}
	key, e := agentpki.CSR(csr)
	if e != nil {
		return nil, ErrAgentDenied
	}
	requested, e := x509.MarshalPKIXPublicKey(key)
	if e != nil || !bytes.Equal(requested, cert.RawSubjectPublicKeyInfo) {
		return nil, ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	predecessor := agentpki.Hash(cert.Raw)
	if e = lockKey(ctx, tx, "agent.renew:"+predecessor); e != nil {
		return nil, e
	}
	credential, e := agentCredential(ctx, tx, cert, authority.ID)
	if e != nil {
		return nil, e
	}
	out := &AgentCertificateRenewal{CredentialID: credential.ID}
	e = tx.QueryRowContext(ctx, `SELECT certificate_pem,expires_at FROM agent_certificate_renewals WHERE credential_id=$1 AND predecessor_hash=$2`, credential.ID, predecessor).Scan(&out.CertificatePEM, &out.ExpiresAt)
	if e == nil {
		if !out.ExpiresAt.After(time.Now()) {
			return nil, ErrAgentDenied
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Second)
	// Certificate lifetimes cannot be extended early in an unbounded loop.
	if cert.NotAfter.Sub(now) > 20*time.Minute || !cert.NotAfter.After(now) {
		return nil, ErrAgentDenied
	}
	public, expires, e := authority.Issue(csr, credential.ProjectID, credential.MachineID, credential.ID, now)
	if e != nil || !expires.After(cert.NotAfter) {
		return nil, ErrAgentDenied
	}
	leaf, e := agentpki.Certificate(public)
	if e != nil {
		return nil, ErrAgentDenied
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO agent_certificate_renewals(credential_id,predecessor_hash,certificate_hash,certificate_pem,issued_at,expires_at)VALUES($1,$2,$3,$4,$5,$6)`, credential.ID, predecessor, agentpki.Hash(leaf.Raw), string(public), now, expires); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, "", credential.ProjectID, "agent.certificate_renewed", credential.ID, "agent", map[string]any{"predecessor_hash": predecessor, "certificate_hash": agentpki.Hash(leaf.Raw), "expires_at": expires}); e != nil {
		return nil, e
	}
	out.CertificatePEM, out.ExpiresAt = string(public), expires
	return out, tx.Commit()
}

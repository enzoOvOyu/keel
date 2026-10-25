package agentclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
)

// RenewIfDue runs only between bounded transport windows. It never cancels an
// active task, re-enrolls, rotates keys, changes task state or extends a lease.
// Retrying a lost RPC response uses the same predecessor and stored key.
func RenewIfDue(ctx context.Context, c Config, version string) (bool, error) {
	if c.Validate() != nil {
		return false, ErrIdentity
	}
	unlock, e := agentpki.LockIdentity(c.StateDirectory)
	if e != nil {
		return false, ErrIdentity
	}
	defer unlock()
	path := filepath.Join(c.StateDirectory, "identity.json")
	raw, e := agentpki.ReadFile(path, 16384, true)
	if e != nil {
		return false, ErrIdentity
	}
	var identity Identity
	if json.Unmarshal(raw, &identity) != nil || identity.Version != version {
		return false, ErrIdentity
	}
	key, e := agentpki.ReadFile(filepath.Join(c.StateDirectory, "agent-key.pem"), 16384, true)
	if e != nil || c.verify(identity, key) != nil {
		return false, ErrIdentity
	}
	pair, e := tls.X509KeyPair([]byte(identity.Certificate), key)
	if e != nil {
		return false, ErrIdentity
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return false, ErrIdentity
	}
	if time.Until(leaf.NotAfter) > 15*time.Minute {
		return false, nil
	}
	private, ok := pair.PrivateKey.(ed25519.PrivateKey)
	if !ok {
		return false, ErrIdentity
	}
	der, e := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, private)
	if e != nil {
		return false, ErrIdentity
	}
	connection, e := c.connect([]tls.Certificate{pair})
	if e != nil {
		return false, e
	}
	defer connection.Close()
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, e := pb.NewAgentIdentityClient(connection).Renew(work, &pb.RenewRequest{CsrPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})})
	if e != nil {
		return false, e
	}
	next := identity
	next.Certificate = string(response.GetCertificatePem())
	if response.GetCredentialId() != identity.CredentialID || c.verify(next, key) != nil {
		return false, ErrIdentity
	}
	renewed, e := agentpki.Certificate([]byte(next.Certificate))
	if e != nil || !renewed.NotAfter.After(leaf.NotAfter) {
		return false, ErrIdentity
	}
	data, e := json.Marshal(next)
	if e != nil || agentpki.ReplaceIdentity(path, raw, data) != nil {
		return false, ErrIdentity
	}
	return true, nil
}

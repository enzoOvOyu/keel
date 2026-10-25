package ldapauth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/ldapauth"
	ber "github.com/go-asn1-ber/asn1-ber"
)

const fixturePassword = "directory-only-secret-42"
const fixtureDN = "uid=directory-user,ou=people,dc=example,dc=invalid"

type directoryFixture struct {
	listener    net.Listener
	ca          string
	mu          sync.Mutex
	connections map[net.Conn]bool
	mode        string
	group       sync.WaitGroup
	binds       atomic.Int32
}

func newDirectoryFixture(t *testing.T) *directoryFixture {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	number, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: number, Subject: pkix.Name{CommonName: "isolated LDAP fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	certificate, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
	if e != nil {
		t.Fatal(e)
	}
	listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
	if e != nil {
		t.Fatal(e)
	}
	ca := filepath.Join(t.TempDir(), "ca.crt")
	if e = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	f := &directoryFixture{listener: listener, ca: ca, connections: map[net.Conn]bool{}}
	f.group.Add(1)
	go func() {
		defer f.group.Done()
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			f.mu.Lock()
			f.connections[connection] = true
			f.mu.Unlock()
			f.group.Add(1)
			go f.serve(connection)
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		f.mu.Lock()
		for c := range f.connections {
			c.Close()
		}
		f.mu.Unlock()
		f.group.Wait()
	})
	return f
}
func (f *directoryFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *directoryFixture) serve(connection net.Conn) {
	defer f.group.Done()
	defer func() { connection.Close(); f.mu.Lock(); delete(f.connections, connection); f.mu.Unlock() }()
	connection.SetDeadline(time.Now().Add(6 * time.Second))
	packet, e := ber.ReadPacket(connection)
	if e != nil || len(packet.Children) != 2 {
		return
	}
	bind := packet.Children[1]
	if bind.ClassType != ber.ClassApplication || bind.Tag != 0 || len(bind.Children) != 3 {
		return
	}
	f.binds.Add(1)
	f.mu.Lock()
	mode := f.mode
	f.mu.Unlock()
	if mode == "drop" {
		return
	}
	if mode == "stall" {
		var b [1]byte
		connection.Read(b[:])
		return
	}
	if mode == "delay" {
		time.Sleep(150 * time.Millisecond)
	}
	code := int64(49)
	if bind.Children[1].Value == fixtureDN && string(bind.Children[2].Data.Bytes()) == fixturePassword {
		code = 0
	}
	if mode == "protocol" {
		code = 2
	}
	response := ber.NewSequence("LDAPMessage")
	response.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, packet.Children[0].Value, "MessageID"))
	result := ber.Encode(ber.ClassApplication, ber.TypeConstructed, 1, nil, "BindResponse")
	result.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, "resultCode"))
	result.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matchedDN"))
	result.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "diagnosticMessage"))
	response.AppendChild(result)
	connection.Write(response.Bytes())
}
func (f *directoryFixture) config() ldapauth.Config {
	return ldapauth.Config{Name: "Fixture Directory", URL: "ldaps://" + f.listener.Addr().String(), CAFile: f.ca, Bindings: map[string]string{"directory-user": fixtureDN}}
}

func TestLDAPVerifiedTLSAndExactBind(t *testing.T) {
	f := newDirectoryFixture(t)
	s, e := ldapauth.New(f.config())
	if e != nil {
		t.Fatal(e)
	}
	valid, e := s.Authenticate(context.Background(), "directory-user", fixturePassword)
	if e != nil || !valid || f.binds.Load() != 1 {
		t.Fatalf("real LDAPS bind failed: %v", e)
	}
	valid, e = s.Authenticate(context.Background(), "directory-user", "wrong-directory-password")
	if e != nil || valid {
		t.Fatal("wrong credential accepted")
	}
	before := f.binds.Load()
	for _, input := range []struct{ name, password string }{{"directory-user", ""}, {"unknown", fixturePassword}, {"*)(uid=*)", fixturePassword}, {"directory-user", "nul\x00password"}} {
		if valid, e = s.Authenticate(context.Background(), input.name, input.password); e != nil || valid {
			t.Fatal("invalid or anonymous directory input accepted")
		}
	}
	if before != f.binds.Load() {
		t.Fatal("unmapped/empty input reached LDAP bind")
	}
	config := f.config()
	config.CAFile = ""
	untrusted, e := ldapauth.New(config)
	if e != nil {
		t.Fatal(e)
	}
	if valid, e = untrusted.Authenticate(context.Background(), "directory-user", fixturePassword); e == nil || valid {
		t.Fatal("untrusted TLS certificate accepted")
	}
	config = f.config()
	config.URL = strings.Replace(config.URL, "127.0.0.1", "localhost", 1)
	wrongHost, e := ldapauth.New(config)
	if e != nil {
		t.Fatal(e)
	}
	if valid, e = wrongHost.Authenticate(context.Background(), "directory-user", fixturePassword); e == nil || valid {
		t.Fatal("wrong certificate hostname accepted")
	}
	for _, mode := range []string{"drop", "protocol"} {
		f.setMode(mode)
		if valid, e = s.Authenticate(context.Background(), "directory-user", fixturePassword); e == nil || valid {
			t.Fatal("transport/protocol failure became authentication")
		}
	}
	f.setMode("stall")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if valid, e = s.Authenticate(ctx, "directory-user", fixturePassword); e == nil || valid || time.Since(started) > time.Second {
		t.Fatal("LDAP did not obey cancellation")
	}
}
func TestLDAPPrivateConfigAndNoPlaintextTransport(t *testing.T) {
	f := newDirectoryFixture(t)
	config := f.config()
	config.URL = strings.Replace(config.URL, "ldaps:", "ldap:", 1)
	if _, e := ldapauth.New(config); e == nil {
		t.Fatal("plaintext LDAP allowed")
	}
	config = f.config()
	config.Bindings = map[string]string{"directory-user": "invalid distinguished name"}
	if _, e := ldapauth.New(config); e == nil {
		t.Fatal("malformed fixed DN allowed")
	}
	config = f.config()
	config.Bindings["another-local-account"] = fixtureDN
	if _, e := ldapauth.New(config); e == nil {
		t.Fatal("same directory identity mapped to two local accounts")
	}
	config = f.config()
	path := filepath.Join(t.TempDir(), "directory.json")
	raw, _ := json.Marshal(config)
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ldapauth.Load(path); e != nil {
		t.Fatal(e)
	}
	os.Chmod(path, 0644)
	if _, e := ldapauth.Load(path); e == nil {
		t.Fatal("public config accepted")
	}
	os.Chmod(path, 0600)
	link := filepath.Join(t.TempDir(), "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := ldapauth.Load(link); e == nil {
		t.Fatal("symlink config accepted")
	}
	if s, e := ldapauth.Load(""); e != nil || s != nil {
		t.Fatal("directory not opt-in")
	}
}

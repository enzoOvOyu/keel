// Package ldapauth authenticates explicit existing accounts using verified
// LDAPS binds. It never searches a directory, creates users or imports roles.
package ldapauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"golang.org/x/sys/unix"
)

var ErrUnavailable = errors.New("organization authentication unavailable")
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

type Config struct {
	Name     string            `json:"name"`
	URL      string            `json:"url"`
	CAFile   string            `json:"ca_file,omitempty"`
	Bindings map[string]string `json:"bindings"`
}
type Service struct {
	name, address string
	tls           *tls.Config
	bindings      map[string]string
}

func readFile(path string, limit int64, secret bool) ([]byte, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	mode := uint32(0022)
	if secret {
		mode = 0077
	}
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&mode != 0 || st.Nlink != 1 || st.Size > limit {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrUnavailable
	}
	return raw, nil
}
func Load(path string) (*Service, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := readFile(path, 1024*1024, true)
	if err != nil {
		return nil, ErrUnavailable
	}
	var c Config
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil {
		return nil, ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrUnavailable
	}
	return New(c)
}
func New(c Config) (*Service, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "ldaps" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || len(c.Name) == 0 || len(c.Name) > 80 || strings.ContainsAny(c.Name, "\r\n\x00") || len(c.Bindings) > 10000 {
		return nil, ErrUnavailable
	}
	port := u.Port()
	if port == "" {
		port = "636"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, ErrUnavailable
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if c.CAFile != "" {
		raw, err := readFile(c.CAFile, 256*1024, false)
		if err != nil || !roots.AppendCertsFromPEM(raw) {
			return nil, ErrUnavailable
		}
	}
	bindings := map[string]string{}
	identities := map[string]bool{}
	for name, dn := range c.Bindings {
		if !usernamePattern.MatchString(name) || len(dn) == 0 || len(dn) > 2048 || strings.ContainsAny(dn, "\r\n\x00") {
			return nil, ErrUnavailable
		}
		parsed, err := ldap.ParseDN(dn)
		if err != nil || len(parsed.RDNs) == 0 {
			return nil, ErrUnavailable
		}
		identity := strings.ToLower(parsed.String())
		if identities[identity] {
			return nil, ErrUnavailable
		}
		identities[identity] = true
		bindings[name] = dn
	}
	return &Service{name: c.Name, address: net.JoinHostPort(u.Hostname(), port), tls: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: roots}, bindings: bindings}, nil
}
func (s *Service) Name() string {
	if s == nil {
		return ""
	}
	return s.name
}
func (s *Service) Authenticate(ctx context.Context, name, password string) (bool, error) {
	if s == nil {
		return false, ErrUnavailable
	}
	dn, ok := s.bindings[name]
	if !ok || len(password) == 0 || len(password) > 1024 || strings.ContainsRune(password, '\x00') {
		return false, nil
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: s.tls.Clone()}
	connection, err := dialer.DialContext(work, "tcp", s.address)
	if err != nil {
		return false, ErrUnavailable
	}
	defer connection.Close()
	deadline, _ := work.Deadline()
	if connection.SetDeadline(deadline) != nil {
		return false, ErrUnavailable
	}
	stop := context.AfterFunc(work, func() { connection.Close() })
	defer stop()
	client := ldap.NewConn(connection, true)
	client.Start()
	defer client.Close()
	client.SetTimeout(5 * time.Second)
	if err = client.Bind(dn, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) || ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return false, nil
		}
		return false, ErrUnavailable
	}
	return true, nil
}

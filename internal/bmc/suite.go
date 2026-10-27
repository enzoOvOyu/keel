package bmc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type TargetConfig struct {
	RedfishFixtureFile string      `json:"redfish_fixture_file,omitempty"`
	IPMI               *IPMIConfig `json:"ipmi,omitempty"`
	ProjectID          string      `json:"project_id"`
	MachineID          string      `json:"machine_id"`
	Origin             string      `json:"origin"`
	Endpoint           string      `json:"endpoint,omitempty"`
	CAFile             string      `json:"ca_file,omitempty"`
	CredentialBatchRef string      `json:"credential_batch_ref"`
}
type SuiteConfig struct {
	Schema         string         `json:"schema"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	Targets        []TargetConfig `json:"targets"`
}
type Selection struct{ Origin, CredentialBatchRef, BindingHash, Hostname string }
type inventoryEndpoint struct {
	fixture   *FixtureReader
	fallback  *ipmiTool
	config    TargetConfig
	selection Selection
	transport *http.Transport
}
type Suite struct {
	targets  map[string]inventoryEndpoint
	provider CredentialProvider
	timeout  time.Duration
}

// ReadOwnedFile reads a bounded regular owner-only file without following links.
// It is shared with the server-only password import CLI, never browser upload.
func ReadOwnedFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || limit < 1 {
		return nil, errors.New("protected absolute file required")
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, errors.New("protected file unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Geteuid()) || st.Size > limit {
		return nil, errors.New("protected file ownership, mode or size invalid")
	}
	raw, e := io.ReadAll(io.LimitReader(file, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, errors.New("protected file unreadable")
	}
	return raw, nil
}
func LoadSuite(path string, provider CredentialProvider) (*Suite, error) {
	if path == "" {
		return nil, nil
	}
	raw, e := ReadOwnedFile(path, 2<<20)
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	var config SuiteConfig
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&config) != nil || d.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid BMC suite configuration")
	}
	if config.Schema != "keel.bmc-inventory/v1" || config.TimeoutSeconds < 1 || config.TimeoutSeconds > 60 || len(config.Targets) < 1 || len(config.Targets) > 10000 {
		return nil, errors.New("invalid bounded BMC suite configuration")
	}
	suite := &Suite{targets: map[string]inventoryEndpoint{}, provider: provider, timeout: time.Duration(config.TimeoutSeconds) * time.Second}
	for _, target := range config.Targets {
		if !stableID.MatchString(target.ProjectID) || !stableID.MatchString(target.MachineID) || (target.Origin != "sim" && target.Origin != "real") || len(target.CredentialBatchRef) < 1 || len(target.CredentialBatchRef) > 120 {
			return nil, errors.New("invalid BMC target configuration")
		}
		key := target.ProjectID + ":" + target.MachineID
		if _, ok := suite.targets[key]; ok {
			return nil, errors.New("duplicate BMC target configuration")
		}
		ep := inventoryEndpoint{config: target, selection: Selection{Origin: target.Origin, CredentialBatchRef: target.CredentialBatchRef}}
		caHash := "system-trust"
		if target.Endpoint == "" {
			if target.Origin != "sim" || target.CAFile != "" {
				return nil, errors.New("real BMC requires fixed HTTPS endpoint")
			}
		} else {
			root, e := url.Parse(target.Endpoint)
			if e != nil || root.Scheme != "https" || root.Hostname() == "" || root.User != nil || root.RawQuery != "" || root.Fragment != "" || root.RawPath != "" || root.Opaque != "" || (root.Path != "" && root.Path != "/") {
				return nil, errors.New("invalid fixed HTTPS BMC origin")
			}
			tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
			if target.CAFile != "" {
				pem, e := ReadOwnedFile(target.CAFile, 1<<20)
				if e != nil {
					return nil, e
				}
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM(pem) {
					return nil, errors.New("invalid BMC CA")
				}
				tlsConfig.RootCAs = pool
				h := sha256.Sum256(pem)
				caHash = hex.EncodeToString(h[:])
			}
			ep.selection.Hostname = strings.ToLower(root.Hostname())
			ep.transport = &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: tlsConfig, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: suite.timeout, MaxResponseHeaderBytes: 64 << 10, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
		}
		fixtureHash, ipmiHash := "", ""
		if target.RedfishFixtureFile != "" {
			if target.Origin != "sim" || target.Endpoint != "" {
				return nil, errors.New("Redfish fixture requires a distinct sim binding")
			}
			ep.fixture, fixtureHash, e = loadInventoryFixture(target.RedfishFixtureFile)
			if e != nil {
				return nil, e
			}
		}
		if target.IPMI != nil {
			ep.fallback, ipmiHash, e = newIPMITool(*target.IPMI, target.Origin, provider, suite.timeout)
			if e != nil {
				return nil, e
			}
			if ep.fallback.fixture == nil && !net.ParseIP(target.IPMI.Address).Equal(net.ParseIP(ep.selection.Hostname)) {
				return nil, errors.New("IPMI and HTTPS bindings require the same literal BMC address")
			}
		}
		encoded, _ := json.Marshal(struct {
			Target      TargetConfig
			CAHash      string
			Timeout     int
			FixtureHash string `json:"FixtureHash,omitempty"`
			IPMIHash    string `json:"IPMIHash,omitempty"`
		}{target, caHash, config.TimeoutSeconds, fixtureHash, ipmiHash})
		h := sha256.Sum256(encoded)
		ep.selection.BindingHash = hex.EncodeToString(h[:])
		suite.targets[key] = ep
	}
	return suite, nil
}
func (s *Suite) Selection(project, machine string) (Selection, error) {
	if s == nil {
		return Selection{}, errors.New("BMC inventory disabled")
	}
	ep, ok := s.targets[project+":"+machine]
	if !ok {
		return Selection{}, errors.New("BMC target not configured")
	}
	return ep.selection, nil
}
func (s *Suite) Reader(target Target) (Reader, error) {
	if s == nil || target.Validate() != nil {
		return nil, errors.New("BMC inventory disabled or invalid target")
	}
	ep, ok := s.targets[target.ProjectID+":"+target.MachineID]
	if !ok || target.AdapterBindingHash != ep.selection.BindingHash || target.CredentialBatchRef != ep.selection.CredentialBatchRef {
		return nil, errors.New("BMC adapter binding changed")
	}
	if ep.fixture != nil {
		return NewFixtureReader(ep.fixture.documents, ep.fixture.failures), nil
	}
	if ep.config.Endpoint == "" {
		return NewSimReader(target)
	}
	return NewClient(ep.config.Endpoint, ep.transport, s.timeout, s.provider, target, ep.config.Origin)
}
func (s *Suite) Close() {
	if s != nil {
		for _, ep := range s.targets {
			if ep.transport != nil {
				ep.transport.CloseIdleConnections()
			}
		}
	}
}

func (s *Suite) Fallback(target Target) (IPMIReader, error) {
	if s == nil || target.Validate() != nil {
		return nil, errors.New("invalid BMC fallback binding")
	}
	ep, ok := s.targets[target.ProjectID+":"+target.MachineID]
	if !ok || target.AdapterBindingHash != ep.selection.BindingHash || target.CredentialBatchRef != ep.selection.CredentialBatchRef {
		return nil, errors.New("BMC fallback binding changed")
	}
	if ep.fallback == nil {
		return nil, nil
	}
	return ep.fallback, nil
}

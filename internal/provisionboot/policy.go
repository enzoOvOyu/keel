// Package provisionboot implements the isolated server boot transport boundary.
// It never assigns addresses, creates an installation or establishes SSH trust.
// A control-plane authority must revalidate a live exclusive machine lease for
// every boot decision/file access. There is no permissive default authority.
package provisionboot

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrDenied = errors.New("provision boot unavailable")
var opaqueID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var sha256ID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

// Scope is an explicitly reserved direct Ethernet broadcast domain. Relays and
// wildcard listeners need a separately reviewed policy; they are not inferred.
type Scope struct {
	ID        string       `json:"id"`
	ProjectID string       `json:"project_id"`
	Interface string       `json:"interface"`
	Server    netip.Addr   `json:"server"`
	Clients   netip.Prefix `json:"clients"`
	Origin    string       `json:"origin"`
	// KeaNetworks is the complete operator-supplied list of network-team owned
	// broadcast domains. Source validation must pin the current network plan too.
	KeaNetworks []netip.Prefix `json:"kea_networks"`
	Development bool           `json:"development"`
}

func (s Scope) Validate() error {
	if !opaqueID.MatchString(s.ID) || !opaqueID.MatchString(s.ProjectID) || !interfaceName.MatchString(s.Interface) {
		return ErrDenied
	}
	if !s.Server.Is4() || !(s.Server.IsGlobalUnicast() || (s.Development && s.Server.IsLoopback())) || !s.Clients.IsValid() || !s.Clients.Addr().Is4() || s.Clients != s.Clients.Masked() || s.Clients.Bits() < 8 || s.Clients.Bits() > 30 || !s.ContainsHost(s.Server) {
		return ErrDenied
	}
	if len(s.KeaNetworks) > 256 {
		return ErrDenied
	}
	for _, network := range s.KeaNetworks {
		if !network.IsValid() || !network.Addr().Is4() || network != network.Masked() || network.Overlaps(s.Clients) {
			return ErrDenied
		}
	}
	u, err := url.Parse(s.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(s.Origin, "\r\n\t ") || (u.Scheme != "https" && !(s.Development && u.Scheme == "http" && u.Hostname() == "127.0.0.1")) {
		return ErrDenied
	}
	return nil
}
func (s Scope) Broadcast() netip.Addr {
	if !s.Clients.IsValid() || !s.Clients.Addr().Is4() {
		return netip.Addr{}
	}
	b := s.Clients.Masked().Addr().As4()
	for bit := s.Clients.Bits(); bit < 32; bit++ {
		b[bit/8] |= 1 << uint(7-bit%8)
	}
	return netip.AddrFrom4(b)
}
func (s Scope) ContainsHost(ip netip.Addr) bool {
	return ip.Is4() && s.Clients.IsValid() && s.Clients.Contains(ip) && ip != s.Clients.Masked().Addr() && ip != s.Broadcast() && (ip.IsGlobalUnicast() || (s.Development && ip.IsLoopback()))
}
func (s Scope) CheckPeer(peer netip.Addr) bool { return s.ContainsHost(peer) && peer != s.Server }
func unicastMAC(value string) bool {
	mac, err := net.ParseMAC(value)
	return err == nil && len(mac) == 6 && mac[0]&1 == 0 && value == strings.ToLower(mac.String()) && value != "00:00:00:00:00:00"
}

// Grant contains only a frozen public identity. A matching struct is never an
// authorization proof: Authority must check current domain/attempt/fence/lease.
type Grant struct {
	ScopeID        string
	ID             string
	ProjectID      string
	RequestID      string
	MachineID      string
	AttemptID      string
	CommandID      string
	SnapshotSHA256 string
	FencingToken   int64
	MAC            string
	Arch           string
	ExpiresAt      time.Time
}

func (g Grant) Validate(s Scope, now time.Time) error {
	if !opaqueID.MatchString(g.ScopeID) || g.ScopeID != s.ID || !opaqueID.MatchString(g.ID) || !opaqueID.MatchString(g.ProjectID) || !opaqueID.MatchString(g.RequestID) || !opaqueID.MatchString(g.MachineID) || !opaqueID.MatchString(g.AttemptID) || !opaqueID.MatchString(g.CommandID) || !sha256ID.MatchString(g.SnapshotSHA256) || g.ProjectID != s.ProjectID || g.FencingToken < 1 || !unicastMAC(g.MAC) || (g.Arch != "amd64" && g.Arch != "arm64") || !g.ExpiresAt.After(now) {
		return ErrDenied
	}
	return nil
}

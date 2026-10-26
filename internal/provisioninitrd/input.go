// Package provisioninitrd implements the read-only phase of the approved
// installation environment. It has no disk writer or implicit install permit.
package provisioninitrd

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

var ErrUnavailable = errors.New("controlled installation preparation unavailable")
var identifier = regexp.MustCompile(`^[a-f0-9]{32}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var originPattern = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
var interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

type Input struct {
	ManifestURL    string
	ManifestSHA256 string
	GrantID        string
	BootMAC        string
	Server         netip.Addr
	Clients        netip.Prefix
}

// ParseCommandLine reads the controlled boot path, not a downloaded file's own
// checksum claim. Unknown/duplicate/case-aliased Keel fields fail closed.
func ParseCommandLine(raw string) (Input, error) {
	var input Input
	if raw == "" || len(raw) > 16<<10 || strings.ContainsAny(raw, "\x00\r\n") {
		return input, ErrUnavailable
	}
	known := map[string]bool{"keel.protocol": true, "keel.manifest": true, "keel.manifest_sha256": true, "keel.server_ipv4": true, "keel.client_cidr": true, "BOOTIF": true, "ip": true}
	values := map[string]string{}
	for _, token := range strings.Fields(raw) {
		key, value, hasValue := strings.Cut(token, "=")
		folded := strings.ToLower(key)
		if !strings.HasPrefix(folded, "keel.") && folded != "bootif" && folded != "ip" {
			continue
		}
		if !known[key] || !hasValue || value == "" || values[key] != "" {
			return input, ErrUnavailable
		}
		values[key] = value
	}
	if len(values) != len(known) || values["keel.protocol"] != mediaboot.InitrdProtocol || values["ip"] != "off" || !strings.HasPrefix(values["BOOTIF"], "01-") {
		return input, ErrUnavailable
	}
	input.BootMAC = strings.ReplaceAll(strings.TrimPrefix(values["BOOTIF"], "01-"), "-", ":")
	input.ManifestURL = values["keel.manifest"]
	input.ManifestSHA256 = values["keel.manifest_sha256"]
	input.Server, _ = netip.ParseAddr(values["keel.server_ipv4"])
	input.Clients, _ = netip.ParsePrefix(values["keel.client_cidr"])
	parsed, err := url.Parse(input.ManifestURL)
	if err != nil {
		return Input{}, ErrUnavailable
	}
	parts := strings.Split(parsed.Path, "/")
	if len(parts) != 4 {
		return Input{}, ErrUnavailable
	}
	input.GrantID = parts[2]
	if input.validate(false) != nil {
		return Input{}, ErrUnavailable
	}
	return input, nil
}
func (i Input) validate(development bool) error {
	parsed, err := url.Parse(i.ManifestURL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || !originPattern.MatchString(parsed.Scheme+"://"+parsed.Host) || parsed.Path != "/boot/"+i.GrantID+"/manifest.json" || !identifier.MatchString(i.GrantID) || !digest.MatchString(i.ManifestSHA256) {
		return ErrUnavailable
	}
	if p := parsed.Port(); p != "" {
		n, err := net.LookupPort("tcp", p)
		if err != nil || n < 1 || n > 65535 {
			return ErrUnavailable
		}
	}
	mac, err := net.ParseMAC(i.BootMAC)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() != i.BootMAC || i.BootMAC == "00:00:00:00:00:00" {
		return ErrUnavailable
	}
	if !i.Server.Is4() || !(i.Server.IsGlobalUnicast() || (development && i.Server.IsLoopback())) || !i.Clients.IsValid() || !i.Clients.Addr().Is4() || i.Clients != i.Clients.Masked() || i.Clients.Bits() < 8 || i.Clients.Bits() > 30 || !i.Clients.Contains(i.Server) || i.Server == i.Clients.Addr() || i.Server == broadcast(i.Clients) {
		return ErrUnavailable
	}
	return nil
}
func broadcast(prefix netip.Prefix) netip.Addr {
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return netip.Addr{}
	}
	bytes := prefix.Masked().Addr().As4()
	for bit := prefix.Bits(); bit < 32; bit++ {
		bytes[bit/8] |= 1 << uint(7-bit%8)
	}
	return netip.AddrFrom4(bytes)
}

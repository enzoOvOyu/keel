package fabricboot

import (
	"encoding/binary"
	"errors"
	"net/url"
	"strconv"
)

// RFC8572 section8.3: each URI is prefixed by a two-octet network-order length.
// The payload is shared by DHCPv4 option143 and DHCPv6 option136.
func EncodeSZTPRedirect(uris []string) ([]byte, error) {
	if len(uris) < 1 || len(uris) > 4 {
		return nil, errors.New("SZTP requires one through four bootstrap origins")
	}
	out := []byte{}
	seen := map[string]bool{}
	for _, raw := range uris {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.String() != raw || len(raw) > 65535 || seen[raw] {
			return nil, errors.New("SZTP URI must be a unique HTTPS origin")
		}
		if u.Port() != "" {
			port, e := strconv.Atoi(u.Port())
			if e != nil || port < 1 || port > 65535 {
				return nil, errors.New("invalid SZTP server port")
			}
		}
		seen[raw] = true
		length := make([]byte, 2)
		binary.BigEndian.PutUint16(length, uint16(len(raw)))
		out = append(out, length...)
		out = append(out, []byte(raw)...)
	}
	return out, nil
}
func DecodeSZTPRedirect(raw []byte) ([]string, error) {
	uris := []string{}
	for len(raw) > 0 {
		if len(raw) < 2 {
			return nil, errors.New("truncated SZTP URI length")
		}
		n := int(binary.BigEndian.Uint16(raw))
		raw = raw[2:]
		if n == 0 || n > len(raw) {
			return nil, errors.New("truncated SZTP URI")
		}
		uris = append(uris, string(raw[:n]))
		raw = raw[n:]
	}
	if _, e := EncodeSZTPRedirect(uris); e != nil {
		return nil, e
	}
	return uris, nil
}
func EncodeSZTPv6Option(uris []string) ([]byte, error) {
	data, e := EncodeSZTPRedirect(uris)
	if e != nil {
		return nil, e
	}
	if len(data) > 65535 {
		return nil, errors.New("SZTP option136 exceeds DHCPv6 length")
	}
	out := make([]byte, 4)
	binary.BigEndian.PutUint16(out, 136)
	binary.BigEndian.PutUint16(out[2:], uint16(len(data)))
	return append(out, data...), nil
}

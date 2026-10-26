package fabricboot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// DHCP6 reservations use an explicit observed DUID, never a guessed MAC-derived
// identifier. This adapter does not allocate addresses or approve a deployment.
type KeaDHCP6Reservation struct {
	SwitchID string `json:"switch_id"`
	DUID     string `json:"duid"`
	Address  string `json:"address"`
	Hostname string `json:"hostname"`
}

func decodeDUID(raw string) ([]byte, error) {
	compact := strings.ReplaceAll(raw, ":", "")
	data, err := hex.DecodeString(compact)
	if err != nil || len(data) < 3 || len(data) > 128 || raw != colonHex(data) {
		return nil, errors.New("DUID must be canonical lowercase colon-separated hex, 3 through 128 octets")
	}
	// RFC8415 types 1..4 have fixed minimum framing; opaque future identifiers
	// need their own adapter rather than accidentally accepting a truncated DUID.
	kind := int(data[0])<<8 | int(data[1])
	if kind == 1 && len(data) >= 9 || kind == 2 && len(data) >= 7 || kind == 3 && len(data) >= 5 || kind == 4 && len(data) == 18 {
		return data, nil
	}
	return nil, errors.New("unsupported or truncated DUID")
}
func colonHex(data []byte) string {
	parts := make([]string, len(data))
	for i, v := range data {
		parts[i] = fmt.Sprintf("%02x", v)
	}
	return strings.Join(parts, ":")
}
func validDHCP6Hostname(s string) bool {
	if len(s) < 1 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// MergeBootstrap136 changes only platform-owned reservations in one existing
// subnet6. Global/shared/database reservations and custom option definitions
// require an explicit compatibility adapter. Never emit IPv4 options here.
func MergeBootstrap136(live json.RawMessage, cidr, owner string, bindings []KeaDHCP6Reservation, uris []string) (json.RawMessage, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || !prefix.Addr().IsGlobalUnicast() || prefix.Masked().String() != cidr || prefix.Bits() < 16 || prefix.Bits() > 126 || !projectPattern.MatchString(owner) || len(bindings) < 1 || len(bindings) > 2000 {
		return nil, errors.New("invalid owned DHCPv6 subnet or bindings")
	}
	payload, err := EncodeSZTPv6Option(uris)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(live))
	decoder.UseNumber()
	if decoder.Decode(&config) != nil {
		return nil, errors.New("invalid Kea configuration")
	}
	dhcp, ok := config["Dhcp6"].(map[string]any)
	if !ok {
		return nil, errors.New("missing Dhcp6")
	}
	for key := range config {
		if key != "Dhcp6" && key != "Logging" {
			return nil, errors.New("unexpected DHCPv6 configuration namespace")
		}
	}
	if err = rejectCustom136(dhcp); err != nil {
		return nil, err
	}
	for _, key := range []string{"hosts-database", "hosts-databases", "shared-networks", "reservations"} {
		if v, exists := dhcp[key]; exists && v != nil {
			switch a := v.(type) {
			case []any:
				if len(a) == 0 {
					continue
				}
			}
			return nil, errors.New("external, global or shared DHCPv6 reservations require a dedicated adapter")
		}
	}
	subnets, ok := dhcp["subnet6"].([]any)
	if !ok {
		return nil, errors.New("missing subnet6")
	}
	var subnet map[string]any
	for _, entry := range subnets {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New("invalid DHCPv6 subnet")
		}
		if m["subnet"] == cidr {
			if subnet != nil {
				return nil, errors.New("ambiguous DHCPv6 subnet")
			}
			subnet = m
		}
	}
	if subnet == nil {
		return nil, errors.New("approved subnet6 is absent")
	}
	for _, scope := range []map[string]any{dhcp, subnet} {
		if disabled, ok := scope["reservations-in-subnet"].(bool); ok && !disabled {
			return nil, errors.New("DHCPv6 reservations are disabled")
		}
		if ids, exists := scope["host-reservation-identifiers"]; exists {
			list, ok := ids.([]any)
			found := false
			for _, v := range list {
				if v == "duid" {
					found = true
				}
			}
			if !ok || !found {
				return nil, errors.New("DUID reservation lookup is disabled")
			}
		}
	}
	existing, ok := subnet["reservations"].([]any)
	if subnet["reservations"] != nil && !ok {
		return nil, errors.New("invalid DHCPv6 reservations")
	}
	duids, addresses, hosts, switches := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, binding := range bindings {
		if _, err = decodeDUID(binding.DUID); err != nil {
			return nil, err
		}
		address, err := netip.ParseAddr(binding.Address)
		if err != nil || !address.Is6() || address.Is4In6() || address.Zone() != "" || !address.IsGlobalUnicast() || !prefix.Contains(address) || address == prefix.Addr() || address.String() != binding.Address || !projectPattern.MatchString(binding.SwitchID) || !validDHCP6Hostname(binding.Hostname) {
			return nil, errors.New("invalid DHCPv6 target binding")
		}
		if duids[binding.DUID] || addresses[binding.Address] || hosts[binding.Hostname] || switches[binding.SwitchID] {
			return nil, errors.New("duplicate DHCPv6 target binding")
		}
		duids[binding.DUID] = true
		addresses[binding.Address] = true
		hosts[binding.Hostname] = true
		switches[binding.SwitchID] = true
		record := map[string]any{"duid": binding.DUID, "ip-addresses": []any{binding.Address}, "hostname": binding.Hostname, "user-context": map[string]any{"keel-owner": owner, "keel-switch": binding.SwitchID}, "option-data": []any{map[string]any{"code": 136, "space": "dhcp6", "csv-format": false, "data": hex.EncodeToString(payload[4:]), "always-send": true}}}
		replaced := false
		for i, entry := range existing {
			current, ok := entry.(map[string]any)
			if !ok {
				return nil, errors.New("invalid DHCPv6 reservation")
			}
			match := false
			if raw, ok := current["duid"].(string); ok {
				data, e := hex.DecodeString(strings.ReplaceAll(raw, ":", ""))
				match = e == nil && colonHex(data) == binding.DUID
			}
			collision := current["hostname"] == binding.Hostname
			if raw, exists := current["ip-addresses"]; exists {
				ips, ok := raw.([]any)
				if !ok {
					return nil, errors.New("invalid reserved IPv6 addresses")
				}
				for _, v := range ips {
					text, ok := v.(string)
					if !ok {
						return nil, errors.New("invalid reserved IPv6 address")
					}
					ip, e := netip.ParseAddr(text)
					if e != nil {
						return nil, errors.New("invalid reserved IPv6 address")
					}
					if ip == address {
						collision = true
					}
				}
			}
			metadata, _ := current["user-context"].(map[string]any)
			sameOwner := metadata["keel-owner"] == owner && metadata["keel-switch"] == binding.SwitchID
			if !match && !collision && !sameOwner {
				continue
			}
			if !match || !sameOwner || len(metadata) != 2 || replaced {
				return nil, errors.New("conflicting or ambiguous DHCPv6 reservation")
			}
			for key := range current {
				if key != "duid" && key != "ip-addresses" && key != "hostname" && key != "user-context" && key != "option-data" {
					return nil, errors.New("owned DHCPv6 reservation has independent settings")
				}
			}
			ips, ok := current["ip-addresses"].([]any)
			if !ok || len(ips) != 1 {
				return nil, errors.New("owned DHCPv6 reservation has additional addresses")
			}
			options, ok := current["option-data"].([]any)
			if !ok || len(options) != 1 {
				return nil, errors.New("owned DHCPv6 reservation has independent options")
			}
			option, ok := options[0].(map[string]any)
			if !ok || fmt.Sprint(option["code"]) != "136" || option["space"] != "dhcp6" {
				return nil, errors.New("owned DHCPv6 option is not136")
			}
			for key := range option {
				if key != "code" && key != "space" && key != "csv-format" && key != "data" && key != "always-send" {
					return nil, errors.New("owned DHCPv6 option has independent settings")
				}
			}
			existing[i] = record
			replaced = true
		}
		if !replaced {
			existing = append(existing, record)
		}
	}
	subnet["reservations"] = existing
	return json.Marshal(config)
}
func rejectCustom136(value any) error {
	switch v := value.(type) {
	case map[string]any:
		if raw, exists := v["option-def"]; exists {
			defs, ok := raw.([]any)
			if !ok {
				return errors.New("invalid option definitions")
			}
			for _, raw := range defs {
				def, ok := raw.(map[string]any)
				if !ok {
					return errors.New("invalid option definition")
				}
				space, _ := def["space"].(string)
				if fmt.Sprint(def["code"]) == "136" && (space == "" || space == "dhcp6") {
					return errors.New("custom option136 requires a dedicated adapter")
				}
			}
		}
		for _, child := range v {
			if err := rejectCustom136(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := rejectCustom136(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// KeaDHCP6Control is deliberately a separate type: a v4 deployment cannot
// silently target dhcp6 or overwrite the other service through a control agent.
type KeaDHCP6Control struct{ control *KeaControl }

func NewKeaDHCP6Control(endpoint string, roots *x509.CertPool, cert tls.Certificate, throughAgent bool) (*KeaDHCP6Control, error) {
	k, err := NewKeaControl(endpoint, roots, cert, throughAgent)
	if err != nil {
		return nil, err
	}
	if throughAgent {
		k.service = []string{"dhcp6"}
	}
	return &KeaDHCP6Control{control: k}, nil
}
func (k *KeaDHCP6Control) ReadFull(ctx context.Context) (*KeaLiveConfig, error) {
	return k.control.readFull(ctx, "Dhcp6")
}
func (k *KeaDHCP6Control) TestFull(ctx context.Context, config json.RawMessage) error {
	if err := validateFullDHCP6(config); err != nil {
		return err
	}
	_, err := k.control.command(ctx, "config-test", config)
	return err
}
func validateFullDHCP6(config json.RawMessage) error {
	var top map[string]json.RawMessage
	if json.Unmarshal(config, &top) != nil || len(top["Dhcp6"]) == 0 {
		return errors.New("full Dhcp6 configuration is required")
	}
	var dhcp map[string]json.RawMessage
	if json.Unmarshal(top["Dhcp6"], &dhcp) != nil || dhcp == nil {
		return errors.New("full Dhcp6 configuration is required")
	}
	for key := range top {
		if key != "Dhcp6" && key != "Logging" {
			return errors.New("unexpected DHCPv6 namespace")
		}
	}
	return nil
}
func (k *KeaDHCP6Control) PersistFull(ctx context.Context, config json.RawMessage, expectedHash string) (*KeaApplyResult, error) {
	if err := validateFullDHCP6(config); err != nil {
		return nil, err
	}
	return k.control.persistFull(ctx, config, expectedHash, "Dhcp6")
}

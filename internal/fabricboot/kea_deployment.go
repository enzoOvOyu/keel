package fabricboot

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// KeaMutationUnknown means a write may have reached Kea. Callers must retain
// resource quarantine and reconcile; a failed/cancelled receipt is not proof.
type KeaMutationUnknown struct{ Stage string }

func (e *KeaMutationUnknown) Error() string {
	return "Kea mutation requires reconciliation at " + e.Stage
}

type KeaLiveConfig struct {
	Config json.RawMessage
	Hash   string
}

func (k *KeaControl) ReadFullDHCP4(ctx context.Context) (*KeaLiveConfig, error) {
	return k.readFull(ctx, "Dhcp4")
}
func (k *KeaControl) readFull(ctx context.Context, namespace string) (*KeaLiveConfig, error) {
	reply, e := k.command(ctx, "config-get", nil)
	if e != nil {
		return nil, e
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(reply.Arguments, &fields) != nil || len(fields[namespace]) == 0 {
		return nil, errors.New("Kea config-get has no expected service configuration")
	}
	var hash string
	if json.Unmarshal(fields["hash"], &hash) != nil || !keaHashPattern.MatchString(hash) {
		return nil, errors.New("Kea config-get has no verified active hash")
	}
	delete(fields, "hash")
	for key := range fields {
		if key != namespace && key != "Logging" {
			return nil, errors.New("unexpected Kea configuration namespace")
		}
	}
	data, e := json.Marshal(fields)
	if e != nil {
		return nil, e
	}
	return &KeaLiveConfig{Config: data, Hash: strings.ToUpper(hash)}, nil
}
func (k *KeaControl) TestFullDHCP4(ctx context.Context, config json.RawMessage) error {
	_, e := k.command(ctx, "config-test", config)
	return e
}

// PersistFullDHCP4 uses only Kea's startup configuration path, never a filename
// supplied by a caller. No automatic retry follows any ambiguous write.
func (k *KeaControl) PersistFullDHCP4(ctx context.Context, config json.RawMessage, expectedHash string) (*KeaApplyResult, error) {
	return k.persistFull(ctx, config, expectedHash, "Dhcp4")
}
func (k *KeaControl) persistFull(ctx context.Context, config json.RawMessage, expectedHash, namespace string) (*KeaApplyResult, error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(config, &top) != nil {
		return nil, errors.New("invalid full Kea configuration")
	}
	var service map[string]json.RawMessage
	if json.Unmarshal(top[namespace], &service) != nil || service == nil {
		return nil, errors.New("expected Kea service configuration is required")
	}
	for key := range top {
		if key != namespace && key != "Logging" {
			return nil, errors.New("unexpected Kea configuration namespace")
		}
	}

	if !keaHashPattern.MatchString(expectedHash) {
		return nil, errors.New("expected Kea hash is required")
	}
	before, e := k.hash(ctx)
	if e != nil {
		return nil, e
	}
	if before != strings.ToUpper(expectedHash) {
		return nil, errors.New("Kea active configuration changed before apply")
	}
	if e = k.TestFullDHCP4(ctx, config); e != nil {
		return nil, e
	}
	current, e := k.hash(ctx)
	if e != nil {
		return nil, e
	}
	if current != before {
		return nil, errors.New("Kea configuration drifted during config-test")
	}
	applied, e := k.command(ctx, "config-set", config)
	if e != nil {
		return nil, &KeaMutationUnknown{Stage: "config-set"}
	}
	var response struct {
		Hash string `json:"hash"`
	}
	if json.Unmarshal(applied.Arguments, &response) != nil || !keaHashPattern.MatchString(response.Hash) {
		return nil, &KeaMutationUnknown{Stage: "config-set-reply"}
	}
	active, e := k.hash(ctx)
	if e != nil || active != strings.ToUpper(response.Hash) {
		return nil, &KeaMutationUnknown{Stage: "active-readback"}
	}
	// Reading the full content checks the actual staged candidate, not only a
	// hash echoed by the server. Credentials in this payload remain in memory.
	live, e := k.readFull(ctx, namespace)
	if e != nil || live.Hash != active || KeaConfigDigest(live.Config) != KeaConfigDigest(config) {
		return nil, &KeaMutationUnknown{Stage: "content-readback"}
	}
	persisted, e := k.command(ctx, "config-write", nil)
	if e != nil {
		return nil, &KeaMutationUnknown{Stage: "config-write"}
	}
	var disk struct {
		Size     int64  `json:"size"`
		Filename string `json:"filename"`
	}
	if json.Unmarshal(persisted.Arguments, &disk) != nil || disk.Size <= 0 || disk.Filename == "" {
		return nil, &KeaMutationUnknown{Stage: "persistence-reply"}
	}
	final, e := k.hash(ctx)
	if e != nil || final != active {
		return nil, &KeaMutationUnknown{Stage: "persistence-readback"}
	}
	return &KeaApplyResult{BeforeHash: before, ActiveHash: active, CandidateSHA256: KeaConfigDigest(config)}, nil
}

type KeaReservation struct {
	SwitchID string `json:"switch_id"`
	MAC      string `json:"management_mac"`
	IP       string `json:"management_ip"`
	Hostname string `json:"hostname"`
}

// MergeBootstrap239 changes only exact reservations in one existing approved
// management subnet. Global settings, other subnets and unrelated reservations
// are preserved; foreign/ambiguous bindings are rejected instead of overwritten.
func MergeBootstrap239(live json.RawMessage, cidr, bootURL, owner string, bindings []KeaReservation) (json.RawMessage, error) {
	return mergeBootstrap239(live, cidr, bootURL, owner, bindings, nil, nil)
}
func MergeBootstrapDual(live json.RawMessage, cidr, bootURL, owner string, bindings []KeaReservation, uris []string) (json.RawMessage, error) {
	redirect, e := EncodeSZTPRedirect(uris)
	if e != nil {
		return nil, e
	}
	if len(redirect) > 255 {
		return nil, errors.New("option143 payload exceeds the single-option capability")
	}
	return mergeBootstrap239(live, cidr, bootURL, owner, bindings, redirect, nil)
}

// MergeBootstrapVendor adds the operator-defined option43 TLV to the exact
// managed reservations. Option60 remains client-supplied, not a server echo.
func MergeBootstrapVendor(live json.RawMessage, cidr, bootURL, owner string, bindings []KeaReservation, uris []string, profile DHCPVendorProfile) (json.RawMessage, error) {
	var redirect []byte
	if len(uris) > 0 {
		var err error
		redirect, err = EncodeSZTPRedirect(uris)
		if err != nil || len(redirect) > 255 {
			return nil, errors.New("invalid option143 redirect")
		}
	}
	if _, err := profile.Payload(); err != nil {
		return nil, err
	}
	return mergeBootstrap239(live, cidr, bootURL, owner, bindings, redirect, &profile)
}
func mergeBootstrap239(live json.RawMessage, cidr, bootURL, owner string, bindings []KeaReservation, redirect []byte, profile *DHCPVendorProfile) (json.RawMessage, error) {
	if len(bootURL) > 255 || !strings.HasPrefix(bootURL, "https://") {
		return nil, errors.New("239 URL must be HTTPS and fit one DHCP option")
	}
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(live))
	decoder.UseNumber()
	if decoder.Decode(&config) != nil {
		return nil, errors.New("invalid live Kea configuration")
	}
	dhcp, ok := config["Dhcp4"].(map[string]any)
	if !ok {
		return nil, errors.New("missing Dhcp4")
	}
	if profile != nil {
		// This adapter uses the built-in RFC2132 TLV interpretation. A controller
		// with custom option43 definitions needs its own vendor adapter; never
		// overwrite definitions used by unrelated clients.
		if err := rejectCustomVendorDefinitions(dhcp); err != nil {
			return nil, err
		}
	}
	// Shared-network subnets and external host databases require their own
	// adapter. Fail explicitly, never add a parallel shadow subnet or reservation.
	if hosts, ok := dhcp["hosts-database"]; ok && hosts != nil {
		return nil, errors.New("external Kea host database is not supported by this reservation adapter")
	}
	if hosts, ok := dhcp["hosts-databases"]; ok && hosts != nil {
		return nil, errors.New("external Kea host databases are not supported by this reservation adapter")
	}
	subnets, _ := dhcp["subnet4"].([]any)
	var subnet map[string]any
	for _, entry := range subnets {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New("invalid Kea subnet")
		}
		if m["subnet"] == cidr {
			if subnet != nil {
				return nil, errors.New("ambiguous management subnet")
			}
			subnet = m
		}
	}
	if subnet == nil {
		return nil, errors.New("approved management subnet does not exist in Kea subnet4")
	}
	if v, ok := subnet["reservations-in-subnet"].(bool); ok && !v {
		return nil, errors.New("Kea subnet reservations are disabled")
	}
	existing, ok := subnet["reservations"].([]any)
	if subnet["reservations"] != nil && !ok {
		return nil, errors.New("invalid subnet reservations collection")
	}
	if global, ok := dhcp["reservations"].([]any); ok && len(global) > 0 {
		return nil, errors.New("global reservations require a dedicated compatibility adapter")
	}
	seenMAC, seenIP, seenHost := map[string]bool{}, map[string]bool{}, map[string]bool{}

	for _, binding := range bindings {
		mac, e := net.ParseMAC(binding.MAC)
		if e != nil || len(mac) != 6 || mac[0]&1 != 0 {
			return nil, errors.New("invalid management MAC")
		}
		canonical := mac.String()
		if seenMAC[canonical] || seenIP[binding.IP] || seenHost[binding.Hostname] {
			return nil, errors.New("duplicate management reservation identity")
		}
		seenMAC[canonical] = true
		seenIP[binding.IP] = true
		seenHost[binding.Hostname] = true

		record := map[string]any{"hw-address": canonical, "ip-address": binding.IP, "hostname": binding.Hostname, "user-context": map[string]any{"keel-owner": owner, "keel-switch": binding.SwitchID}, "option-data": []any{map[string]any{"code": 239, "space": "dhcp4", "csv-format": false, "data": hex.EncodeToString([]byte(bootURL)), "always-send": true}}}
		if len(redirect) > 0 {
			options := record["option-data"].([]any)
			record["option-data"] = append(options, map[string]any{"code": 143, "space": "dhcp4", "csv-format": false, "data": hex.EncodeToString(redirect), "always-send": true})
		}
		if profile != nil {
			payload, _ := profile.Payload()
			record["user-context"].(map[string]any)["keel-vendor-profile"] = ManagementPlanDigest(*profile)
			record["option-data"] = append(record["option-data"].([]any), map[string]any{"code": 43, "space": "dhcp4", "csv-format": false, "data": hex.EncodeToString(payload), "always-send": true})
		}
		replaced := false
		for i, entry := range existing {
			current, ok := entry.(map[string]any)
			if !ok {
				return nil, errors.New("invalid Kea reservation")
			}
			matchMAC := false
			if raw, ok := current["hw-address"].(string); ok {
				if parsed, e := net.ParseMAC(raw); e == nil {
					matchMAC = parsed.String() == canonical
				}
			}
			if matchMAC || current["ip-address"] == binding.IP || current["hostname"] == binding.Hostname {
				context, _ := current["user-context"].(map[string]any)
				if !matchMAC || context["keel-owner"] != owner || context["keel-switch"] != binding.SwitchID {
					return nil, fmt.Errorf("conflicting existing reservation for switch %s", binding.SwitchID)
				}
				vendorOwned := false
				if marker, ok := context["keel-vendor-profile"].(string); ok && len(marker) == 64 {
					decoded, err := hex.DecodeString(marker)
					vendorOwned = err == nil && len(decoded) == 32
				}
				if len(context) != 2 && !(profile != nil && vendorOwned && len(context) == 3) {
					return nil, errors.New("managed reservation contains independent metadata")
				}
				if profile == nil && vendorOwned {
					return nil, errors.New("vendor-managed reservation requires an explicit vendor profile")
				}
				options, ok := current["option-data"].([]any)
				if !ok || len(options) < 1 || len(options) > 3 || len(redirect) == 0 && len(options) > 1 && !vendorOwned {
					return nil, errors.New("managed reservation contains independent options")
				}
				if profile == nil && len(options) > 2 {
					return nil, errors.New("managed reservation contains independent options")
				}
				seenCodes := map[string]bool{}
				for _, rawOption := range options {
					option, ok := rawOption.(map[string]any)
					if !ok {
						return nil, errors.New("invalid managed option")
					}
					code := fmt.Sprint(option["code"])
					if seenCodes[code] || code != "239" && (code != "143" || len(redirect) == 0) && (code != "43" || profile == nil || !vendorOwned) {
						return nil, errors.New("managed reservation contains independent options")
					}
					seenCodes[code] = true
					for field := range option {
						if field != "code" && field != "space" && field != "csv-format" && field != "data" && field != "always-send" {
							return nil, errors.New("managed option contains independent settings")
						}
					}
				}
				if replaced {
					return nil, errors.New("duplicate managed reservation")
				}
				// Do not silently discard independently attached settings/fields.
				for key := range current {
					if key != "hw-address" && key != "ip-address" && key != "hostname" && key != "user-context" && key != "option-data" {
						return nil, errors.New("managed reservation contains unsupported extra settings")
					}
				}
				existing[i] = record
				replaced = true
			}
		}
		if !replaced {
			existing = append(existing, record)
		}
	}
	subnet["reservations"] = existing
	return json.Marshal(config)
}

func KeaConfigDigest(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ""
	}
	return ManagementPlanDigest(value)
}

// Definitions may occur in global, subnet and class scopes. Refuse ambiguous
// semantics even when the immediate managed reservation could be overwritten.
func rejectCustomVendorDefinitions(value any) error {
	switch v := value.(type) {
	case map[string]any:
		if defs, ok := v["option-def"].([]any); ok {
			for _, raw := range defs {
				def, ok := raw.(map[string]any)
				if !ok {
					return errors.New("invalid Kea option definition")
				}
				space, _ := def["space"].(string)
				if (fmt.Sprint(def["code"]) == "43" && (space == "" || space == "dhcp4")) || space == "vendor-encapsulated-options-space" {
					return errors.New("custom option43 definitions require a dedicated vendor adapter")
				}
			}
		}
		for _, child := range v {
			if err := rejectCustomVendorDefinitions(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := rejectCustomVendorDefinitions(child); err != nil {
				return err
			}
		}
	}
	return nil
}

package fabricboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	refPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
	projectPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// DHCPOptions is a mock Kea configuration contract. It does not send changes
// to a live DHCP server. Option 60 selects the vendor class; option 43 carries
// NOS and version intent. Optional DHCP6 uses explicit DUID reservations.
type DHCPOptions struct {
	DHCP6          *DHCP6MockOptions `json:"dhcp6,omitempty"`
	ProjectID      string            `json:"project_id"`
	BatchRef       string            `json:"batch_ref"`
	StageRef       string            `json:"stage_ref"`
	ManagementCIDR string            `json:"management_cidr"`
	Option239URL   string            `json:"option_239_url"`
	Option143URIs  []string          `json:"option_143_uris"`
	Option60VCI    string            `json:"option_60_vci"`
	Option43       map[string]string `json:"option_43"`
}

type DHCP6MockOptions struct {
	ManagementCIDR string                `json:"management_cidr"`
	Bindings       []KeaDHCP6Reservation `json:"bindings"`
}

type stagedPlan struct {
	active     *DHCPOptions
	activeHash string
	staged     *DHCPOptions
	stagedHash string
}

type MockKea struct {
	mu    sync.RWMutex
	plans map[string]*stagedPlan
}

func NewMockKea() *MockKea { return &MockKea{plans: make(map[string]*stagedPlan)} }

func planKey(projectID, batchRef, stageRef string) string {
	return projectID + "\x00" + batchRef + "\x00" + stageRef
}

func validBootURL(raw string, schemes map[string]bool, projectID, batchRef, stageRef string, requireRoute bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || !schemes[parsed.Scheme] || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return errors.New("invalid absolute boot URL")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("invalid boot URL control character")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return errors.New("invalid boot URL query")
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "sn", "serial", "mac", "mgmt_mac", "base_mac", "device", "device_id", "switch_id":
			return errors.New("device identity cannot be embedded in boot URL")
		}
	}
	if requireRoute && (len(query["project"]) != 1 || query.Get("project") != projectID || len(query["policy"]) != 1 || !projectPattern.MatchString(query.Get("policy")) || len(query["batch"]) != 1 || query.Get("batch") != batchRef || len(query["stage"]) != 1 || query.Get("stage") != stageRef) {
		return errors.New("option 239 must route by project, batch, stage and approved script reference")
	}
	return nil
}

func validateOptions(input DHCPOptions) (DHCPOptions, error) {
	if !projectPattern.MatchString(input.ProjectID) || !refPattern.MatchString(input.BatchRef) || !refPattern.MatchString(input.StageRef) {
		return input, errors.New("invalid project, batch or stage")
	}
	prefix, err := netip.ParsePrefix(input.ManagementCIDR)
	if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() < 8 || prefix.Bits() > 30 || prefix.Masked().String() != input.ManagementCIDR {
		return input, errors.New("invalid management IPv4 subnet")
	}
	if err = validBootURL(input.Option239URL, map[string]bool{"http": true, "https": true}, input.ProjectID, input.BatchRef, input.StageRef, true); err != nil {
		return input, err
	}
	if len(input.Option143URIs) == 0 || len(input.Option143URIs) > 4 {
		return input, errors.New("option 143 requires one to four bootstrap URIs")
	}
	for _, uri := range input.Option143URIs {
		parsed, parseErr := url.Parse(uri)
		if parseErr != nil || parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(uri, "\r\n\x00") || parsed.String() != uri {
			return input, errors.New("option 143 URI must be https://host[:port] without path or query")
		}
		if port := parsed.Port(); port != "" {
			n, portErr := strconv.Atoi(port)
			if portErr != nil || n < 1 || n > 65535 {
				return input, errors.New("option 143 port must be 1..65535")
			}
		}
	}
	if strings.TrimSpace(input.Option60VCI) == "" || len(input.Option60VCI) > 128 || strings.ContainsAny(input.Option60VCI, "\r\n\x00") {
		return input, errors.New("invalid option 60 vendor class")
	}
	if len(input.Option43) != 2 || strings.TrimSpace(input.Option43["nos"]) == "" || strings.TrimSpace(input.Option43["version_intent"]) == "" {
		return input, errors.New("option 43 requires NOS and version intent")
	}
	for _, value := range input.Option43 {
		if len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return input, errors.New("invalid option 43 value")
		}
	}
	if input.DHCP6 != nil {
		live, _ := json.Marshal(map[string]any{"Dhcp6": map[string]any{"subnet6": []any{map[string]any{"subnet": input.DHCP6.ManagementCIDR}}}})
		if _, err = MergeBootstrap136(live, input.DHCP6.ManagementCIDR, input.ProjectID, input.DHCP6.Bindings, input.Option143URIs); err != nil {
			return input, err
		}
		input.DHCP6 = &DHCP6MockOptions{ManagementCIDR: input.DHCP6.ManagementCIDR, Bindings: append([]KeaDHCP6Reservation(nil), input.DHCP6.Bindings...)}
	}
	input.Option143URIs = append([]string(nil), input.Option143URIs...)
	input.Option43 = map[string]string{"nos": input.Option43["nos"], "version_intent": input.Option43["version_intent"]}
	return input, nil
}

// Stage validates an immutable snapshot without replacing the active offer.
// The returned digest is the commit token and binds the exact candidate.
func (m *MockKea) Stage(input DHCPOptions) (string, error) {
	cfg, err := validateOptions(input)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	key := planKey(cfg.ProjectID, cfg.BatchRef, cfg.StageRef)
	plan := m.plans[key]
	if plan == nil {
		plan = &stagedPlan{}
		m.plans[key] = plan
	}
	plan.staged = &cfg
	plan.stagedHash = digest
	return digest, nil
}

func (m *MockKea) Commit(projectID, batchRef, stageRef, digest string) error {
	if !projectPattern.MatchString(projectID) || !refPattern.MatchString(batchRef) || !refPattern.MatchString(stageRef) {
		return errors.New("invalid project, batch or stage")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	plan := m.plans[planKey(projectID, batchRef, stageRef)]
	if plan == nil || plan.staged == nil || digest == "" || plan.stagedHash != digest {
		return errors.New("no matching staged Kea configuration")
	}
	cfg := *plan.staged
	plan.active = &cfg
	plan.activeHash = plan.stagedHash
	plan.staged = nil
	plan.stagedHash = ""
	return nil
}

type DORARequest struct {
	DUID        string `json:"duid,omitempty"`
	ProjectID   string `json:"project_id"`
	BatchRef    string `json:"batch_ref"`
	StageRef    string `json:"stage_ref"`
	Interface   string `json:"interface"`
	LinkIP      string `json:"link_ip"`
	VendorClass string `json:"vendor_class"`
}

type DORAResult struct {
	Protocol      string            `json:"protocol,omitempty"`
	Address       string            `json:"address,omitempty"`
	SwitchID      string            `json:"switch_id,omitempty"`
	Option136URIs []string          `json:"option_136_uris,omitempty"`
	Outcome       string            `json:"outcome"`
	Reason        string            `json:"reason,omitempty"`
	ConfigHash    string            `json:"config_hash,omitempty"`
	Option239URL  string            `json:"option_239_url,omitempty"`
	Option143URIs []string          `json:"option_143_uris,omitempty"`
	Option60VCI   string            `json:"option_60_vci,omitempty"`
	Option43      map[string]string `json:"option_43,omitempty"`
}

// DORA returns an ACK only for a committed configuration on eth0 inside the
// management subnet with the intended vendor class. LinkIP represents the
// relay or ingress subnet selected by Kea, not the client source address
// (which is 0.0.0.0 during discovery). Other probes receive no
// ZTP options, including front-panel probes on the same IPv4 subnet.
func (m *MockKea) DORA(req DORARequest) (DORAResult, error) {
	if !projectPattern.MatchString(req.ProjectID) || !refPattern.MatchString(req.BatchRef) || !refPattern.MatchString(req.StageRef) {
		return DORAResult{}, errors.New("invalid project, batch or stage")
	}
	addr, err := netip.ParseAddr(req.LinkIP)
	if err != nil || !addr.Is4() && (addr.Is4In6() || addr.Zone() != "" || !addr.IsGlobalUnicast()) {
		return DORAResult{}, fmt.Errorf("invalid DHCP management-link address")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	plan := m.plans[planKey(req.ProjectID, req.BatchRef, req.StageRef)]
	if plan == nil || plan.active == nil {
		return DORAResult{Outcome: "no_offer", Reason: "no_active_plan"}, nil
	}
	cfg := plan.active

	if addr.Is6() {
		if cfg.DHCP6 == nil {
			return DORAResult{Outcome: "no_offer", Reason: "dhcp6_not_configured"}, nil
		}
		prefix, _ := netip.ParsePrefix(cfg.DHCP6.ManagementCIDR)
		if req.Interface != "eth0" || !prefix.Contains(addr) {
			return DORAResult{Outcome: "no_offer", Reason: "outside_management_pool"}, nil
		}
		if _, err := decodeDUID(req.DUID); err != nil {
			return DORAResult{}, err
		}
		for _, binding := range cfg.DHCP6.Bindings {
			if binding.DUID == req.DUID {
				return DORAResult{Outcome: "ack", Protocol: "dhcp6", Address: binding.Address, SwitchID: binding.SwitchID, ConfigHash: plan.activeHash, Option136URIs: append([]string(nil), cfg.Option143URIs...)}, nil
			}
		}
		return DORAResult{Outcome: "no_offer", Reason: "unknown_duid"}, nil
	}
	if req.DUID != "" {
		return DORAResult{}, errors.New("DUID is only valid on the DHCPv6 adapter")
	}
	prefix, _ := netip.ParsePrefix(cfg.ManagementCIDR)
	if req.Interface != "eth0" || !prefix.Contains(addr) {
		return DORAResult{Outcome: "no_offer", Reason: "outside_management_pool"}, nil
	}
	if req.VendorClass != cfg.Option60VCI {
		return DORAResult{Outcome: "no_offer", Reason: "vendor_class_mismatch"}, nil
	}
	return DORAResult{Outcome: "ack", ConfigHash: plan.activeHash, Option239URL: cfg.Option239URL, Option143URIs: append([]string(nil), cfg.Option143URIs...), Option60VCI: cfg.Option60VCI, Option43: map[string]string{"nos": cfg.Option43["nos"], "version_intent": cfg.Option43["version_intent"]}}, nil
}

// Package wiring compares independently frozen BOM expectations with retained
// host and switch observations. It cannot execute, authorize or publish gates.
package wiring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

const Version = "keel.wiring/v1"
const RuleVersion = "wiring-three-source/v1"
const MaxCaptureBytes = 2 << 20
const MaxPorts = 128

var scopeDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var entityID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]{0,14}$`)
var bdf = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
var field = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,120}$`)

type ExpectedPort struct {
	PhysicalPortID   string   `json:"physical_port_id"`
	ComponentID      string   `json:"component_id"`
	PortIndex        string   `json:"port_index"`
	Interface        string   `json:"interface"`
	PermanentMAC     string   `json:"permanent_mac"`
	BDF              string   `json:"bdf"`
	Rail             string   `json:"rail"`
	CableID          string   `json:"cable_id"`
	SwitchID         string   `json:"switch_id"`
	SwitchSerial     string   `json:"switch_serial"`
	SwitchChassisIDs []string `json:"switch_chassis_ids"`
	SwitchPort       string   `json:"switch_port"`
}
type Expectation struct {
	HealthPolicy *HealthPolicy  `json:"health_policy,omitempty"`
	Version      string         `json:"version"`
	ProjectID    string         `json:"project_id"`
	AssetID      string         `json:"asset_id"`
	MachineID    string         `json:"machine_id"`
	Serial       string         `json:"serial"`
	Hostname     string         `json:"hostname"`
	ScopeHash    string         `json:"scope_hash"`
	Ports        []ExpectedPort `json:"ports"`
}

func (e Expectation) Validate() error {
	if e.HealthPolicy.Validate() != nil || e.Version != Version || !entityID.MatchString(e.ProjectID) || !entityID.MatchString(e.AssetID) || !entityID.MatchString(e.MachineID) || !scopeDigest.MatchString(e.ScopeHash) || !field.MatchString(e.Serial) || !field.MatchString(e.Hostname) || len(e.Ports) < 1 || len(e.Ports) > MaxPorts {
		return errors.New("invalid wiring expectation")
	}
	seen := map[string]bool{}
	interfaces := map[string]bool{}
	peers := map[string]bool{}
	for _, p := range e.Ports {
		peer := p.SwitchID + ":" + p.SwitchPort
		if !entityID.MatchString(p.PhysicalPortID) || !entityID.MatchString(p.ComponentID) || !entityID.MatchString(p.CableID) || !entityID.MatchString(p.SwitchID) || !field.MatchString(p.PortIndex) || !field.MatchString(p.SwitchSerial) || !field.MatchString(p.SwitchPort) || !interfaceName.MatchString(p.Interface) || !bdf.MatchString(p.BDF) || canonicalMAC(p.PermanentMAC) == "" || seen[p.PhysicalPortID] || interfaces[p.Interface] || peers[peer] || len(p.SwitchChassisIDs) > 64 || (p.Rail != "" && !field.MatchString(p.Rail)) || len(p.Rail) > 40 {
			return errors.New("invalid or ambiguous wiring port identity")
		}
		for _, mac := range p.SwitchChassisIDs {
			if canonicalMAC(mac) == "" {
				return errors.New("switch chassis identities must be approved MAC bindings")
			}
		}
		seen[p.PhysicalPortID] = true
		interfaces[p.Interface] = true
		peers[peer] = true
	}
	return nil
}
func canonicalMAC(value string) string {
	mac, e := net.ParseMAC(value)
	if e != nil || len(mac) != 6 || mac[0]&1 != 0 {
		return ""
	}
	allZero := true
	for _, b := range mac {
		allZero = allZero && b == 0
	}
	if allZero {
		return ""
	}
	return mac.String()
}
func sameMAC(a, b string) bool { return canonicalMAC(a) != "" && canonicalMAC(a) == canonicalMAC(b) }
func Hash(raw []byte) string   { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func Description(serial, rail, index string) string {
	// Explicit keyed fields avoid ambiguous substring/SN-prefix comparisons.
	raw, _ := json.Marshal(struct {
		Serial string `json:"sn"`
		Rail   string `json:"rail"`
		Port   string `json:"port"`
	}{serial, rail, index})
	return string(raw)
}

type Identity struct {
	ChassisID string `json:"chassis_id"`
	PortID    string `json:"port_id"`
	Sysname   string `json:"sysname"`
	Sysdesc   string `json:"sysdesc"`
}
type HostPort struct {
	Health         *HealthObservation   `json:"health,omitempty"`
	PhysicalPortID string               `json:"physical_port_id"`
	Interface      string               `json:"interface"`
	BDF            string               `json:"bdf"`
	PermanentMAC   string               `json:"permanent_mac"`
	CurrentMAC     string               `json:"current_mac"`
	LinkUp         *bool                `json:"link_up"`
	LLDPReadable   bool                 `json:"lldp_readable"`
	WindowComplete bool                 `json:"window_complete"`
	Advertised     *Identity            `json:"advertised"`
	Neighbors      []fabricmon.Neighbor `json:"neighbors"`
}
type Capture struct {
	Stopped                bool       `json:"stopped"`
	AdminRestored          bool       `json:"admin_restored"`
	ConfigurationPreserved bool       `json:"configuration_preserved"`
	Version                string     `json:"version"`
	Origin                 string     `json:"origin"`
	ProjectID              string     `json:"project_id"`
	MachineID              string     `json:"machine_id"`
	ScopeHash              string     `json:"scope_hash"`
	Serial                 string     `json:"serial"`
	StartedAt              time.Time  `json:"started_at"`
	AdvertisingStoppedAt   time.Time  `json:"advertising_stopped_at"`
	AdvertisingAt          time.Time  `json:"advertising_at"`
	CollectedAt            time.Time  `json:"collected_at"`
	Ports                  []HostPort `json:"ports"`
	Missing                []string   `json:"missing"`
}
type SwitchEvidence struct {
	Origin   string              `json:"origin"`
	Snapshot *fabricmon.Snapshot `json:"snapshot"`
}
type Finding struct {
	Step           int    `json:"step"`
	PhysicalPortID string `json:"physical_port_id"`
	CableID        string `json:"cable_id"`
	Verdict        string `json:"verdict"`
	Category       string `json:"category"`
	Code           string `json:"code"`
	Expected       any    `json:"expected,omitempty"`
	Actual         any    `json:"actual,omitempty"`
}
type Result struct {
	HealthChecks []HealthCheck `json:"health_checks,omitempty"`
	Version      string        `json:"version"`
	RuleVersion  string        `json:"rule_version"`
	Step2        string        `json:"step2"`
	Step3        string        `json:"step3"`
	Findings     []Finding     `json:"findings"`
}

// Package pcitopology compares retained observations with independently
// published model expectations. It never configures hardware or invents a
// baseline from enumeration order.
package pcitopology

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const Version = "keel.pcie-topology/v1"
const RuleVersion = "pcie-topology/v1"
const MaxNodes = 4096

var ErrInvalid = errors.New("invalid PCIe topology evidence")
var bdfPattern = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
var rootPattern = regexp.MustCompile(`^pci[0-9a-f]{4}:[0-9a-f]{2}$`)

func CanonicalBDF(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) == 16 && strings.HasPrefix(v, "0000") {
		v = v[4:]
	}
	if !bdfPattern.MatchString(v) {
		return ""
	}
	return v
}

// Upstream is root-to-parent, excluding the endpoint. Root identifies a sysfs
// host bridge; it is not a guessed root-complex grouping from bus numbers.
type Node struct {
	BDF          string   `json:"bdf"`
	Root         string   `json:"root"`
	Upstream     []string `json:"upstream"`
	Class        string   `json:"class"`
	PCIeType     string   `json:"pcie_type"`
	NUMA         *int     `json:"numa"`
	Socket       *int     `json:"socket"`
	PhysicalSlot string   `json:"physical_slot"`
	MaxSpeedGT   *float64 `json:"max_speed_gt"`
	MaxWidth     *int     `json:"max_width"`
	SpeedGT      *float64 `json:"speed_gt"`
	Width        *int     `json:"width"`
}
type Device struct {
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	BDF      string `json:"bdf"`
	Identity string `json:"identity"`
	Serial   string `json:"serial,omitempty"`
}
type Affinity struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Observed string `json:"observed"`
	Computed string `json:"computed"`
}
type NVLink struct {
	GPU   string `json:"gpu"`
	Index int    `json:"index"`
	State string `json:"state"`
}
type Snapshot struct {
	Version  string     `json:"version"`
	Nodes    []Node     `json:"nodes"`
	Devices  []Device   `json:"devices"`
	Affinity []Affinity `json:"affinity"`
	NVLinks  []NVLink   `json:"nvlinks"`
	Missing  []string   `json:"missing"`
}
type Provenance struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	SHA256    string `json:"sha256"`
}
type ExpectedDevice struct {
	Key      string   `json:"key"`
	Kind     string   `json:"kind"`
	PCIESlot string   `json:"pcie_slot"`
	Root     string   `json:"root"`
	Upstream []string `json:"upstream"`
	NUMA     *int     `json:"numa"`
	SpeedGT  float64  `json:"speed_gt"`
	Width    int      `json:"width"`
}
type ExpectedNVLink struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Links int    `json:"links"`
}
type Expectation struct {
	Version    string           `json:"version"`
	Provenance Provenance       `json:"provenance"`
	Devices    []ExpectedDevice `json:"devices"`
	NVLinks    []ExpectedNVLink `json:"nvlinks"`
}

// Binding comes from stable BOM components/ports and retained identity evidence,
// never from sorting the BDFs being validated. Several ports may share a NIC.
type Binding struct {
	Key         string `json:"key"`
	ComponentID string `json:"component_id"`
	Kind        string `json:"kind"`
	BDF         string `json:"bdf"`
}
type Diff struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Target   string `json:"target"`
	Verdict  string `json:"verdict"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
}
type Evaluation struct {
	RuleVersion string `json:"rule_version"`
	Verdict     string `json:"verdict"`
	Diffs       []Diff `json:"diffs"`
}

func DecodeExpectation(raw []byte) (*Expectation, error) {
	if len(raw) == 0 || len(raw) > 512<<10 {
		return nil, ErrInvalid
	}
	var out Expectation
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.Validate() != nil {
		return nil, ErrInvalid
	}
	// All dimensions are explicit. Keeping the canonical typed form identical
	// to the published JSON also makes the DB model-to-scope guard exact.
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrInvalid
	}
	canonical, _ := json.Marshal(value)
	typed, _ := json.Marshal(out)
	var typedValue any
	_ = json.Unmarshal(typed, &typedValue)
	typed, _ = json.Marshal(typedValue)
	if !bytes.Equal(canonical, typed) {
		return nil, ErrInvalid
	}
	return &out, nil
}
func validSpeed(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && (v == 2.5 || v == 5 || v == 8 || v == 16 || v == 32 || v == 64 || v == 128)
}
func validWidth(v int) bool {
	return v == 1 || v == 2 || v == 4 || v == 8 || v == 12 || v == 16 || v == 32
}
func (e *Expectation) Validate() error {
	if e == nil || e.Version != Version || (e.Provenance.Kind != "vendor_spec" && e.Provenance.Kind != "approved_golden") || len(strings.TrimSpace(e.Provenance.Reference)) < 3 || len(e.Provenance.Reference) > 1000 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(e.Provenance.SHA256) || len(e.Devices) == 0 || len(e.Devices) > 256 || e.NVLinks == nil || len(e.NVLinks) > 256*256 {
		return ErrInvalid
	}
	keys := map[string]string{}
	slots := map[string]bool{}
	for _, d := range e.Devices {
		if strings.TrimSpace(d.Key) != d.Key || d.Key == "" || len(d.Key) > 64 || keys[d.Key] != "" || (d.Kind != "gpu" && d.Kind != "nic") || d.PCIESlot == "" || strings.TrimSpace(d.PCIESlot) != d.PCIESlot || len(d.PCIESlot) > 64 || slots[d.PCIESlot] || !rootPattern.MatchString(d.Root) || len(d.Upstream) > 32 || d.NUMA == nil || *d.NUMA < 0 || *d.NUMA > 4095 || !validSpeed(d.SpeedGT) || !validWidth(d.Width) {
			return ErrInvalid
		}
		path := map[string]bool{}
		for _, b := range d.Upstream {
			if CanonicalBDF(b) != b || b == "" || path[b] {
				return ErrInvalid
			}
			path[b] = true
		}
		keys[d.Key] = d.Kind
		slots[d.PCIESlot] = true
	}
	pairs := map[string]bool{}
	for _, n := range e.NVLinks {
		if keys[n.From] != "gpu" || keys[n.To] != "gpu" || n.From == n.To || n.Links < 0 || n.Links > 128 || pairs[n.From+":"+n.To] {
			return ErrInvalid
		}
		pairs[n.From+":"+n.To] = true
	}
	// Every GPU pair is explicit, including absence of an NVLink fabric.
	for a, kind := range keys {
		if kind != "gpu" {
			continue
		}
		for b, other := range keys {
			if other == "gpu" && a != b && !pairs[a+":"+b] {
				return ErrInvalid
			}
		}
	}
	return nil
}

func (s *Snapshot) Validate() error {
	if s == nil || s.Version != Version || len(s.Nodes) > MaxNodes || len(s.Devices) > 384 || len(s.Affinity) > 384*384 || len(s.NVLinks) > 256*128 || len(s.Missing) > 256 {
		return ErrInvalid
	}
	nodes := map[string]bool{}
	for _, n := range s.Nodes {
		if n.BDF == "" || CanonicalBDF(n.BDF) != n.BDF || nodes[n.BDF] || !rootPattern.MatchString(n.Root) || len(n.Upstream) > 32 || len(n.PhysicalSlot) > 64 || len(n.Class) > 8 || len(n.PCIeType) > 32 || n.NUMA != nil && (*n.NUMA < 0 || *n.NUMA > 4095) || n.Socket != nil && (*n.Socket < 0 || *n.Socket > 4095) || n.MaxWidth != nil && !validWidth(*n.MaxWidth) || n.Width != nil && !validWidth(*n.Width) || n.MaxSpeedGT != nil && !validSpeed(*n.MaxSpeedGT) || n.SpeedGT != nil && !validSpeed(*n.SpeedGT) {
			return ErrInvalid
		}
		seen := map[string]bool{n.BDF: true}
		for _, b := range n.Upstream {
			if b == "" || CanonicalBDF(b) != b || seen[b] {
				return ErrInvalid
			}
			seen[b] = true
		}
		nodes[n.BDF] = true
	}
	byBDF := map[string]Node{}
	for _, n := range s.Nodes {
		byBDF[n.BDF] = n
	}
	for _, n := range s.Nodes {
		for i, parent := range n.Upstream {
			p, ok := byBDF[parent]
			if !ok || p.Root != n.Root || !equalPath(p.Upstream, n.Upstream[:i]) {
				return ErrInvalid
			}
		}
	}
	labels := map[string]bool{}
	bdfs := map[string]bool{}
	for _, d := range s.Devices {
		if (d.Kind != "gpu" && d.Kind != "nic") || len(d.Label) > 32 || d.Label == "" || labels[d.Label] || !nodes[d.BDF] || bdfs[d.BDF] || len(d.Serial) > 120 || len(d.Identity) > 120 || d.Identity == "" {
			return ErrInvalid
		}
		labels[d.Label] = true
		bdfs[d.BDF] = true
	}
	pairs := map[string]bool{}
	for _, a := range s.Affinity {
		if !labels[a.From] || !labels[a.To] || pairs[a.From+":"+a.To] || !validAffinity(a.Observed) || a.Computed != "unknown" && !validAffinity(a.Computed) || (a.From == a.To && a.Observed != "X") || (a.From != a.To && a.Observed == "X") {
			return ErrInvalid
		}
		pairs[a.From+":"+a.To] = true
	}
	links := map[string]bool{}
	for _, l := range s.NVLinks {
		key := l.GPU + ":" + strconv.Itoa(l.Index)
		if !labels[l.GPU] || !gpuLabel.MatchString(l.GPU) || l.Index < 0 || l.Index >= 128 || (l.State != "active" && l.State != "inactive" && l.State != "unknown") || links[key] {
			return ErrInvalid
		}
		links[key] = true
	}
	for _, m := range s.Missing {
		if m == "" || len(m) > 256 {
			return ErrInvalid
		}
	}
	return nil
}

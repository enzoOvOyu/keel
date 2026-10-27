// Package precollect normalizes retained read-only host observations. It never
// configures interfaces, starts LLDP or publishes a verified topology/mapping.
package precollect

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Version = "keel.precollect/v1"
const RuleVersion = "precollect-g1/v1"
const MaxBundleBytes = 2 << 20
const ChunkBytes = 8192

var ErrInvalid = errors.New("invalid precollection evidence")
var ID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var pci = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
var iface = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
var gpuUUID = regexp.MustCompile(`^GPU-[A-Fa-f0-9-]{8,80}$`)

type Probe struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	State  string `json:"state"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}
type InterfaceSeed struct {
	Name            string `json:"name"`
	BDF             string `json:"bdf"`
	PFBDF           string `json:"pf_bdf"`
	VirtualFunction bool   `json:"virtual_function"`
}
type NetConfig struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	MTU     int    `json:"mtu"`
	AdminUp bool   `json:"admin_up"`
}
type Bundle struct {
	ConfigBefore []NetConfig     `json:"config_before"`
	ConfigAfter  []NetConfig     `json:"config_after"`
	Version      string          `json:"version"`
	Origin       string          `json:"origin"`
	StartedAt    time.Time       `json:"started_at"`
	CollectedAt  time.Time       `json:"collected_at"`
	Serial       string          `json:"serial"`
	Arch         string          `json:"arch"`
	OSRelease    string          `json:"os_release"`
	Interfaces   []InterfaceSeed `json:"interfaces"`
	RDMADevices  []string        `json:"rdma_devices"`
	Probes       []Probe         `json:"probes"`
	Missing      []string        `json:"missing"`
}
type GPU struct {
	UUID           string `json:"uuid"`
	BDF            string `json:"bdf"`
	MemoryMiB      *int64 `json:"memory_mib"`
	DriverVersion  string `json:"driver_version"`
	UncorrectedECC *int64 `json:"uncorrected_ecc"`
	Present        bool   `json:"present"`
}
type NIC struct {
	Name            string   `json:"name"`
	BDF             string   `json:"bdf"`
	PFBDF           string   `json:"pf_bdf"`
	VirtualFunction bool     `json:"virtual_function"`
	PermanentMAC    string   `json:"permanent_mac"`
	Link            string   `json:"link"`
	SpeedMbps       *int64   `json:"speed_mbps"`
	Driver          string   `json:"driver"`
	Firmware        string   `json:"firmware"`
	RDMALinks       []string `json:"rdma_links"`
	DOMState        string   `json:"dom_state"`
	ModulePresent   *bool    `json:"module_present"`
	TemperatureC    *float64 `json:"temperature_c"`
	RxPowerDBm      *float64 `json:"rx_power_dbm"`
	TxPowerDBm      *float64 `json:"tx_power_dbm"`
}
type Report struct {
	ConfigUnchanged bool      `json:"config_unchanged"`
	Version         string    `json:"version"`
	Origin          string    `json:"origin"`
	Serial          string    `json:"serial"`
	Arch            string    `json:"arch"`
	OSRelease       string    `json:"os_release"`
	CollectedAt     time.Time `json:"collected_at"`
	GPUs            []GPU     `json:"gpus"`
	NICs            []NIC     `json:"nics"`
	RDMADevices     []string  `json:"rdma_devices"`
	NVLinkStatus    string    `json:"nvlink_status"`
	PCITopology     string    `json:"pci_topology"`
	GPUTopology     string    `json:"gpu_topology"`
	Missing         []string  `json:"missing"`
}

func Hash(raw []byte) string       { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func ValidInterface(v string) bool { return iface.MatchString(v) }
func CanonicalBDF(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) == 16 && strings.HasPrefix(v, "0000") {
		v = v[4:]
	}
	if !pci.MatchString(v) {
		return ""
	}
	return v
}
func CanonicalMAC(v string) string {
	m, e := net.ParseMAC(strings.TrimSpace(v))
	if e != nil || len(m) != 6 || m[0]&1 != 0 || m.String() == "00:00:00:00:00:00" {
		return ""
	}
	return m.String()
}
func Decode(raw []byte, now time.Time) (*Bundle, error) {
	if len(raw) == 0 || len(raw) > MaxBundleBytes {
		return nil, ErrInvalid
	}
	var b Bundle
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	if b.Version != Version || (b.Origin != "sim" && b.Origin != "real") || b.StartedAt.IsZero() || b.CollectedAt.Before(b.StartedAt) || b.CollectedAt.After(now.Add(5*time.Second)) || b.CollectedAt.Sub(b.StartedAt) > 30*time.Second || len(b.Serial) > 120 || len(b.OSRelease) > 16384 || len(b.Interfaces) > 128 || len(b.RDMADevices) > 128 || len(b.Probes) > 600 || len(b.Missing) > 128 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, p := range b.Interfaces {
		if !iface.MatchString(p.Name) || p.BDF == "" || CanonicalBDF(p.BDF) != p.BDF || p.PFBDF != "" && CanonicalBDF(p.PFBDF) != p.PFBDF || p.VirtualFunction && p.PFBDF == "" || seen[p.Name] {
			return nil, ErrInvalid
		}
		seen[p.Name] = true
	}
	probes := map[string]bool{}
	for _, p := range b.Probes {
		switch p.Kind {
		case "gpu", "nvlink", "gpu_topology", "pci_topology", "rdma_links", "ibv_devices":
			if p.Target != "" {
				return nil, ErrInvalid
			}
		case "nic_permanent", "nic_link", "nic_driver", "nic_dom":
			if !seen[p.Target] {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrInvalid
		}
		key := p.Kind + ":" + p.Target
		if probes[key] || len(p.Stdout) > 262144 || len(p.Stderr) > 4096 {
			return nil, ErrInvalid
		}
		probes[key] = true
		switch p.State {
		case "ready", "tool_unavailable", "timeout", "output_limit", "read_failed", "cancelled":
		default:
			return nil, ErrInvalid
		}
	}
	for _, v := range b.RDMADevices {
		if !iface.MatchString(v) {
			return nil, ErrInvalid
		}
	}
	for _, v := range b.Missing {
		if v == "" || len(v) > 160 {
			return nil, ErrInvalid
		}
	}
	for _, config := range [][]NetConfig{b.ConfigBefore, b.ConfigAfter} {
		if len(config) == 0 || len(config) > 2048 {
			return nil, ErrInvalid
		}
		seen := map[string]bool{}
		for _, n := range config {
			if !iface.MatchString(n.Name) || seen[n.Name] || n.MTU < 0 || n.MTU > 1<<20 || len(n.MAC) > 64 {
				return nil, ErrInvalid
			}
			seen[n.Name] = true
		}
	}
	return &b, nil
}
func decimal(v string) *int64 {
	n, e := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if e != nil || n < 0 {
		return nil
	}
	return &n
}
func field(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func measurement(text, key, unit string) *float64 {
	value := field(text, key)
	at := strings.LastIndex(value, unit)
	if at < 0 {
		return nil
	}
	part := strings.Fields(strings.TrimSpace(value[:at]))
	if len(part) == 0 {
		return nil
	}
	number, e := strconv.ParseFloat(part[len(part)-1], 64)
	if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return &number
}
func Normalize(b *Bundle) (*Report, error) {
	if b == nil {
		return nil, ErrInvalid
	}
	r := &Report{Version: Version, Origin: b.Origin, Serial: b.Serial, Arch: b.Arch, OSRelease: b.OSRelease, CollectedAt: b.CollectedAt, GPUs: []GPU{}, NICs: []NIC{}, RDMADevices: append([]string{}, b.RDMADevices...), Missing: append([]string{}, b.Missing...)}
	before, _ := json.Marshal(b.ConfigBefore)
	after, _ := json.Marshal(b.ConfigAfter)
	r.ConfigUnchanged = len(b.ConfigBefore) > 0 && bytes.Equal(before, after)
	probes := map[string]Probe{}
	for _, p := range b.Probes {
		probes[p.Kind+":"+p.Target] = p
	}
	ready := func(kind, target string) string {
		p, ok := probes[kind+":"+target]
		if !ok || p.State != "ready" {
			return ""
		}
		return strings.TrimSpace(p.Stdout)
	}
	if p, ok := probes["gpu:"]; !ok || p.State != "ready" {
		r.Missing = append(r.Missing, "gpu_inventory_unavailable")
	}
	gpu := ready("gpu", "")
	if gpu != "" {
		csvReader := csv.NewReader(strings.NewReader(gpu))
		csvReader.TrimLeadingSpace = true
		csvReader.FieldsPerRecord = 5
		rows, e := csvReader.ReadAll()
		if e != nil || len(rows) > 256 {
			return nil, ErrInvalid
		}
		seen := map[string]bool{}
		bdfs := map[string]bool{}
		for _, row := range rows {
			uuid := strings.TrimSpace(row[0])
			bdf := CanonicalBDF(row[1])
			if !gpuUUID.MatchString(uuid) || bdf == "" || seen[uuid] || bdfs[bdf] {
				return nil, ErrInvalid
			}
			seen[uuid] = true
			bdfs[bdf] = true
			driver := strings.TrimSpace(row[3])
			if len(driver) > 120 {
				return nil, ErrInvalid
			}
			r.GPUs = append(r.GPUs, GPU{UUID: uuid, BDF: bdf, MemoryMiB: decimal(row[2]), DriverVersion: driver, UncorrectedECC: decimal(row[4]), Present: true})
		}
	}
	r.NVLinkStatus = ready("nvlink", "")
	r.PCITopology = ready("pci_topology", "")
	r.GPUTopology = ready("gpu_topology", "")
	var rdma []struct {
		Ifname string `json:"ifname"`
		Netdev string `json:"netdev"`
		Port   int    `json:"port"`
		State  string `json:"state"`
	}
	rdmaRaw := ready("rdma_links", "")
	if rdmaRaw != "" && json.Unmarshal([]byte(rdmaRaw), &rdma) != nil {
		r.Missing = append(r.Missing, "rdma_link_json_invalid")
		rdma = nil
	}
	devices := map[string]bool{}
	ibv := ready("ibv_devices", "")
	for _, device := range b.RDMADevices {
		for _, line := range strings.Split(ibv, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == device {
				devices[device] = true
			}
		}
	}
	for _, seed := range b.Interfaces {
		n := NIC{Name: seed.Name, BDF: seed.BDF, PFBDF: seed.PFBDF, VirtualFunction: seed.VirtualFunction, RDMALinks: []string{}, DOMState: "unknown"}
		n.PermanentMAC = CanonicalMAC(field(ready("nic_permanent", n.Name), "Permanent address"))
		link := ready("nic_link", n.Name)
		n.Link = field(link, "Link detected")
		speed := strings.TrimSuffix(field(link, "Speed"), "Mb/s")
		n.SpeedMbps = decimal(speed)
		driver := ready("nic_driver", n.Name)
		n.Driver = field(driver, "driver")
		n.Firmware = field(driver, "firmware-version")
		if bdf := CanonicalBDF(field(driver, "bus-info")); bdf != "" && bdf != n.BDF {
			r.Missing = append(r.Missing, "nic_driver_bdf_mismatch:"+n.Name)
		}
		for _, entry := range rdma {
			if entry.Netdev == n.Name && devices[entry.Ifname] && iface.MatchString(entry.Ifname) && entry.Port > 0 && entry.Port <= 255 {
				n.RDMALinks = append(n.RDMALinks, entry.Ifname+"/"+strconv.Itoa(entry.Port))
			}
		}
		if p, ok := probes["nic_dom:"+n.Name]; ok {
			n.DOMState = p.State
			if p.State == "ready" {
				text := p.Stdout
				n.TemperatureC = measurement(text, "Module temperature", "degrees C")
				n.RxPowerDBm = measurement(text, "Receiver signal average optical power", "dBm")
				n.TxPowerDBm = measurement(text, "Laser output power", "dBm")
				if n.TemperatureC != nil && n.RxPowerDBm != nil && n.TxPowerDBm != nil {
					present := true
					n.ModulePresent = &present
				} else {
					n.DOMState = "unsupported"
				}
			}
		}
		r.NICs = append(r.NICs, n)
	}
	return r, nil
}

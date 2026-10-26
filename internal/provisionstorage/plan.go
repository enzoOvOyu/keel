// Package provisionstorage derives an explicit single-disk layout from frozen
// intent and verified read-only facts. Plans are data, never write permission.
package provisionstorage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

const IntentSchema = "keel.install-storage-intent/v1"
const PlanSchema = "keel.install-storage-plan/v1"
const Layout = "uefi-gpt-ext4/v1"
const MiB int64 = 1 << 20
const MinimumRootBytes int64 = 16 << 30
const MaximumDiskBytes int64 = 1 << 50

var ErrDenied = errors.New("explicit installation storage plan unavailable")
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var diskPath = regexp.MustCompile(`^/dev/(sd[a-z]+|nvme[0-9]+n[0-9]+|vd[a-z]+|xvd[a-z]+)$`)
var deviceNumber = regexp.MustCompile(`^(0|[1-9][0-9]{0,3}):(0|[1-9][0-9]{0,6})$`)

// Intent comes from an independent choice, never the first detected /dev node.
// A later approval must bind the whole plan digest and exact target attempt.
type Intent struct {
	Schema            string `json:"schema"`
	Layout            string `json:"layout"`
	WWID              string `json:"wwid"`
	Serial            string `json:"serial_number"`
	SizeBytes         int64  `json:"size_bytes"`
	LogicalBlockBytes int    `json:"logical_block_bytes"`
	EraseSelectedDisk bool   `json:"erase_selected_disk"`
}
type Device struct {
	Node              string `json:"node"`
	Number            string `json:"device_number"`
	Sequence          uint64 `json:"disk_sequence"`
	WWID              string `json:"wwid"`
	Serial            string `json:"serial_number"`
	SizeBytes         int64  `json:"size_bytes"`
	LogicalBlockBytes int    `json:"logical_block_bytes"`
}
type Partition struct {
	Number     int    `json:"number"`
	Kind       string `json:"kind"`
	Filesystem string `json:"filesystem"`
	MountPoint string `json:"mount_point"`
	StartBytes int64  `json:"start_bytes"`
	SizeBytes  int64  `json:"size_bytes"`
}
type Plan struct {
	Schema         string                  `json:"schema"`
	Spec           provisionpreflight.Spec `json:"spec"`
	Intent         Intent                  `json:"intent"`
	EvidenceSHA256 string                  `json:"preflight_report_sha256"`
	Device         Device                  `json:"device"`
	Partitions     []Partition             `json:"partitions"`
	SHA256         string                  `json:"plan_sha256"`
}

func boundedIdentity(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}
func (i Intent) Validate() error {
	if i.Schema != IntentSchema || i.Layout != Layout || !boundedIdentity(i.WWID, 512) || !boundedIdentity(i.Serial, 256) || i.SizeBytes > MaximumDiskBytes || i.SizeBytes < MinimumRootBytes+514*MiB || (i.LogicalBlockBytes != 512 && i.LogicalBlockBytes != 4096) || i.SizeBytes%int64(i.LogicalBlockBytes) != 0 || !i.EraseSelectedDisk {
		return ErrDenied
	}
	return nil
}
func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func seal(p *Plan) error {
	p.SHA256 = ""
	raw, err := json.Marshal(p)
	if err != nil {
		return ErrDenied
	}
	p.SHA256 = hash(raw)
	return nil
}

// Build requires the caller's independently frozen expectation and raw report.
// EvaluateReport recomputes all hardware results; no client-supplied PASS is
// trusted. Authentication and independent approval remain controller duties.
func Build(expected provisionpreflight.Spec, rawReport []byte, intent Intent, now time.Time) (*Plan, error) {
	if intent.Validate() != nil {
		return nil, ErrDenied
	}
	report, err := provisionpreflight.EvaluateReport(expected, rawReport, now)
	if err != nil || report.Status != "PASS" || report.Observation.Firmware != "uefi" {
		return nil, ErrDenied
	}
	selected, err := selectDevice(report.Observation.Disks, intent)
	if err != nil {
		return nil, err
	}
	partitions, err := layout(selected)
	if err != nil {
		return nil, err
	}
	expected.ExpiresAt = expected.ExpiresAt.UTC()
	p := &Plan{Schema: PlanSchema, Spec: expected, Intent: intent, EvidenceSHA256: hash(rawReport), Device: selected, Partitions: partitions}
	if seal(p) != nil {
		return nil, ErrDenied
	}
	return p, nil
}
func selectDevice(disks []provisionpreflight.Disk, intent Intent) (Device, error) {
	var selected Device
	matches := 0
	names := map[string]bool{}
	numbers := map[string]bool{}
	for _, disk := range disks {
		if !diskPath.MatchString(disk.Device) || names[disk.Device] {
			return Device{}, ErrDenied
		}
		names[disk.Device] = true
		if disk.DeviceNumber != "" {
			if !validDeviceNumber(disk.DeviceNumber) || numbers[disk.DeviceNumber] {
				return Device{}, ErrDenied
			}
			numbers[disk.DeviceNumber] = true
		}
		if disk.WWID != intent.WWID {
			continue
		}
		matches++
		if disk.Serial != intent.Serial || disk.SizeBytes != intent.SizeBytes || disk.LogicalBlockBytes != intent.LogicalBlockBytes || disk.DiskSequence == 0 || !validDeviceNumber(disk.DeviceNumber) || disk.Healthy == nil || !*disk.Healthy || disk.ReadOnly == nil || *disk.ReadOnly || disk.Removable == nil || *disk.Removable || disk.InUse == nil || *disk.InUse {
			return Device{}, ErrDenied
		}
		selected = Device{Node: disk.Device, Number: disk.DeviceNumber, Sequence: disk.DiskSequence, WWID: disk.WWID, Serial: disk.Serial, SizeBytes: disk.SizeBytes, LogicalBlockBytes: disk.LogicalBlockBytes}
	}
	if matches != 1 {
		return Device{}, ErrDenied
	}
	return selected, nil
}
func validDeviceNumber(value string) bool {
	if !deviceNumber.MatchString(value) {
		return false
	}
	var major, minor uint32
	if _, err := fmt.Sscanf(value, "%d:%d", &major, &minor); err != nil {
		return false
	}
	return major < 4096 && minor < 1<<20 && fmt.Sprintf("%d:%d", major, minor) == value
}
func layout(d Device) ([]Partition, error) {
	// Both ends retain a full MiB for protective MBR/GPT headers and entries;
	// every boundary is MiB-aligned for both 512-byte and 4Kn logical sectors.
	rootStart := 513 * MiB
	rootEnd := (d.SizeBytes - MiB) / MiB * MiB
	if rootEnd-rootStart < MinimumRootBytes {
		return nil, ErrDenied
	}
	return []Partition{
		{Number: 1, Kind: "efi-system", Filesystem: "fat32", MountPoint: "/boot/efi", StartBytes: MiB, SizeBytes: 512 * MiB},
		{Number: 2, Kind: "linux-filesystem", Filesystem: "ext4", MountPoint: "/", StartBytes: rootStart, SizeBytes: rootEnd - rootStart},
	}, nil
}

// Validate checks intrinsic plan bindings and geometry. It does not authenticate
// approval and cannot make the historical evidence a fresh device observation.
func (p Plan) Validate(now time.Time) error {
	if p.Schema != PlanSchema || p.Spec.Validate(now) != nil || p.Intent.Validate() != nil || !digest.MatchString(p.EvidenceSHA256) || !digest.MatchString(p.SHA256) {
		return ErrDenied
	}
	d := p.Device
	if !diskPath.MatchString(d.Node) || !validDeviceNumber(d.Number) || d.Sequence == 0 || d.WWID != p.Intent.WWID || d.Serial != p.Intent.Serial || d.SizeBytes != p.Intent.SizeBytes || d.LogicalBlockBytes != p.Intent.LogicalBlockBytes {
		return ErrDenied
	}
	expected, err := layout(d)
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(p.Partitions)
	if err != nil || !bytes.Equal(a, b) {
		return ErrDenied
	}
	prior := p.SHA256
	if seal(&p) != nil || p.SHA256 != prior {
		return ErrDenied
	}
	return nil
}

// VerifyCurrent recomputes fresh evidence and refuses renumbering, reattachment,
// changed identity/geometry/usage/health, expiry or changed frozen expectations.
// Even success is not a write permit; the live lease and one-time approval must
// be checked immediately before any destructive action by the installer.
func VerifyCurrent(p Plan, rawReport []byte, now time.Time) error {
	if p.Validate(now) != nil {
		return ErrDenied
	}
	report, err := provisionpreflight.EvaluateReport(p.Spec, rawReport, now)
	if err != nil || report.Status != "PASS" || report.Observation.Firmware != "uefi" {
		return ErrDenied
	}
	selected, err := selectDevice(report.Observation.Disks, p.Intent)
	if err != nil || selected != p.Device {
		return ErrDenied
	}
	return nil
}

// Decode accepts the producer's exact canonical JSON, rejecting aliases,
// duplicate/unknown fields and trailing documents without permissive decoding.
func Decode(raw []byte, now time.Time) (*Plan, error) {
	if len(raw) < 1 || len(raw) > 128<<10 || !utf8.Valid(raw) {
		return nil, ErrDenied
	}
	var p Plan
	if json.Unmarshal(raw, &p) != nil {
		return nil, ErrDenied
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, raw) || p.Validate(now) != nil {
		return nil, ErrDenied
	}
	return &p, nil
}

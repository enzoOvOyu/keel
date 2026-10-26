// Package agentinventory defines optional observational Agent inventory. It
// grants no execution authority and cannot substitute for hardware acceptance.
package agentinventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"sort"
	"time"
)

const NetworkVersion = "agent-network-inventory/v1"
const MaxNetworkInterfaces = 64
const MaxNetworkFrameBytes = 12288

var ErrInvalid = errors.New("invalid Agent network inventory")
var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,15}$`)

type Interface struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	MTU       int    `json:"mtu"`
	AdminUp   bool   `json:"admin_up"`
	OperState string `json:"oper_state"`
}

type NetworkFrame struct {
	Version     string      `json:"version"`
	State       string      `json:"state"`
	Full        bool        `json:"full"`
	CapturedAt  time.Time   `json:"captured_at"`
	Interfaces  []Interface `json:"interfaces"`
	Removed     []int       `json:"removed"`
	FailureCode string      `json:"failure_code,omitempty"`
}

func (p Interface) Validate() error {
	if p.Index < 1 || p.Index > 1<<31-1 || !interfaceName.MatchString(p.Name) || p.MTU < 0 || p.MTU > 1<<20 {
		return ErrInvalid
	}
	if p.MAC != "" {
		mac, err := net.ParseMAC(p.MAC)
		if err != nil || (len(mac) != 6 && len(mac) != 8 && len(mac) != 20) || mac.String() != p.MAC {
			return ErrInvalid
		}
	}
	switch p.OperState {
	case "unknown", "notpresent", "down", "lowerlayerdown", "testing", "dormant", "up":
	default:
		return ErrInvalid
	}
	return nil
}

func (f NetworkFrame) Validate(now time.Time) error {
	if f.Version != NetworkVersion || f.CapturedAt.IsZero() || f.CapturedAt.Before(now.Add(-30*time.Second)) || f.CapturedAt.After(now.Add(5*time.Second)) || len(f.Interfaces) > MaxNetworkInterfaces || len(f.Removed) > MaxNetworkInterfaces {
		return ErrInvalid
	}
	if f.State == "unavailable" {
		if f.Full || len(f.Interfaces) != 0 || len(f.Removed) != 0 {
			return ErrInvalid
		}
		switch f.FailureCode {
		case "interface_read_failed", "interface_limit_exceeded", "interface_state_unavailable", "inventory_frame_limit":
			return nil
		default:
			return ErrInvalid
		}
	}
	if f.State != "ready" || f.FailureCode != "" || f.Full && len(f.Removed) != 0 {
		return ErrInvalid
	}
	seen := map[int]bool{}
	names := map[string]bool{}
	for _, p := range f.Interfaces {
		if p.Validate() != nil || seen[p.Index] || names[p.Name] {
			return ErrInvalid
		}
		seen[p.Index], names[p.Name] = true, true
	}
	for _, index := range f.Removed {
		if index < 1 || index > 1<<31-1 || seen[index] {
			return ErrInvalid
		}
		seen[index] = true
	}
	return nil
}

func DecodeNetwork(raw []byte, now time.Time) (*NetworkFrame, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxNetworkFrameBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var f NetworkFrame
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Validate(now) != nil {
		return nil, ErrInvalid
	}
	return &f, nil
}

// Merge accepts an initial full observation, then applies bounded deltas. Only
// explicit removals delete entries; a failed collection never produces a delta.
func MergeNetwork(previous []Interface, f NetworkFrame) ([]Interface, error) {
	byIndex := map[int]Interface{}
	if !f.Full {
		for _, p := range previous {
			byIndex[p.Index] = p
		}
	}
	for _, index := range f.Removed {
		if _, exists := byIndex[index]; !exists {
			return nil, ErrInvalid
		}
		delete(byIndex, index)
	}
	for _, p := range f.Interfaces {
		byIndex[p.Index] = p
	}
	if len(byIndex) > MaxNetworkInterfaces {
		return nil, ErrInvalid
	}
	out := make([]Interface, 0, len(byIndex))
	names := map[string]bool{}
	for _, p := range byIndex {
		if p.Validate() != nil || names[p.Name] {
			return nil, ErrInvalid
		}
		names[p.Name] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

func NetworkDelta(previous, current []Interface, now time.Time) NetworkFrame {
	f := NetworkFrame{Version: NetworkVersion, State: "ready", Full: previous == nil, CapturedAt: now.UTC(), Interfaces: []Interface{}, Removed: []int{}}
	byIndex := map[int]Interface{}
	for _, p := range previous {
		byIndex[p.Index] = p
	}
	for _, p := range current {
		if old, exists := byIndex[p.Index]; !exists || old != p {
			f.Interfaces = append(f.Interfaces, p)
		}
		delete(byIndex, p.Index)
	}
	for index := range byIndex {
		f.Removed = append(f.Removed, index)
	}
	sort.Slice(f.Interfaces, func(i, j int) bool { return f.Interfaces[i].Index < f.Interfaces[j].Index })
	sort.Ints(f.Removed)
	return f
}

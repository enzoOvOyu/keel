package wiring

import (
	"encoding/json"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"
)

// Invocation is an argument vector for fixed installed binaries. A plan is
// reviewable data, never a shell command or authorization to execute it.
type Invocation struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
}
type PortPlan struct {
	PhysicalPortID string     `json:"physical_port_id"`
	Interface      string     `json:"interface"`
	PermanentMAC   string     `json:"permanent_mac"`
	BDF            string     `json:"bdf"`
	Directory      string     `json:"directory"`
	Socket         string     `json:"socket"`
	Configuration  string     `json:"configuration"`
	Start          Invocation `json:"start"`
	ReadLocal      Invocation `json:"read_local"`
	ReadNeighbors  Invocation `json:"read_neighbors"`
}
type Plan struct {
	HealthPolicy *HealthPolicy `json:"health_policy,omitempty"`
	Version      string        `json:"version"`
	ScopeHash    string        `json:"scope_hash"`
	CommandID    string        `json:"command_id"`
	Ports        []PortPlan    `json:"ports"`
	Digest       string        `json:"digest"`
}

func quoteConfiguration(value string) string { raw, _ := json.Marshal(value); return string(raw) }

// Each selected physical port gets an isolated daemon: chassis and sysdesc are
// daemon-wide lldpd settings, so one shared process cannot faithfully advertise
// a different permanent MAC and SN/rail/port tuple on every NIC.
// The eventual privileged adapter must create owned directories, reject an
// existing daemon on the interface, check the current BDF/MAC before temporary
// up, retain original admin state, and prove its own process tree stopped.
func BuildPlan(expected Expectation, commandID string) (*Plan, error) {
	if expected.Validate() != nil || !entityID.MatchString(commandID) {
		return nil, errors.New("invalid fixed LLDP plan scope")
	}
	out := &Plan{HealthPolicy: expected.HealthPolicy, Version: Version, ScopeHash: expected.ScopeHash, CommandID: commandID, Ports: []PortPlan{}}
	selected := slices.Clone(expected.Ports)
	sort.Slice(selected, func(i, j int) bool { return selected[i].PhysicalPortID < selected[j].PhysicalPortID })
	for _, p := range selected {
		dir := path.Join("/run/keel/lldp", commandID, p.PhysicalPortID)
		socket := path.Join(dir, "ctl.sock")
		if len(socket) > 103 {
			return nil, errors.New("LLDP socket path exceeds bounds")
		}
		// Configuration is loaded before lldpd's automatic initial unpause. No
		// customer /etc/lldpd.conf or /etc/lldpd.d is read or modified.
		config := strings.Join([]string{
			"configure system hostname " + quoteConfiguration(expected.Hostname),
			"configure system description " + quoteConfiguration(Description(expected.Serial, p.Rail, p.PortIndex)),
			"configure system chassisid " + quoteConfiguration(canonicalMAC(p.PermanentMAC)),
			"configure ports " + p.Interface + " lldp portidsubtype local " + p.PhysicalPortID,
			"configure lldp tx-interval 1",
			"configure lldp tx-hold 4",
			"unconfigure lldp management-addresses-advertisements",
			"configure ports " + p.Interface + " lldp status rx-and-tx",
		}, "\n") + "\n"
		cli := func(kind string) Invocation {
			return Invocation{Tool: "lldpcli", Args: []string{"-u", socket, "-f", "json0", "show", kind, "ports", p.Interface, "details", "hidden"}}
		}
		out.Ports = append(out.Ports, PortPlan{PhysicalPortID: p.PhysicalPortID, Interface: p.Interface, PermanentMAC: canonicalMAC(p.PermanentMAC), BDF: p.BDF, Directory: dir, Socket: socket, Configuration: config, Start: Invocation{Tool: "lldpd", Args: []string{"-d", "-I", p.Interface, "-C", p.Interface, "-H", "0", "-u", socket, "-p", path.Join(dir, "pid"), "-O", path.Join(dir, "lldpd.conf"), "-L", "/usr/sbin/lldpcli", "-k", "-i"}}, ReadLocal: cli("interfaces"), ReadNeighbors: cli("neighbors")})
	}
	raw, _ := json.Marshal(out)
	out.Digest = Hash(raw)
	return out, nil
}

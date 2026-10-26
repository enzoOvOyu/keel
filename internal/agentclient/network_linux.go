package agentclient

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
)

func collectNetworkInterfaces() ([]agentinventory.Interface, string) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, "interface_read_failed"
	}
	return collectNetworkInterfaceState(interfaces, os.ReadFile)
}

func collectNetworkInterfaceState(interfaces []net.Interface, read func(string) ([]byte, error)) ([]agentinventory.Interface, string) {
	if len(interfaces) > agentinventory.MaxNetworkInterfaces {
		return nil, "interface_limit_exceeded"
	}
	out := make([]agentinventory.Interface, 0, len(interfaces))
	for _, p := range interfaces {
		item := agentinventory.Interface{Index: p.Index, Name: p.Name, MAC: p.HardwareAddr.String(), MTU: p.MTU, AdminUp: p.Flags&net.FlagUp != 0, OperState: "unknown"}
		// Validate the kernel name before using it as a path component.
		if item.Validate() != nil {
			return nil, "interface_state_unavailable"
		}
		state, err := read(filepath.Join("/sys/class/net", p.Name, "operstate"))
		if err != nil || len(state) > 64 {
			return nil, "interface_state_unavailable"
		}
		item.OperState = strings.TrimSpace(string(state))
		if item.Validate() != nil {
			return nil, "interface_state_unavailable"
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, ""
}

func networkHeartbeat(previous []agentinventory.Interface, now time.Time) (agentinventory.NetworkFrame, []agentinventory.Interface) {
	current, failure := collectNetworkInterfaces()
	if failure != "" {
		return agentinventory.NetworkFrame{Version: agentinventory.NetworkVersion, State: "unavailable", CapturedAt: now.UTC(), Interfaces: []agentinventory.Interface{}, Removed: []int{}, FailureCode: failure}, previous
	}
	return agentinventory.NetworkDelta(previous, current, now), current
}

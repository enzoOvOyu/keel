package agentclient

import (
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
)

func TestAgentNetworkCollectorDoesNotPublishPartialState(t *testing.T) {
	nics := []net.Interface{{Index: 2, Name: "eth1", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 2}, MTU: 9000}, {Index: 1, Name: "eth0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 1}, MTU: 1500, Flags: net.FlagUp}}
	states := map[string][]byte{"/sys/class/net/eth0/operstate": []byte("up\n"), "/sys/class/net/eth1/operstate": []byte("down\n")}
	read := func(path string) ([]byte, error) {
		if body, ok := states[path]; ok {
			return body, nil
		}
		return nil, errors.New("fixture interface disappeared")
	}
	got, failure := collectNetworkInterfaceState(nics, read)
	if failure != "" || len(got) != 2 || got[0].Index != 1 || !got[0].AdminUp || got[1].OperState != "down" {
		t.Fatal("observed kernel state lost", got, failure)
	}
	delete(states, filepath.Join("/sys/class/net", "eth1", "operstate"))
	if got, failure = collectNetworkInterfaceState(nics, read); failure != "interface_state_unavailable" || got != nil {
		t.Fatal("partial interface snapshot published", got, failure)
	}
	nics[0].Name = "../escape"
	called := false
	_, failure = collectNetworkInterfaceState(nics, func(string) ([]byte, error) { called = true; return []byte("up\n"), nil })
	if failure != "interface_state_unavailable" || called {
		t.Fatal("invalid interface name reached filesystem reader")
	}
	if _, failure = collectNetworkInterfaceState(make([]net.Interface, agentinventory.MaxNetworkInterfaces+1), read); failure != "interface_limit_exceeded" {
		t.Fatal("interface overflow silently truncated")
	}
}

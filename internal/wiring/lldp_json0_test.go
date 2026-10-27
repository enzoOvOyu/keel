package wiring

import (
	"strings"
	"testing"
)

// Synthetic data follows the upstream json0 schema. No captured production or
// vendor evidence is embedded, and these authored cases have not been run.
const json0Peer = `{"lldp":[{"interface":[{"name":"eth1","via":"LLDP","chassis":[{"id":[{"type":"mac","value":"02:00:00:00:02:01"}],"name":[{"value":"leaf-1"}],"descr":[{"value":"test leaf"}]}],"port":[{"id":[{"type":"ifname","value":"swp1"}]}]}]}]}`

func TestLLDPJSON0RetainsMultiplicityAndRejectsAmbiguousIdentity(t *testing.T) {
	result, e := ParseNeighborsJSON0([]byte(json0Peer), []string{"eth1"})
	if e != nil || len(result["eth1"]) != 1 || result["eth1"][0].PortID != "swp1" {
		t.Fatalf("%+v %v", result, e)
	}
	empty, e := ParseNeighborsJSON0([]byte(`{"lldp":[{"interface":[]}]}`), []string{"eth1"})
	if e != nil || len(empty["eth1"]) != 0 {
		t.Fatal("explicit empty readable array not retained")
	}
	for _, raw := range []string{`{}`, `{"lldp":null}`, `{"lldp":[{}]}`, `{"lldp":{"interface":{}}}`, json0Peer + `{}`, strings.Replace(json0Peer, `"name":"eth1"`, `"name":"eth1","name":"eth2"`, 1), strings.Replace(json0Peer, `"name":"eth1"`, `"name":"unselected"`, 1), strings.Replace(json0Peer, `"via":"LLDP"`, `"via":"CDP"`, 1)} {
		if _, e = ParseNeighborsJSON0([]byte(raw), []string{"eth1"}); e == nil {
			t.Fatalf("ambiguous or unsupported LLDP input admitted: %s", raw)
		}
	}
	local, e := ParseLocalJSON0([]byte(json0Peer), []string{"eth1"})
	if e != nil || local["eth1"].ChassisID != "02:00:00:00:02:01" {
		t.Fatal("local advertised identity missing")
	}
	if _, e = ParseLocalJSON0([]byte(`{"lldp":[{"interface":[]}]}`), []string{"eth1"}); e == nil {
		t.Fatal("empty local identity became complete")
	}
}

package fabricboot

import (
	"strings"
	"testing"
)

func TestManagementScriptIsBoundedAndSecretFree(t *testing.T) {
	r := ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "poc", BatchBy: "rack"}
	a := ManagementAssignment{SwitchID: "device", Serial: "SN-1", Hostname: "dc1-p1-leaf-r1-1", Address: "192.0.2.10", Gateway: r.Gateway, Batch: "poc-r1"}
	one, e := RenderManagementScript(r, a, "vault-switch-batch")
	if e != nil {
		t.Fatal(e)
	}
	two, e := RenderManagementScript(r, a, "vault-switch-batch")
	if e != nil || one.SHA256 != two.SHA256 {
		t.Fatal("non-deterministic renderer")
	}
	if !strings.Contains(one.Body, "# CUMULUS-AUTOPROVISIONING") || !strings.Contains(one.Body, "nv set interface eth0 ip address 192.0.2.10/24") || !strings.Contains(one.Body, "nv config save") || strings.Contains(one.Body, "vault-switch-batch") {
		t.Fatal("management renderer boundary violated")
	}
	a.Hostname = "a; touch /tmp/escape"
	if _, e = RenderManagementScript(r, a, "batch"); e == nil {
		t.Fatal("shell injection accepted")
	}
	a.Hostname = "valid-name"
	a.Address = "198.51.100.1"
	if _, e = RenderManagementScript(r, a, "batch"); e == nil {
		t.Fatal("foreign allocation accepted")
	}
}

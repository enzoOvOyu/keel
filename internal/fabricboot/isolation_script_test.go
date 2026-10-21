package fabricboot

import (
	"os"
	"strings"
	"testing"
)

func TestIsolationScriptGoldenContract(t *testing.T) {
	expected, err := os.ReadFile("testdata/isolation_script_v1.sh")
	if err != nil {
		t.Fatal(err)
	}
	if IsolationScriptV1 != string(expected) {
		t.Fatal("approved isolation script contract changed")
	}
	if !strings.HasPrefix(IsolationScriptV1, "#!/bin/bash\n# CUMULUS-AUTOPROVISIONING\n") || !strings.HasSuffix(IsolationScriptV1, "\nexit 0\n") {
		t.Fatal("Cumulus ZTP markers changed")
	}
	preflight := strings.Index(IsolationScriptV1, "pending_before=\"$(nv config diff applied pending -o commands)\"")
	apply := strings.Index(IsolationScriptV1, "nv config apply --assume-yes")
	verify := strings.Index(IsolationScriptV1, "pending_after=\"$(nv config diff applied pending -o commands)\"")
	if preflight < 0 || verify <= preflight || apply <= verify {
		t.Fatal("unreviewed pending NVUE changes could be applied")
	}
	if strings.Contains(IsolationScriptV1, "nv set interface \"$iface\" link state up") || strings.Contains(IsolationScriptV1, "eth0 link state down") {
		t.Fatal("isolation script enables fabric or closes management")
	}
}

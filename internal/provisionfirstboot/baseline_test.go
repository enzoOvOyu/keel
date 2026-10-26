package provisionfirstboot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFirstBootSoftwareBaselineSignedAndOptional(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	secret, report := firstBootFixture(t, now)
	legacy, err := Sign(secret, report, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(legacy)
	if strings.Contains(string(raw), "software_baseline") || VerifySubmission(secret.Binding, *legacy, now) != nil {
		t.Fatal("optional observation changed legacy report")
	}
	report.SoftwareBaseline = &SoftwareBaseline{OSID: "ubuntu", OSVersion: "24.04", KernelRelease: "6.8.0-fixture", PackageSource: "dpkg-status/driver-packages-v1", PackageStatus: "observed", DriverPackages: []DriverPackage{{Name: "nvidia-driver-580", Version: "580.0-1", Arch: "amd64"}}}
	in, err := Sign(secret, report, now)
	if err != nil || VerifySubmission(secret.Binding, *in, now) != nil {
		t.Fatal("signed observation", err)
	}
	in.Report.SoftwareBaseline.DriverPackages[0].Version = "581.0-1"
	if VerifySubmission(secret.Binding, *in, now) == nil {
		t.Fatal("unsigned version change accepted")
	}
}

func TestFirstBootDriverPackageInventoryScope(t *testing.T) {
	raw := []byte("Package: nvidia-driver-580\nStatus: install ok installed\nVersion: 580.1-1\nArchitecture: amd64\nDescription: test\n continuation\n\nPackage: bash\nStatus: install ok installed\nVersion: 5.2-1\nArchitecture: amd64\n\nPackage: rdma-core\nStatus: deinstall ok config-files\nVersion: 52.0-1\nArchitecture: amd64\n\nPackage: libibverbs1\nStatus: install ok installed\nVersion: 1:52.0-1\nArchitecture: arm64\n")
	packages, err := parseDriverPackages(raw)
	if err != nil || len(packages) != 2 || packages[0].Name != "libibverbs1" || packages[1].Name != "nvidia-driver-580" {
		t.Fatal(packages, err)
	}
	for _, bad := range [][]byte{nil, append(append([]byte{}, raw...), raw...), []byte("Package: nvidia-driver-580\nStatus: install ok installed\nVersion: 580\nVersion: 581\nArchitecture: amd64\n"), []byte("Package: nvidia-driver-580\nStatus: install ok installed\nVersion: $(touch injected)\nArchitecture: amd64\n")} {
		if _, err := parseDriverPackages(bad); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
	packages, err = parseDriverPackages([]byte("Package: bash\nStatus: install ok installed\nVersion: 5.2-1\nArchitecture: amd64\n"))
	if err != nil || packages == nil || len(packages) != 0 {
		t.Fatal("valid empty driver scope is distinct from unknown", err)
	}
}

func TestFirstBootOSReleaseIsData(t *testing.T) {
	for _, raw := range []string{"ID=ubuntu\nVERSION_ID=\"24.04\"\n", "ID='ubuntu'\nVERSION_ID=24.04\n"} {
		id, version := parseOSRelease([]byte(raw))
		if id != "ubuntu" || version != "24.04" {
			t.Fatal(id, version)
		}
	}
	for _, raw := range []string{"ID=ubuntu\nID=other\nVERSION_ID=24.04\n", "ID=ubuntu\nVERSION_ID=$(anything)\n", "ID=ubuntu\n"} {
		id, version := parseOSRelease([]byte(raw))
		if id != "unknown" || version != "unknown" {
			t.Fatal("ambiguous OS identity", id, version)
		}
	}
}

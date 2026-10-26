//go:build linux

package provisionfirstboot

import (
	"os"
	"path/filepath"
	"strings"
)

func collectSoftwareBaseline() *SoftwareBaseline {
	out := &SoftwareBaseline{OSID: "unknown", OSVersion: "unknown", KernelRelease: "unknown", PackageSource: "dpkg-status/driver-packages-v1", PackageStatus: "unknown", DriverPackages: []DriverPackage{}}
	path := "/etc/os-release"
	// Ubuntu commonly links this file to the vendor copy. Resolve only to the
	// one standard vendor path; arbitrary targets never become readable inputs.
	if stat, err := os.Lstat(path); os.IsNotExist(err) {
		path = "/usr/lib/os-release"
	} else if err == nil && stat.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			path = ""
		} else {
			if !filepath.IsAbs(target) {
				target = filepath.Join("/etc", target)
			}
			if filepath.Clean(target) == "/usr/lib/os-release" {
				path = "/usr/lib/os-release"
			} else {
				path = ""
			}
		}
	}
	if path != "" {
		if raw, _, err := readProtected(path, 64<<10, false); err == nil {
			out.OSID, out.OSVersion = parseOSRelease(raw)
		}
	}
	if value, err := readSmall("/proc/sys/kernel/osrelease", 256); err == nil && baselineValue(value, 256) {
		out.KernelRelease = value
	}
	if raw, _, err := readProtected("/var/lib/dpkg/status", 16<<20, false); err == nil {
		if packages, err := parseDriverPackages(raw); err == nil {
			out.PackageStatus = "observed"
			out.DriverPackages = packages
		}
	}
	// Keep the signed v1 payload bounded even with an unusual kernel string.
	out.KernelRelease = strings.TrimSpace(out.KernelRelease)
	return out
}

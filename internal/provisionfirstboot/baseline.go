package provisionfirstboot

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SoftwareBaseline is an observation, not a compatibility or installation
// success verdict. The package scope is deliberately explicit: unloaded drivers
// can be installed, and out-of-band/runfile drivers are not in the dpkg DB.
type SoftwareBaseline struct {
	OSID           string          `json:"os_id"`
	OSVersion      string          `json:"os_version"`
	KernelRelease  string          `json:"kernel_release"`
	PackageSource  string          `json:"package_source"`
	PackageStatus  string          `json:"package_status"`
	DriverPackages []DriverPackage `json:"driver_packages"`
}
type DriverPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,127}$`)
var packageArch = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
var packageVersion = regexp.MustCompile(`^[0-9][A-Za-z0-9.+:~_-]{0,127}$`)

func baselineValue(v string, limit int) bool {
	if v == "" || len(v) > limit || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (b *SoftwareBaseline) Validate() error {
	if b == nil {
		return nil
	} // Older v1 helpers did not send this optional observation.
	if !baselineValue(b.OSID, 128) || !baselineValue(b.OSVersion, 128) || !baselineValue(b.KernelRelease, 256) || b.PackageSource != "dpkg-status/driver-packages-v1" || (b.PackageStatus != "observed" && b.PackageStatus != "unknown") || b.DriverPackages == nil || len(b.DriverPackages) > 64 || (b.PackageStatus == "unknown" && len(b.DriverPackages) != 0) {
		return ErrDenied
	}
	previous := ""
	for _, p := range b.DriverPackages {
		key := p.Name + ":" + p.Arch
		if !packageName.MatchString(p.Name) || !driverPackageName(p.Name) || !packageArch.MatchString(p.Arch) || !packageVersion.MatchString(p.Version) || key <= previous {
			return ErrDenied
		}
		previous = key
	}
	return nil
}

func driverPackageName(name string) bool {
	for _, prefix := range []string{"nvidia-", "libnvidia-", "linux-modules-nvidia-", "linux-objects-nvidia-", "mlx", "mlnx-", "ofed-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return name == "rdma-core" || name == "ibverbs-providers" || name == "libibverbs1"
}

// parseDriverPackages reads Debian control records without executing dpkg or
// shell code. Any malformed/oversized inventory becomes unknown as a whole.
func parseDriverPackages(raw []byte) ([]DriverPackage, error) {
	if len(raw) == 0 || len(raw) > 16<<20 || !utf8.Valid(raw) {
		return nil, ErrDenied
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	fields := map[string]string{}
	out := []DriverPackage{}
	seen := map[string]bool{}
	flush := func() error {
		name := fields["Package"]
		if !driverPackageName(name) || fields["Status"] != "install ok installed" {
			fields = map[string]string{}
			return nil
		}
		p := DriverPackage{Name: name, Version: fields["Version"], Arch: fields["Architecture"]}
		key := p.Name + ":" + p.Arch
		if !packageName.MatchString(p.Name) || !packageArch.MatchString(p.Arch) || !packageVersion.MatchString(p.Version) || seen[key] || len(out) >= 64 {
			return ErrDenied
		}
		seen[key] = true
		out = append(out, p)
		fields = map[string]string{}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if flush() != nil {
				return nil, ErrDenied
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, ErrDenied
		}
		switch key {
		case "Package", "Status", "Version", "Architecture":
			if _, exists := fields[key]; exists {
				return nil, ErrDenied
			}
			fields[key] = strings.TrimSpace(value)
		}
	}
	if scanner.Err() != nil || flush() != nil {
		return nil, ErrDenied
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name+":"+out[i].Arch < out[j].Name+":"+out[j].Arch })
	return out, nil
}

// os-release is data, never sourced as a shell script. Only the two simple
// identifying fields are accepted; unsupported quoting remains unknown.
func parseOSRelease(raw []byte) (string, string) {
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "ID" && key != "VERSION_ID") {
			continue
		}
		if _, exists := values[key]; exists {
			return "unknown", "unknown"
		}
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if !baselineValue(value, 128) || strings.ContainsAny(value, "\\\"'$` ") {
			return "unknown", "unknown"
		}
		values[key] = value
	}
	if values["ID"] == "" || values["VERSION_ID"] == "" {
		return "unknown", "unknown"
	}
	return values["ID"], values["VERSION_ID"]
}

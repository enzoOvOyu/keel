//go:build linux

package precollect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Collector has no caller-supplied command, executable, interface or sysfs path.
// Every external invocation below is a read-only argument vector without shell.
func Collect(parent context.Context) (*Bundle, error) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	b := &Bundle{Version: Version, Origin: "real", StartedAt: time.Now().UTC(), Arch: runtime.GOARCH, Interfaces: []InterfaceSeed{}, RDMADevices: []string{}, Probes: []Probe{}, Missing: []string{}}
	missing := func(code string) {
		if len(b.Missing) < 128 {
			b.Missing = append(b.Missing, code)
		}
	}
	b.ConfigBefore = networkConfiguration()
	if len(b.ConfigBefore) == 0 {
		missing("network_configuration_unavailable")
	}
	b.Serial, _ = readHostFile("/sys/class/dmi/id/product_serial", 256)
	if b.Serial == "" {
		b.Serial, _ = readHostFile("/sys/firmware/devicetree/base/serial-number", 256)
	}
	b.Serial = strings.TrimSpace(strings.TrimRight(b.Serial, "\x00"))
	if b.Serial == "" {
		missing("system_serial_unavailable")
	}
	b.OSRelease, _ = readHostFile("/etc/os-release", 16384)
	if b.OSRelease == "" {
		missing("os_release_unavailable")
	}
	entries, e := os.ReadDir("/sys/class/net")
	if e != nil {
		missing("network_interfaces_unavailable")
	}
	if len(entries) > 2048 {
		return nil, ErrInvalid
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			missing("precollect_deadline")
			break
		}
		name := entry.Name()
		if !ValidInterface(name) {
			missing("network_interface_name_unsupported")
			continue
		}
		device, e := filepath.EvalSymlinks("/sys/class/net/" + name + "/device")
		if e != nil {
			continue
		}
		bdf := CanonicalBDF(filepath.Base(device))
		if bdf == "" {
			missing("network_bdf_unavailable:" + name)
			continue
		}
		if len(b.Interfaces) >= 128 {
			missing("network_interface_limit")
			break
		}
		seed := InterfaceSeed{Name: name, BDF: bdf}
		if pf, e := filepath.EvalSymlinks(device + "/physfn"); e == nil {
			seed.VirtualFunction = true
			seed.PFBDF = CanonicalBDF(filepath.Base(pf))
			if seed.PFBDF == "" {
				missing("network_pf_bdf_unavailable:" + name)
				continue
			}
		} else if !os.IsNotExist(e) {
			missing("network_function_identity_unavailable:" + name)
			continue
		}
		b.Interfaces = append(b.Interfaces, seed)
	}
	rdma, e := os.ReadDir("/sys/class/infiniband")
	if e != nil {
		missing("rdma_devices_unavailable")
	}
	if len(rdma) > 128 {
		missing("rdma_device_limit")
	} else {
		for _, entry := range rdma {
			if !iface.MatchString(entry.Name()) {
				missing("rdma_device_name_unsupported")
				continue
			}
			b.RDMADevices = append(b.RDMADevices, entry.Name())
		}
	}
	totalProbeBytes := 0
	appendProbe := func(kind, target, tool string, args ...string) {
		if ctx.Err() != nil {
			missing("precollect_deadline")
			return
		}
		p := runHostProbe(ctx, kind, target, tool, args...)
		totalProbeBytes += len(p.Stdout) + len(p.Stderr)
		if totalProbeBytes > MaxBundleBytes/2 {
			missing("precollect_output_limit")
			return
		}
		b.Probes = append(b.Probes, p)
	}
	appendProbe("gpu", "", "nvidia-smi", "--query-gpu=uuid,pci.bus_id,memory.total,driver_version,ecc.errors.uncorrected.volatile.total", "--format=csv,noheader,nounits")
	appendProbe("nvlink", "", "nvidia-smi", "nvlink", "--status")
	appendProbe("gpu_topology", "", "nvidia-smi", "topo", "-m")
	appendProbe("pci_topology", "", "lspci", "-D", "-tv")
	appendProbe("rdma_links", "", "rdma", "-j", "link", "show")
	appendProbe("ibv_devices", "", "ibv_devices")
	for _, seed := range b.Interfaces {
		if seed.VirtualFunction {
			continue
		}
		appendProbe("nic_permanent", seed.Name, "ethtool", "-P", seed.Name)
		appendProbe("nic_link", seed.Name, "ethtool", seed.Name)
		appendProbe("nic_driver", seed.Name, "ethtool", "-i", seed.Name)
		appendProbe("nic_dom", seed.Name, "ethtool", "-m", seed.Name)
	}
	if ctx.Err() != nil {
		missing("precollect_deadline")
	}
	b.ConfigAfter = networkConfiguration()
	b.CollectedAt = time.Now().UTC()
	raw, e := json.Marshal(b)
	if e != nil || len(raw) > MaxBundleBytes {
		return nil, ErrInvalid
	}
	return b, nil
}
func readHostFile(path string, limit int64) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return "", ErrInvalid
	}
	return strings.TrimSpace(string(raw)), nil
}
func networkConfiguration() []NetConfig {
	values, e := net.Interfaces()
	if e != nil || len(values) > 2048 {
		return nil
	}
	out := []NetConfig{}
	for _, v := range values {
		if !ValidInterface(v.Name) {
			return nil
		}
		out = append(out, NetConfig{Name: v.Name, MAC: v.HardwareAddr.String(), MTU: v.MTU, AdminUp: v.Flags&net.FlagUp != 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type probeBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *probeBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		b.overflow = true
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}
func hostTool(name string) (string, error) {
	allowed := map[string][]string{"nvidia-smi": {"/usr/bin/nvidia-smi"}, "lspci": {"/usr/bin/lspci", "/usr/sbin/lspci"}, "rdma": {"/usr/bin/rdma", "/usr/sbin/rdma", "/sbin/rdma"}, "ibv_devices": {"/usr/bin/ibv_devices"}, "ethtool": {"/usr/sbin/ethtool", "/sbin/ethtool", "/usr/bin/ethtool"}}
	for _, path := range allowed[name] {
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil {
			continue
		}
		info, e := os.Stat(resolved)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			continue
		}
		return resolved, nil
	}
	return "", os.ErrNotExist
}
func runHostProbe(parent context.Context, kind, target, tool string, args ...string) Probe {
	p := Probe{Kind: kind, Target: target, State: "tool_unavailable"}
	path, e := hostTool(tool)
	if e != nil {
		return p
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	cmd.WaitDelay = time.Second
	out, stderr := &probeBuffer{limit: 262144}, &probeBuffer{limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = stderr
	e = cmd.Run()
	p.Stdout = out.String()
	p.Stderr = stderr.String()
	p.State = "ready"
	if out.overflow || stderr.overflow {
		p.State = "output_limit"
	} else if ctx.Err() != nil {
		p.State = "timeout"
	} else if e != nil {
		p.State = "read_failed"
	}
	return p
}

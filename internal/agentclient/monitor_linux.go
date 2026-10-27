//go:build linux

package agentclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

type monitorOutput struct{ bytes.Buffer }

func (b *monitorOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, monitor.ErrInvalid
	}
	return b.Buffer.Write(p)
}
func monitorTool(parent context.Context, tool string, args ...string) (string, bool) {
	allowed := map[string][]string{"ethtool": {"/usr/sbin/ethtool", "/sbin/ethtool", "/usr/bin/ethtool"}, "nvidia-smi": {"/usr/bin/nvidia-smi"}}
	var path string
	for _, candidate := range allowed[tool] {
		if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() {
			path = candidate
			break
		}
	}
	if path == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/usr/sbin:/bin:/sbin", "LC_ALL=C"}
	var out monitorOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	if cmd.Run() != nil {
		return "", false
	}
	return out.String(), true
}
func monitorSample(key, entity string, value *float64, state, quality string, at time.Time) monitor.Sample {
	return monitor.Sample{Key: key, Entity: entity, Value: value, State: state, Quality: quality, ObservedAt: at}
}
func monitorNumber(key, entity string, value float64, at time.Time) monitor.Sample {
	return monitorSample(key, entity, &value, "", "good", at)
}
func collectMonitorGroup(ctx context.Context, dtype string, now time.Time, cpu *monitorCPUCounter) []monitor.Sample {
	samples := []monitor.Sample{}
	unknown := func(key, entity string) {
		samples = append(samples, monitorSample(key, entity, nil, "", "unknown", now))
	}
	switch dtype {
	case "D1", "D2":
		ports, e := net.Interfaces()
		if e != nil {
			return []monitor.Sample{monitorSample("link.state", "host", nil, "", "unknown", now)}
		}
		for _, port := range ports {
			if port.Flags&net.FlagLoopback != 0 || !precollect.ValidInterface(port.Name) {
				continue
			}
			device, e := filepath.EvalSymlinks(filepath.Join("/sys/class/net", port.Name, "device"))
			if e != nil {
				continue
			}
			bdf := precollect.CanonicalBDF(filepath.Base(device))
			if bdf == "" {
				continue
			}
			permanent, _ := monitorTool(ctx, "ethtool", "-P", port.Name)
			mac := precollect.CanonicalMAC(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(permanent), "Permanent address:")))
			appendPort := func(s monitor.Sample) {
				s.Interface = port.Name
				s.PermanentMAC = mac
				s.BDF = bdf
				samples = append(samples, s)
			}
			if dtype == "D1" {
				raw, e := os.ReadFile(filepath.Join("/sys/class/net", port.Name, "operstate"))
				state := strings.TrimSpace(string(raw))
				quality := "good"
				if e != nil || state != "up" && state != "down" {
					state = ""
					quality = "unknown"
				}
				appendPort(monitorSample("link.state", port.Name, nil, state, quality, now))
				raw, e = os.ReadFile(filepath.Join("/sys/class/net", port.Name, "speed"))
				speed, parse := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
				if e == nil && parse == nil && speed > 0 {
					appendPort(monitorNumber("link.speed", port.Name, speed, now))
				} else {
					appendPort(monitorSample("link.speed", port.Name, nil, "", "unknown", now))
				}
			} else {
				raw, ok := monitorTool(ctx, "ethtool", "-m", port.Name)
				state := "unknown"
				if ok {
					state = "ready"
				}
				report, e := precollect.Normalize(&precollect.Bundle{Interfaces: []precollect.InterfaceSeed{{Name: port.Name, BDF: bdf}}, Probes: []precollect.Probe{{Kind: "nic_dom", Target: port.Name, State: state, Stdout: raw}}})
				if e != nil || len(report.NICs) != 1 {
					for _, key := range []string{"dom.temperature", "dom.rx_power", "dom.tx_power"} {
						appendPort(monitorSample(key, port.Name, nil, "", "unknown", now))
					}
					continue
				}
				nic := report.NICs[0]
				for key, value := range map[string]*float64{"dom.temperature": nic.TemperatureC, "dom.rx_power": nic.RxPowerDBm, "dom.tx_power": nic.TxPowerDBm} {
					quality := "good"
					if value == nil {
						quality = "unknown"
					}
					appendPort(monitorSample(key, port.Name, value, "", quality, now))
				}
			}
		}
	case "D3":
		cpuRaw, cpuErr := os.ReadFile("/proc/stat")
		if cpuErr == nil {
			if utilization, ok := cpu.observe(cpuRaw); ok {
				samples = append(samples, monitorNumber("host.cpu_utilization", "host", utilization, now))
			} else {
				unknown("host.cpu_utilization", "host")
			}
		} else {
			unknown("host.cpu_utilization", "host")
		}
		raw, e := os.ReadFile("/proc/loadavg")
		fields := strings.Fields(string(raw))
		if e == nil && len(fields) > 0 {
			value, e := strconv.ParseFloat(fields[0], 64)
			if e == nil {
				samples = append(samples, monitorNumber("host.load1", "host", value, now))
			} else {
				unknown("host.load1", "host")
			}
		} else {
			unknown("host.load1", "host")
		}
		raw, e = os.ReadFile("/proc/meminfo")
		values := map[string]float64{}
		if e == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				f := strings.Fields(line)
				if len(f) >= 2 && (f[0] == "MemTotal:" || f[0] == "MemAvailable:") {
					v, e := strconv.ParseFloat(f[1], 64)
					if e == nil {
						values[f[0]] = v * 1024
					}
				}
			}
		}
		total, totalOK := values["MemTotal:"]
		available, availableOK := values["MemAvailable:"]
		if totalOK && availableOK && total >= available {
			samples = append(samples, monitorNumber("host.memory_used", "host", total-available, now))
		} else {
			unknown("host.memory_used", "host")
		}
		var fs syscall.Statfs_t
		if syscall.Statfs("/", &fs) == nil {
			samples = append(samples, monitorNumber("host.disk_used", "root", float64(fs.Blocks-fs.Bfree)*float64(fs.Bsize), now))
		} else {
			unknown("host.disk_used", "root")
		}
	case "D4":
		raw, ok := monitorTool(ctx, "nvidia-smi", "--query-gpu=uuid,utilization.gpu,temperature.gpu,power.draw,ecc.errors.uncorrected.volatile.total", "--format=csv,noheader,nounits")
		reader := csv.NewReader(strings.NewReader(raw))
		reader.TrimLeadingSpace = true
		reader.FieldsPerRecord = 5
		rows, e := reader.ReadAll()
		if !ok || e != nil || len(rows) > 256 || len(rows) == 0 {
			for _, key := range []string{"gpu.utilization", "gpu.temperature", "gpu.power", "gpu.ecc"} {
				unknown(key, "gpu")
			}
			break
		}
		for _, row := range rows {
			entity := strings.TrimSpace(row[0])
			for i, key := range []string{"gpu.utilization", "gpu.temperature", "gpu.power", "gpu.ecc"} {
				value, e := strconv.ParseFloat(strings.TrimSpace(row[i+1]), 64)
				if e == nil {
					samples = append(samples, monitorNumber(key, entity, value, now))
				} else {
					unknown(key, entity)
				}
			}
		}
	case "D5":
		devices, e := os.ReadDir("/sys/class/infiniband")
		if e != nil {
			unknown("rdma.errors", "rdma")
			break
		}
		for _, device := range devices {
			if !precollect.ValidInterface(device.Name()) {
				continue
			}
			ports, e := os.ReadDir(filepath.Join("/sys/class/infiniband", device.Name(), "ports"))
			if e != nil {
				continue
			}
			for _, port := range ports {
				index, e := strconv.Atoi(port.Name())
				if e != nil || index < 1 || index > 255 {
					continue
				}
				entity := device.Name() + "/" + port.Name()
				total := float64(0)
				known := true
				for _, counter := range []string{"port_rcv_errors", "port_xmit_discards"} {
					raw, e := os.ReadFile(filepath.Join("/sys/class/infiniband", device.Name(), "ports", port.Name(), "counters", counter))
					value, parse := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
					if e != nil || parse != nil {
						known = false
						break
					}
					total += value
				}
				if known {
					samples = append(samples, monitorNumber("rdma.errors", entity, total, now))
				} else {
					unknown("rdma.errors", entity)
				}
				unknown("rdma.pfc_pause", entity)
			}
		}
	case "D6":
		samples = append(samples, monitorSample("service.state", "keel-agent", nil, "running", "good", now))
	}
	return samples
}

// Sampling lives outside the reconnecting transport. Critical changes are
// fsynced before sending; overflow stops sampling and produces an explicit gap.
func RunMonitorCollector(ctx context.Context, c Config) error {
	if !c.MonitorEnabled {
		return nil
	}
	if _, e := monitorQueueDirectory(c); e != nil {
		return e
	}
	cpu := &monitorCPUCounter{}
	previous := map[string]monitor.Sample{}
	last := map[string]time.Time{}
	var baseline, policyID string
	var jittered bool
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			assignment, e := monitorLoadAssignment(c)
			if e != nil {
				return e
			}
			if assignment == nil || !now.Before(assignment.ValidUntil) || assignment.Policy.Stage == "maintained" {
				continue
			}
			if !jittered {
				jittered = true
				timer := time.NewTimer(time.Duration(assignment.InitialJitterMillis) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
				now = time.Now()
			}
			if policyID != assignment.PolicyID {
				policyID = assignment.PolicyID
				last = map[string]time.Time{}
				previous = map[string]monitor.Sample{}
				baseline = ""
			}
		collect:
			for _, dtype := range []string{"D1", "D2", "D3", "D4", "D5", "D6"} {
				if !time.Now().Before(assignment.ValidUntil) {
					break
				}
				interval := assignment.Policy.Intervals[dtype]
				if interval == 0 || now.Sub(last[dtype]) < time.Duration(interval)*time.Second {
					continue
				}
				deadline := time.Now().Add(15 * time.Second)
				if assignment.ValidUntil.Before(deadline) {
					deadline = assignment.ValidUntil
				}
				work, cancel := context.WithDeadline(ctx, deadline)
				samples := collectMonitorGroup(work, dtype, time.Now().UTC(), cpu)
				cancel()
				for offset := 0; offset < len(samples); offset += 12 {
					end := offset + 12
					if end > len(samples) {
						end = len(samples)
					}
					id := make([]byte, 16)
					if _, e = rand.Read(id); e != nil {
						return e
					}
					frame := monitor.Frame{Version: monitor.Version, ID: hex.EncodeToString(id), PolicyID: policyID, Full: baseline == "", BaselineID: baseline, Samples: []monitor.Sample{}, Unchanged: []monitor.Unchanged{}}
					for _, sample := range samples[offset:end] {
						old, ok := previous[sample.Identity()]
						old.ObservedAt = sample.ObservedAt
						if ok && string(mustMonitorJSON(old)) == string(mustMonitorJSON(sample)) && !frame.Full {
							frame.Unchanged = append(frame.Unchanged, monitor.Unchanged{Key: sample.Key, Entity: sample.Entity, ObservedAt: sample.ObservedAt})
						} else {
							frame.Samples = append(frame.Samples, sample)
						}
					}
					if e = monitorQueueAppend(c, frame); e != nil {
						if errors.Is(e, errMonitorQueueFull) {
							break collect
						}
						return e
					}
					baseline = frame.ID
					for _, sample := range samples[offset:end] {
						previous[sample.Identity()] = sample
					}
				}
				last[dtype] = now
			}
		}
	}
}
func mustMonitorJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

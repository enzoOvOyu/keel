//go:build linux

package provisionpreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

func (linuxSource) health(parent context.Context, name string) Disk {
	out := Disk{Device: "/dev/" + name, Reason: "smart_unavailable", SmartExit: -1}
	if !diskName.MatchString(name) {
		return out
	}
	binary := ""
	for _, candidate := range []string{"/usr/sbin/smartctl", "/usr/bin/smartctl"} {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			binary = candidate
			break
		}
	}
	if binary == "" {
		return out
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--json=c", "--health", out.Device)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	var output limitedOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	status := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return out
		}
		status = exit.ExitCode()
	}
	if ctx.Err() != nil || output.overflow {
		return out
	}
	return ParseSMART(out.Device, output.Bytes(), status)
}

type limitedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(raw []byte) (int, error) {
	n := len(raw)
	if b.Len()+n > 1<<20 {
		b.overflow = true
		remaining := (1 << 20) - b.Len()
		if remaining > 0 {
			b.Buffer.Write(raw[:remaining])
		}
		return n, nil
	}
	b.Buffer.Write(raw)
	return n, nil
}

// A failing SMART check often has a nonzero process exit status and valid JSON.
// Bits 0-2 mean unavailable/read/parse failure; bits 3-4 are current failures.
// Never turn an unsupported device or missing smart_status into healthy=true.
func ParseSMART(device string, raw []byte, exit int) Disk {
	out := Disk{Device: device, Reason: "smart_unavailable", SmartExit: exit}
	if len(raw) > 1<<20 || exit < 0 || exit > 255 || exit&7 != 0 {
		return out
	}
	var report struct {
		Smartctl struct {
			ExitStatus *int `json:"exit_status"`
		} `json:"smartctl"`
		Status *struct {
			Passed *bool `json:"passed"`
		} `json:"smart_status"`
		Serial string `json:"serial_number"`
		NVMe   *struct {
			Critical *uint64 `json:"critical_warning"`
		} `json:"nvme_smart_health_information_log"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&report) != nil {
		return out
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return out
	}
	if report.Smartctl.ExitStatus == nil || *report.Smartctl.ExitStatus != exit || report.Status == nil || report.Status.Passed == nil {
		return out
	}
	out.Serial = report.Serial
	if len(out.Serial) > 256 {
		out.Serial = ""
	}
	healthy := *report.Status.Passed && exit&24 == 0
	if report.NVMe != nil {
		if report.NVMe.Critical == nil {
			return out
		}
		healthy = healthy && *report.NVMe.Critical == 0
	}
	out.Healthy = &healthy
	out.Reason = "smart_health_passed"
	if !healthy {
		out.Reason = "smart_health_failed"
	}
	return out
}

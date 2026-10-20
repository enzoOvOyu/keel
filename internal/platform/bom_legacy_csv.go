package platform

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// LegacyBOMRows folds a UTF-8 wide sheet into one server and its MAC-bearing
// rows per SN. It retains the original bytes separately for batch hashing.
type LegacyBOMRows struct {
	Input       BomImportInput
	Rows        int
	Issues      []BOMPackageIssue
	PortLines   map[string]int
	ServerLines map[string]int
}

var legacyBOMHeaders = map[string]string{
	"serial_number": "sn", "sn": "sn", "序列号": "sn",
	"rack": "rack", "机柜": "rack",
	"unit_position": "u", "u位": "u", "u 位": "u",
	"bmc_address": "bmc_address", "bmc": "bmc_address", "bmc 地址": "bmc_address",
	"hostname": "hostname", "主机名": "hostname",
	"ip_reference": "ip_reference", "ip": "ip_reference", "ip 参考": "ip_reference",
	"model_name": "model_name", "model": "model_name", "型号": "model_name",
	"mac_address": "mac", "mac": "mac", "mac 地址": "mac",
	"port_name": "port_name", "port": "port_name", "端口": "port_name",
	"rail": "rail",
}

func ParseLegacyBOMCSV(data []byte) (*LegacyBOMRows, error) {
	if len(data) == 0 || len(data) > 64<<20 {
		return nil, errors.New("legacy BOM file size outside allowed range")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("legacy BOM must be UTF-8 CSV")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("legacy BOM header: %w", err)
	}
	columns := map[string]int{}
	out := &LegacyBOMRows{Input: BomImportInput{Machines: []BomMachineRow{}}, Issues: []BOMPackageIssue{}, PortLines: map[string]int{}, ServerLines: map[string]int{}}
	for i, raw := range header {
		name := strings.ToLower(strings.TrimSpace(raw))
		for _, secret := range []string{"password", "passwd", "secret", "private_key", "token"} {
			if strings.Contains(name, secret) {
				return nil, fmt.Errorf("legacy BOM must not include plaintext credential column %q", raw)
			}
		}
		canonical, known := legacyBOMHeaders[name]
		if !known {
			out.Issues = append(out.Issues, BOMPackageIssue{Severity: "WARN", File: "legacy/wide.csv", Field: raw, Code: "unmapped_column"})
			continue
		}
		if _, duplicate := columns[canonical]; duplicate {
			return nil, fmt.Errorf("duplicate legacy BOM column %q", canonical)
		}
		columns[canonical] = i
	}
	for _, key := range []string{"sn", "hostname", "model_name", "mac"} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("legacy BOM missing %s column", key)
		}
	}
	machines := map[string]int{}
	seenMAC := map[string]int{}
	for {
		line, _ := r.FieldPos(1)
		cells, readErr := r.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("legacy BOM row after line %d: %w", line, readErr)
		}
		if len(cells) != len(header) {
			return nil, fmt.Errorf("legacy BOM line %d has %d columns; expected %d", line, len(cells), len(header))
		}
		blank := true
		for _, c := range cells {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		out.Rows++
		if out.Rows > 250000 {
			return nil, errors.New("legacy BOM exceeds 250000 data rows")
		}
		line, _ = r.FieldPos(1)
		pick := func(key string) string {
			if i, ok := columns[key]; ok {
				return strings.TrimSpace(cells[i])
			}
			return ""
		}
		sn, mac := pick("sn"), pick("mac")
		if !snPattern.MatchString(sn) {
			return nil, fmt.Errorf("legacy BOM line %d has invalid SN", line)
		}
		if _, valid := normalizeMAC(mac); !valid {
			return nil, fmt.Errorf("legacy BOM line %d has invalid MAC", line)
		}
		mac, _ = normalizeMAC(mac)
		if first, duplicate := seenMAC[mac]; duplicate {
			return nil, fmt.Errorf("legacy BOM MAC %s duplicates line %d at line %d", mac, first, line)
		}
		seenMAC[mac] = line
		if !hostPattern.MatchString(pick("hostname")) {
			return nil, fmt.Errorf("legacy BOM line %d has invalid hostname", line)
		}
		if pick("model_name") == "" {
			return nil, fmt.Errorf("legacy BOM line %d has no model", line)
		}
		rail := 0
		if value := pick("rail"); value != "" {
			rail, err = strconv.Atoi(value)
			if err != nil || rail < 0 || rail > 32767 {
				return nil, fmt.Errorf("legacy BOM line %d has invalid rail", line)
			}
		}
		portName := pick("port_name")
		if portName == "" {
			portName = fmt.Sprintf("port%d", len(out.Input.Machines)+1)
		}
		if len(portName) > 64 {
			return nil, fmt.Errorf("legacy BOM line %d port name too long", line)
		}
		if index, found := machines[sn]; found {
			m := &out.Input.Machines[index]
			if m.Rack != pick("rack") || m.UnitPosition != pick("u") || m.BmcAddress != pick("bmc_address") || m.Hostname != pick("hostname") || m.IpReference != pick("ip_reference") || m.ModelName != pick("model_name") {
				return nil, fmt.Errorf("legacy BOM line %d conflicts with prior facts for SN %s", line, sn)
			}
			if pick("port_name") == "" {
				portName = fmt.Sprintf("port%d", len(m.Ports)+1)
			}
			for _, p := range m.Ports {
				if p.PortName == portName {
					return nil, fmt.Errorf("legacy BOM line %d duplicates port name %s", line, portName)
				}
			}
			m.Ports = append(m.Ports, BomPortRow{MacAddress: mac, PortName: portName, Rail: rail})
		} else {
			machines[sn] = len(out.Input.Machines)
			out.ServerLines[sn] = line
			out.Input.Machines = append(out.Input.Machines, BomMachineRow{SerialNumber: sn, Rack: pick("rack"), UnitPosition: pick("u"), BmcAddress: pick("bmc_address"), Hostname: pick("hostname"), IpReference: pick("ip_reference"), ModelName: pick("model_name"), Ports: []BomPortRow{{MacAddress: mac, PortName: portName, Rail: rail}}})
		}
		out.PortLines[mac] = line
	}
	if out.Rows == 0 {
		return nil, errors.New("legacy BOM has no data rows")
	}
	return out, nil
}

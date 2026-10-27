package bmc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const IPMIParserVersion = "ipmi-inventory/v1"

var ipmiOperations = []string{"device-id", "system-guid", "chassis-status", "fru-0"}

func ipmiPath(operation string) string { return "/ipmi/v2/inventory/" + operation }

// InventoryPath validates storage identities only. IPMI identities can never be
// resolved by the HTTPS reader or used to submit arbitrary raw commands.
func InventoryPath(path string) (string, error) {
	for _, operation := range ipmiOperations {
		if path == ipmiPath(operation) {
			return path, nil
		}
	}
	return CanonicalPath("/redfish/v1/", path)
}

type IPMIReader interface {
	ReadIPMI(context.Context, Target, string) ([]byte, error)
	Origin() string
}
type FallbackInfo struct {
	Protocol      string `json:"protocol"`
	Reason        string `json:"reason"`
	ParserVersion string `json:"parser_version"`
	ResourceCount int    `json:"resource_count"`
}
type IPMIEnvelope struct {
	Schema     string         `json:"schema"`
	Type       string         `json:"@odata.type"`
	Operation  string         `json:"operation"`
	PayloadHex string         `json:"payload_hex"`
	Decoded    map[string]any `json:"decoded"`
}

func readHexPayload(raw []byte) ([]byte, error) {
	fields := strings.Fields(string(raw))
	if len(fields) < 1 || len(fields) > 65535 {
		return nil, errors.New("invalid IPMI raw reply size")
	}
	out := make([]byte, len(fields))
	for i, v := range fields {
		if len(v) != 2 {
			return nil, errors.New("invalid IPMI raw hex")
		}
		b, e := hex.DecodeString(v)
		if e != nil {
			return nil, errors.New("invalid IPMI raw hex")
		}
		out[i] = b[0]
	}
	return out, nil
}
func decodeIPMI(operation string, payload []byte) (map[string]any, error) {
	switch operation {
	case "device-id":
		if len(payload) < 11 || len(payload) > 15 {
			return nil, errors.New("invalid IPMI device id")
		}
		return map[string]any{"DeviceID": int(payload[0]), "FirmwareMajor": int(payload[2] & 0x7f), "FirmwareMinorBCD": fmt.Sprintf("%02x", payload[3]), "IPMIVersionBCD": fmt.Sprintf("%02x", payload[4]), "ManufacturerID": int(payload[6]) | int(payload[7])<<8 | int(payload[8])<<16, "ProductID": int(payload[9]) | int(payload[10])<<8}, nil
	case "system-guid":
		if len(payload) != 16 {
			return nil, errors.New("invalid IPMI GUID")
		}
		return map[string]any{"SystemGUIDRawHex": hex.EncodeToString(payload)}, nil
	case "chassis-status":
		if len(payload) < 3 || len(payload) > 4 {
			return nil, errors.New("invalid IPMI chassis status")
		}
		return map[string]any{"PowerOn": payload[0]&1 != 0, "PowerOverload": payload[0]&2 != 0, "PowerFault": payload[0]&8 != 0, "ChassisIntrusion": payload[2]&1 != 0}, nil
	case "fru-0":
		return decodeFRU(payload)
	default:
		return nil, errors.New("IPMI operation is not in the inventory allowlist")
	}
}

// FRU type/length fields are parsed from checksummed standard areas. Only a
// Product Serial Number can identify the server; a board SN is not substituted.
func decodeFRU(raw []byte) (map[string]any, error) {
	checksum := func(area []byte) bool {
		var sum byte
		for _, v := range area {
			sum += v
		}
		return sum == 0
	}
	if len(raw) < 8 || len(raw) > 65535 || raw[0] != 1 || raw[6] != 0 || !checksum(raw[:8]) {
		return nil, errors.New("invalid FRU common header")
	}
	out := map[string]any{"FRUDeviceID": 0}
	lastEnd := 8
	for _, area := range []struct {
		index, offset int
		names         []string
	}{{2, 3, []string{"ChassisPartNumber", "ChassisSerialNumber"}}, {3, 6, []string{"BoardManufacturer", "BoardProductName", "BoardSerialNumber", "BoardPartNumber", "BoardFRUFileID"}}, {4, 3, []string{"ProductManufacturer", "ProductName", "ProductPartNumber", "ProductVersion", "ProductSerialNumber", "ProductAssetTag", "ProductFRUFileID"}}} {
		start := int(raw[area.index]) * 8
		if start == 0 {
			continue
		}
		if start < lastEnd || start+2 > len(raw) {
			return nil, errors.New("overlapping or truncated FRU area")
		}
		end := start + int(raw[start+1])*8
		if raw[start] != 1 || end > len(raw) || end <= start+area.offset || !checksum(raw[start:end]) {
			return nil, errors.New("invalid FRU area version, length or checksum")
		}
		lastEnd = end
		position := start + area.offset
		terminated := false
		for field := 0; position < end-1; field++ {
			tag := raw[position]
			position++
			if tag == 0xc1 {
				terminated = true
				break
			}
			size := int(tag & 0x3f)
			if position+size > end-1 {
				return nil, errors.New("truncated FRU field")
			}
			value := raw[position : position+size]
			position += size
			if field >= len(area.names) || tag>>6 != 3 {
				continue
			}
			ascii := true
			for _, c := range value {
				if c < 32 || c > 126 {
					ascii = false
					break
				}
			}
			if ascii {
				out[area.names[field]] = strings.TrimSpace(string(value))
			}
		}
		if !terminated {
			return nil, errors.New("unterminated FRU fields")
		}
	}
	return out, nil
}
func validateIPMIResource(r Resource, target Target) (bool, error) {
	var envelope IPMIEnvelope
	if json.Unmarshal(r.Data, &envelope) != nil || envelope.Schema != IPMIParserVersion || r.Type != "#IPMI.Inventory.v1" || envelope.Type != r.Type || r.Path != ipmiPath(envelope.Operation) {
		return false, errors.New("IPMI resource binding differs from retained envelope")
	}
	payload, e := hex.DecodeString(envelope.PayloadHex)
	if e != nil {
		return false, e
	}
	decoded, e := decodeIPMI(envelope.Operation, payload)
	if e != nil {
		return false, e
	}
	a, _ := json.Marshal(decoded)
	b, _ := json.Marshal(envelope.Decoded)
	if CanonicalDigest(a) != CanonicalDigest(b) {
		return false, errors.New("IPMI decoded data differs from raw payload")
	}
	serial, _ := decoded["ProductSerialNumber"].(string)
	return envelope.Operation == "fru-0" && serial != "" && serial == target.Serial, nil
}
func fallbackReason(out *Snapshot) string {
	for _, f := range out.Failures {
		if f.Class == "authentication_failed" && strings.HasPrefix(f.Path, "/redfish/") {
			return ""
		}
	}
	for _, f := range out.Failures {
		if f.Path != "/redfish/v1/" {
			continue
		}
		if f.Class == "timeout" {
			return "redfish_timeout"
		}
		if f.Class == "version_unsupported" {
			return "redfish_unsupported"
		}
		if f.Code == "transport_unavailable" || (f.Code == "http_response_failed" && f.HTTPStatus >= 500) {
			return "redfish_unavailable"
		}
	}
	return ""
}
func (c *Collector) collectIPMISupplement(ctx context.Context, out *Snapshot, total *int64) {
	reason := fallbackReason(out)
	if reason == "" || c.Fallback == nil || ctx.Err() != nil {
		return
	}
	if c.Fallback.Origin() != out.Origin {
		if len(out.Failures) < 256 {
			out.Failures = append(out.Failures, *failure(ipmiPath("device-id"), "partial_failure", "ipmi_origin_mismatch", false))
		}
		return
	}
	out.Fallback = &FallbackInfo{Protocol: "ipmi", Reason: reason, ParserVersion: IPMIParserVersion}
	for _, operation := range ipmiOperations {
		path := ipmiPath(operation)
		if len(out.Resources) >= c.Config.MaxResources || *total >= c.Config.MaxBytes {
			break
		}
		payload, e := c.Fallback.ReadIPMI(ctx, out.Target, operation)
		if e != nil {
			f := failure(path, "partial_failure", "ipmi_read_failed", true)
			var classified *Failure
			if errors.As(e, &classified) {
				copy := *classified
				f = &copy
				f.Path = path
			}
			if len(out.Failures) < 256 {
				out.Failures = append(out.Failures, *f)
			}
			if f.Class == "authentication_failed" || ctx.Err() != nil {
				break
			}
			continue
		}
		decoded, e := decodeIPMI(operation, payload)
		if e != nil {
			if len(out.Failures) < 256 {
				out.Failures = append(out.Failures, *failure(path, "partial_failure", "ipmi_payload_invalid", false))
			}
			continue
		}
		body, e := json.Marshal(IPMIEnvelope{Schema: IPMIParserVersion, Type: "#IPMI.Inventory.v1", Operation: operation, PayloadHex: hex.EncodeToString(payload), Decoded: decoded})
		if e != nil {
			continue
		}
		if int64(len(body))+*total > c.Config.MaxBytes {
			break
		}
		raw, e := c.Raw.StoreRaw(ctx, out.Target, path, body)
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		if e != nil || raw.Key != RawKey(out.Target, path, digest) || raw.Version == "" || raw.Version == "null" || raw.SHA256 != digest || raw.Size != int64(len(body)) {
			if len(out.Failures) < 256 {
				out.Failures = append(out.Failures, *failure(path, "partial_failure", "raw_storage_unavailable", true))
			}
			break
		}
		resource := Resource{Path: path, Type: "#IPMI.Inventory.v1", Source: "bmc", ReceivedAt: time.Now().UTC(), Raw: raw, DataSHA256: CanonicalDigest(body), Data: body}
		matched, e := validateIPMIResource(resource, out.Target)
		if e != nil {
			return
		}
		out.Resources = append(out.Resources, resource)
		*total += raw.Size
		out.Fallback.ResourceCount++
		if matched {
			out.IdentityMatched = true
		}
		out.Capabilities["IPMI:"+operation] = "collected"
	}
}

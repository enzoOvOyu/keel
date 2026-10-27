package bmc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Collector struct {
	Fallback IPMIReader
	Reader   Reader
	Raw      RawSink
	Config   Config
}
type pendingResource struct {
	path, feature string
	depth         int
}

var levelFeatures = map[string][]string{
	"L1": {"Systems", "Managers", "Chassis"},
	"L2": {"Systems", "Managers", "Chassis", "NetworkInterfaces", "EthernetInterfaces"},
	"L3": {"Systems", "Managers", "Chassis", "NetworkInterfaces", "EthernetInterfaces", "Processors", "Memory", "Storage", "Power", "Thermal", "Bios", "BootOptions", "FirmwareInventory"},
}
var featureAliases = map[string]string{"Drives": "Storage", "Volumes": "Storage", "Controllers": "Storage", "StorageControllers": "Storage", "Ports": "NetworkInterfaces", "NetworkDeviceFunctions": "NetworkInterfaces", "NetworkAdapters": "NetworkInterfaces", "NetworkAdapter": "NetworkInterfaces", "NetworkDeviceFunction": "NetworkInterfaces", "NetworkPort": "NetworkInterfaces", "PowerSubsystem": "Power", "PowerSupplies": "Power", "ThermalSubsystem": "Thermal", "Fans": "Thermal", "Sensors": "Thermal", "BootOption": "BootOptions", "UpdateService": "FirmwareInventory", "NetworkPorts": "NetworkInterfaces"}

func versionParts(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, v := range parts {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 || n > 9999 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
func versionAtLeast(value, minimum string) bool {
	v, ok := versionParts(value)
	if !ok || v[0] != 1 {
		return false
	}
	m, ok := versionParts(minimum)
	if !ok {
		return false
	}
	for i, n := range v {
		if n != m[i] {
			return n > m[i]
		}
	}
	return true
}
func (c *Collector) Collect(ctx context.Context, target Target, level string) (*Snapshot, error) {
	if e := target.Validate(); e != nil {
		return nil, e
	}
	features, ok := levelFeatures[level]
	if !ok {
		return nil, errors.New("BMC collection level must be L1, L2 or L3")
	}
	cfg := c.Config
	if c.Reader == nil || c.Raw == nil || cfg.MaxResources < 1 || cfg.MaxResources > 4096 || cfg.MaxDepth < 1 || cfg.MaxDepth > 16 || cfg.MaxBytes < MaxResourceBytes || cfg.MaxBytes > MaxTotalBytes {
		return nil, errors.New("BMC collection requires an adapter, durable raw sink and bounded configuration")
	}
	if _, ok = versionParts(cfg.MinimumVersion); !ok {
		return nil, errors.New("invalid minimum Redfish version")
	}
	origin := c.Reader.Origin()
	if origin != "sim" && origin != "real" {
		return nil, errors.New("BMC adapter has unknown evidence origin")
	}
	now := time.Now().UTC()
	out := &Snapshot{Version: "bmc-inventory/v1", Target: target, Level: level, Origin: origin, Source: "bmc", ParserVersion: ParserVersion, State: "unknown", StartedAt: now, Resources: []Resource{}, Failures: []Failure{}, Capabilities: map[string]string{}}
	wanted := map[string]bool{}
	for _, name := range features {
		wanted[name] = true
		out.Capabilities[name] = "unknown"
	}
	queue := []pendingResource{{path: "/redfish/v1/", feature: "ServiceRoot"}}
	queued := map[string]bool{"/redfish/v1/": true}
	var total int64
	addFailure := func(f Failure) {
		if len(out.Failures) < 256 {
			out.Failures = append(out.Failures, f)
		}
	}
	enqueue := func(parent, reference, feature string, depth int) {
		location, e := c.Reader.Resolve(parent, reference)
		if e == nil {
			location, e = RedfishInventoryPath(location)
		}
		if e != nil {
			addFailure(*failure(parent, "partial_failure", "invalid_resource_link", false))
			out.Capabilities[feature] = "partial"
			return
		}
		if queued[location] {
			return
		}
		if depth > cfg.MaxDepth || len(queued) >= cfg.MaxResources {
			addFailure(*failure(parent, "partial_failure", "resource_traversal_limit", false))
			out.Capabilities[feature] = "partial"
			return
		}
		queued[location] = true
		queue = append(queue, pendingResource{path: location, feature: feature, depth: depth})
	}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if e := ctx.Err(); e != nil {
			addFailure(*failure(item.path, "timeout", "collection_deadline", false))
			break
		}
		doc, e := c.Reader.Read(ctx, item.path)
		if e != nil {
			var f *Failure
			if !errors.As(e, &f) {
				f = failure(item.path, "partial_failure", "adapter_failed", true)
			}
			copy := *f
			copy.Path = item.path
			addFailure(copy)
			out.Capabilities[item.feature] = "partial"
			if f.Class == "version_unsupported" {
				out.Capabilities[item.feature] = "unsupported"
			}
			if f.Class == "authentication_failed" {
				break
			}
			continue
		}
		if doc.Path != item.path || doc.ReceivedAt.IsZero() || doc.ReceivedAt.After(time.Now().Add(5*time.Second)) || doc.ReceivedAt.Before(out.StartedAt.Add(-5*time.Second)) || len(doc.Body) == 0 || len(doc.Body) > MaxResourceBytes {
			addFailure(*failure(item.path, "partial_failure", "invalid_adapter_document", false))
			continue
		}
		total += int64(len(doc.Body))
		if total > cfg.MaxBytes {
			addFailure(*failure(item.path, "partial_failure", "collection_size_limit", false))
			break
		}
		var data map[string]any
		decoder := json.NewDecoder(bytes.NewReader(doc.Body))
		decoder.UseNumber()
		var trailing any
		if decoder.Decode(&data) != nil || data == nil || decoder.Decode(&trailing) != io.EOF {
			addFailure(*failure(item.path, "partial_failure", "invalid_json_resource", false))
			continue
		}
		structuredDigest := CanonicalDigest(doc.Body)
		if structuredDigest == "" {
			addFailure(*failure(item.path, "partial_failure", "json_number_limit", false))
			out.Capabilities[item.feature] = "partial"
			continue
		}
		raw, e := c.Raw.StoreRaw(ctx, target, item.path, doc.Body)
		digest := sha256.Sum256(doc.Body)
		if e != nil || raw.Key != RawKey(target, item.path, hex.EncodeToString(digest[:])) || (raw.Version == "" || raw.Version == "null") || raw.SHA256 != hex.EncodeToString(digest[:]) || raw.Size != int64(len(doc.Body)) {
			addFailure(*failure(item.path, "partial_failure", "raw_storage_unavailable", true))
			break
		}
		resourceType, _ := data["@odata.type"].(string)
		out.Resources = append(out.Resources, Resource{Path: item.path, Type: resourceType, Source: "bmc", ReceivedAt: doc.ReceivedAt, Raw: raw, DataSHA256: structuredDigest, Data: append(json.RawMessage(nil), doc.Body...)})
		if item.feature == "ServiceRoot" {
			version, _ := data["RedfishVersion"].(string)
			if !versionAtLeast(version, cfg.MinimumVersion) {
				addFailure(*failure(item.path, "version_unsupported", "redfish_version_unsupported", false))
				break
			}
		}
		if code := inventoryDocumentCode(item.path, item.feature, data, c.Reader.Resolve); code != "" {
			addFailure(*failure(item.path, "partial_failure", code, false))
			out.Capabilities[item.feature] = "partial"
			continue
		}
		if out.Capabilities[item.feature] != "partial" && out.Capabilities[item.feature] != "unsupported" {
			out.Capabilities[item.feature] = "collected"
		}
		if strings.HasPrefix(resourceType, "#ComputerSystem.") {
			serial, _ := data["SerialNumber"].(string)
			if strings.TrimSpace(serial) == target.Serial {
				out.IdentityMatched = true
			}
		}
		// Follow only inventory resource links and collection pagination. Account,
		// session, action and OEM links never enter this read-only inventory graph.
		var links func(map[string]any)
		links = func(object map[string]any) {
			keys := make([]string, 0, len(object))
			for key := range object {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value := object[key]
				if key == "Links" || key == "Boot" {
					if nested, yes := value.(map[string]any); yes {
						links(nested)
					}
					continue
				}
				if key == "Members@odata.nextLink" {
					if next, yes := value.(string); yes {
						enqueue(item.path, next, item.feature, item.depth)
					}
					continue
				}
				if key == "Members" {
					members, yes := value.([]any)
					if !yes {
						addFailure(*failure(item.path, "partial_failure", "invalid_resource_members", false))
						continue
					}
					for _, member := range members {
						if link, yes := member.(map[string]any); yes {
							if location, yes := link["@odata.id"].(string); yes {
								enqueue(item.path, location, item.feature, item.depth+1)
							}
						}
					}
					continue
				}
				feature := key
				if alias, yes := featureAliases[key]; yes {
					feature = alias
				}
				if !wanted[feature] {
					continue
				}
				if out.Capabilities[feature] == "unknown" {
					out.Capabilities[feature] = "advertised"
				}
				switch link := value.(type) {
				case map[string]any:
					if location, yes := link["@odata.id"].(string); yes {
						enqueue(item.path, location, feature, item.depth+1)
					}
				case []any:
					for _, member := range link {
						if object, yes := member.(map[string]any); yes {
							if location, yes := object["@odata.id"].(string); yes {
								enqueue(item.path, location, feature, item.depth+1)
							}
						}
					}
				}
			}
		}
		links(data)
	}
	for _, f := range collectionCoverageFailures(out.Resources) {
		addFailure(f.Failure)
		if f.Feature != "" {
			out.Capabilities[f.Feature] = "partial"
		}
	}
	c.collectIPMISupplement(ctx, out, &total)
	out.CollectedAt = time.Now().UTC()
	if !out.IdentityMatched {
		addFailure(*failure("/redfish/v1/Systems", "partial_failure", "target_serial_unconfirmed", false))
	}
	out.State = "completed"
	for name, state := range out.Capabilities {
		if name != "ServiceRoot" && state != "collected" {
			out.State = "partial"
		}
	}
	if len(out.Failures) > 0 {
		out.State = "partial"
	}
	if len(out.Resources) == 0 {
		out.State = "failed"
	}
	out.Degraded = out.State != "completed"
	return out, nil
}

// CanonicalDigest survives PostgreSQL JSONB whitespace/property ordering while
// Raw.SHA256 remains the digest of the exact original object-store bytes.
func CanonicalDigest(raw []byte) string {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return ""
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return ""
	}
	value, ok := canonicalJSONNumbers(value)
	if !ok {
		return ""
	}
	canonical, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

// PostgreSQL JSONB normalizes number spellings (1e3 -> 1000). Normalize
// decimals without float64 rounding, and bound exponents before allocation.
func canonicalJSONNumbers(value any) (any, bool) {
	switch v := value.(type) {
	case json.Number:
		number := string(v)
		negative := strings.HasPrefix(number, "-")
		number = strings.TrimPrefix(number, "-")
		mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(number), "e")
		exponent := 0
		if hasExponent {
			var e error
			exponent, e = strconv.Atoi(exponentText)
			if e != nil || exponent < -4096 || exponent > 4096 {
				return nil, false
			}
		}
		whole, fraction, _ := strings.Cut(mantissa, ".")
		if len(whole)+len(fraction) > 4096 {
			return nil, false
		}
		digits := strings.TrimLeft(whole+fraction, "0")
		if digits == "" {
			return json.Number("0"), true
		}
		scale := len(fraction) - exponent
		for scale > 0 && strings.HasSuffix(digits, "0") {
			digits = strings.TrimSuffix(digits, "0")
			scale--
		}
		var out string
		switch {
		case scale <= 0:
			out = digits + strings.Repeat("0", -scale)
		case scale >= len(digits):
			out = "0." + strings.Repeat("0", scale-len(digits)) + digits
		default:
			out = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
		}
		if negative {
			out = "-" + out
		}
		return json.Number(out), true
	case map[string]any:
		for key, item := range v {
			normalized, ok := canonicalJSONNumbers(item)
			if !ok {
				return nil, false
			}
			v[key] = normalized
		}
		return v, true
	case []any:
		for i, item := range v {
			normalized, ok := canonicalJSONNumbers(item)
			if !ok {
				return nil, false
			}
			v[i] = normalized
		}
		return v, true
	default:
		return value, true
	}
}
func RawKey(target Target, resource, digest string) string {
	pathHash := sha256.Sum256([]byte(resource))
	return "bmc/raw/" + target.ProjectID + "/" + target.AssetID + "/" + target.RequestID + "/" + hex.EncodeToString(pathHash[:]) + "/" + digest + ".json"
}
func (s Snapshot) Validate() error {
	if s.Version != "bmc-inventory/v1" || s.ParserVersion != ParserVersion || s.Target.Validate() != nil || levelFeatures[s.Level] == nil || (s.Origin != "sim" && s.Origin != "real") || s.Source != "bmc" || s.StartedAt.IsZero() || s.CollectedAt.Before(s.StartedAt) || len(s.Resources) > 4096 || len(s.Failures) > 256 {
		return errors.New("invalid BMC collection snapshot")
	}
	switch s.State {
	case "completed", "partial", "failed":
	default:
		return errors.New("invalid BMC collection outcome")
	}
	if s.State == "completed" && (!s.IdentityMatched || len(s.Resources) == 0 || len(s.Failures) > 0 || s.Degraded) {
		return errors.New("incomplete BMC evidence cannot be marked complete")
	}
	if s.State == "completed" {
		for _, feature := range levelFeatures[s.Level] {
			if s.Capabilities[feature] != "collected" {
				return errors.New("unknown required capability cannot be marked complete")
			}
		}
	}
	if s.State == "failed" && len(s.Resources) != 0 || s.State != "completed" && !s.Degraded {
		return errors.New("inconsistent BMC collection quality")
	}
	for _, f := range s.Failures {
		switch f.Class {
		case "authentication_failed", "timeout", "version_unsupported", "partial_failure":
		default:
			return errors.New("unknown BMC failure classification")
		}
		if f.Code == "" || len(f.Code) > 120 || len(f.Path) > 2048 {
			return errors.New("invalid BMC failure metadata")
		}
	}
	seen := map[string]bool{}
	identityMatched := false
	validatedFeatures := map[string]bool{}
	var total int64
	for _, r := range s.Resources {
		if !sha256Pattern.MatchString(r.Raw.SHA256) || r.Raw.Key != RawKey(s.Target, r.Path, r.Raw.SHA256) || (r.Raw.Version == "" || r.Raw.Version == "null") || r.Raw.Size < 1 || r.Raw.Size > MaxResourceBytes || r.Source != "bmc" || r.ReceivedAt.IsZero() || r.ReceivedAt.Before(s.StartedAt.Add(-5*time.Second)) || r.ReceivedAt.After(s.CollectedAt) || seen[r.Path] || r.DataSHA256 == "" || CanonicalDigest(r.Data) != r.DataSHA256 {
			return fmt.Errorf("BMC resource has no valid durable raw/structured evidence: %s", r.Path)
		}
		if canonical, e := InventoryPath(r.Path); e != nil || canonical != r.Path {
			return errors.New("invalid BMC resource path")
		}
		if strings.HasPrefix(r.Path, "/ipmi/") {
			matched, e := validateIPMIResource(r, s.Target)
			if e != nil {
				return e
			}
			if matched {
				identityMatched = true
			}
			seen[r.Path] = true
			total += r.Raw.Size
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(r.Data, &object) != nil || object == nil {
			return errors.New("BMC evidence must be an object")
		}
		var resourceType, serial string
		_ = json.Unmarshal(object["@odata.type"], &resourceType)
		_ = json.Unmarshal(object["SerialNumber"], &serial)
		decoder := json.NewDecoder(bytes.NewReader(r.Data))
		decoder.UseNumber()
		var data map[string]any
		if decoder.Decode(&data) != nil {
			return errors.New("invalid inventory document")
		}
		validEnvelope := false
		if s.State == "completed" && strings.HasPrefix(resourceType, "#ServiceRoot.") {
			version, _ := data["RedfishVersion"].(string)
			if !versionAtLeast(version, "1.0.0") {
				return errors.New("complete inventory has unsupported service root version")
			}
		}
		for feature := range inventoryTypes {
			if inventoryType(feature, resourceType) && inventoryDocumentCode(r.Path, feature, data, retainedInventoryResolve) == "" {
				validatedFeatures[feature] = true
				validEnvelope = true
			}
		}
		if s.State == "completed" && !validEnvelope {
			return errors.New("complete inventory contains invalid resource metadata")
		}
		if resourceType != r.Type {
			return errors.New("BMC resource type does not match raw evidence")
		}
		if validEnvelope && strings.HasPrefix(resourceType, "#ComputerSystem.") && strings.TrimSpace(serial) == s.Target.Serial {
			identityMatched = true
		}
		seen[r.Path] = true
		total += r.Raw.Size
	}
	if s.State == "completed" {
		if !validatedFeatures["ServiceRoot"] {
			return errors.New("complete inventory lacks its retained service root")
		}
		if len(collectionCoverageFailures(s.Resources)) > 0 {
			return errors.New("complete inventory has missing or cyclic collection pages/members")
		}
		for _, feature := range levelFeatures[s.Level] {
			if !validatedFeatures[feature] {
				return errors.New("complete capability lacks retained typed inventory evidence")
			}
		}
	}
	if s.Fallback != nil {
		count := 0
		for _, r := range s.Resources {
			if strings.HasPrefix(r.Path, "/ipmi/") {
				count++
			}
		}
		if s.Fallback.Protocol != "ipmi" || s.Fallback.ParserVersion != IPMIParserVersion || s.Fallback.Reason != fallbackReason(&s) || s.Fallback.Reason == "" || s.Fallback.ResourceCount != count || !s.Degraded {
			return errors.New("invalid IPMI degradation metadata")
		}
	} else {
		for _, r := range s.Resources {
			if strings.HasPrefix(r.Path, "/ipmi/") {
				return errors.New("IPMI evidence lacks its degradation reason")
			}
		}
	}
	if s.IdentityMatched != identityMatched {
		return errors.New("BMC identity assertion does not match retained evidence")
	}
	if total > MaxTotalBytes {
		return errors.New("BMC raw evidence exceeds collection budget")
	}
	return nil
}

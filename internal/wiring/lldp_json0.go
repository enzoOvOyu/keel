package wiring

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// The fixed json0 output is array-shaped, including singleton values. We do
// not accept the shape-changing json format or fall back to human text output.
// Unknown optional lldpd TLVs are retained in the original artifact, not used
// to infer an identity. Input must come from a successful fixed CLI operation.
type lldpValue struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}
type lldpChassis struct {
	ID          []lldpValue `json:"id"`
	Name        []lldpValue `json:"name"`
	Description []lldpValue `json:"descr"`
}
type lldpPort struct {
	ID []lldpValue `json:"id"`
}
type lldpInterface struct {
	Name    string        `json:"name"`
	Via     string        `json:"via"`
	Chassis []lldpChassis `json:"chassis"`
	Port    []lldpPort    `json:"port"`
}
type lldpDocument struct {
	LLDP []struct {
		Interfaces []lldpInterface `json:"interface"`
	} `json:"lldp"`
}

func decodeLLDPJSON0(raw []byte) ([]lldpInterface, error) {
	if len(raw) == 0 || len(raw) > MaxCaptureBytes {
		return nil, errors.New("LLDP JSON output exceeds bounds")
	}
	// A small token walk rejects duplicate keys before standard Go decoding can
	// silently replace an earlier identity. The original bytes remain untouched.
	dec := json.NewDecoder(bytes.NewReader(raw))
	if e := uniqueJSONValue(dec, 0); e != nil {
		return nil, e
	}
	if _, e := dec.Token(); e != io.EOF {
		return nil, errors.New("trailing LLDP JSON data")
	}
	var doc lldpDocument
	if e := json.Unmarshal(raw, &doc); e != nil {
		return nil, e
	}
	if len(doc.LLDP) != 1 || doc.LLDP[0].Interfaces == nil || len(doc.LLDP[0].Interfaces) > 4096 {
		return nil, errors.New("unsupported LLDP json0 structure")
	}
	return doc.LLDP[0].Interfaces, nil
}
func uniqueJSONValue(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("LLDP JSON nesting exceeds bounds")
	}
	token, e := dec.Token()
	if e != nil {
		return e
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, e := dec.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate LLDP JSON key")
			}
			seen[name] = true
			if e = uniqueJSONValue(dec, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for dec.More() {
			if e = uniqueJSONValue(dec, depth+1); e != nil {
				return e
			}
		}
	default:
		return errors.New("invalid LLDP JSON delimiter")
	}
	_, e = dec.Token()
	return e
}
func normalizedLLDPIdentity(row lldpInterface) (Identity, error) {
	if len(row.Chassis) != 1 || len(row.Port) != 1 {
		return Identity{}, errors.New("ambiguous LLDP chassis or port")
	}
	c, p := row.Chassis[0], row.Port[0]
	if len(c.ID) != 1 || len(c.Name) != 1 || len(c.Description) > 1 || len(p.ID) != 1 {
		return Identity{}, errors.New("ambiguous or missing LLDP identity fields")
	}
	value := Identity{ChassisID: c.ID[0].Value, Sysname: c.Name[0].Value, PortID: p.ID[0].Value}
	if len(c.Description) == 1 {
		value.Sysdesc = c.Description[0].Value
	}
	if value.ChassisID == "" || value.PortID == "" || len(value.ChassisID) > 128 || len(value.PortID) > 128 || len(value.Sysname) > 253 || len(value.Sysdesc) > 1024 || strings.ContainsAny(value.ChassisID+value.PortID+value.Sysname+value.Sysdesc, "\r\n\x00") {
		return Identity{}, errors.New("invalid LLDP identity text")
	}
	return value, nil
}

// ParseNeighborsJSON0 distinguishes an explicit empty readable neighbor array
// from a missing/unsupported CLI result. Multiplicity is preserved so the
// evaluator can block ambiguity instead of arbitrarily choosing one neighbor.
func ParseNeighborsJSON0(raw []byte, interfaces []string) (map[string][]fabricmon.Neighbor, error) {
	if len(interfaces) == 0 || len(interfaces) > MaxPorts {
		return nil, errors.New("explicit LLDP interfaces required")
	}
	selected := map[string]bool{}
	out := map[string][]fabricmon.Neighbor{}
	for _, name := range interfaces {
		if !interfaceName.MatchString(name) || selected[name] {
			return nil, errors.New("invalid or duplicate selected LLDP interface")
		}
		selected[name] = true
		out[name] = []fabricmon.Neighbor{}
	}
	rows, e := decodeLLDPJSON0(raw)
	if e != nil {
		return nil, e
	}
	for _, row := range rows {
		if !selected[row.Name] || row.Via != "LLDP" {
			return nil, errors.New("LLDP result is outside frozen interfaces or protocol")
		}
		identity, e := normalizedLLDPIdentity(row)
		if e != nil {
			return nil, e
		}
		if len(out[row.Name]) >= 64 {
			return nil, errors.New("LLDP neighbor bound exceeded")
		}
		out[row.Name] = append(out[row.Name], fabricmon.Neighbor{ChassisID: identity.ChassisID, PortID: identity.PortID, Sysname: identity.Sysname, Sysdesc: identity.Sysdesc})
	}
	return out, nil
}
func ParseLocalJSON0(raw []byte, interfaces []string) (map[string]Identity, error) {
	if len(interfaces) == 0 || len(interfaces) > MaxPorts {
		return nil, errors.New("explicit LLDP interfaces required")
	}
	selected := map[string]bool{}
	for _, name := range interfaces {
		if !interfaceName.MatchString(name) || selected[name] {
			return nil, errors.New("invalid or duplicate local LLDP interface")
		}
		selected[name] = true
	}
	rows, e := decodeLLDPJSON0(raw)
	if e != nil {
		return nil, e
	}
	out := map[string]Identity{}
	for _, row := range rows {
		if !selected[row.Name] {
			return nil, errors.New("local LLDP identity outside frozen interfaces")
		}
		if _, seen := out[row.Name]; seen {
			return nil, errors.New("duplicate local LLDP interface")
		}
		identity, e := normalizedLLDPIdentity(row)
		if e != nil {
			return nil, e
		}
		out[row.Name] = identity
	}
	if len(out) != len(selected) {
		return nil, errors.New("selected local LLDP identity missing")
	}
	return out, nil
}

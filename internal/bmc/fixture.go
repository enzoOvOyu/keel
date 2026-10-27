package bmc

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// FixtureReader is an immutable protocol boundary. It uses the same documents,
// limits, raw capture, parser and failures as HTTPS; no alternate business path.
type FixtureReader struct {
	documents map[string][]byte
	failures  map[string]Failure
}

func NewFixtureReader(documents map[string][]byte, failures map[string]Failure) *FixtureReader {
	f := &FixtureReader{documents: map[string][]byte{}, failures: map[string]Failure{}}
	for p, raw := range documents {
		f.documents[p] = append([]byte(nil), raw...)
	}
	for p, v := range failures {
		f.failures[p] = v
	}
	return f
}
func (f *FixtureReader) Origin() string { return "sim" }
func (f *FixtureReader) Resolve(parent, reference string) (string, error) {
	return CanonicalPath(parent, reference)
}
func (f *FixtureReader) Read(ctx context.Context, path string) (Document, error) {
	if e := ctx.Err(); e != nil {
		return Document{}, e
	}
	if failure, ok := f.failures[path]; ok {
		copy := failure
		copy.Path = path
		return Document{}, &copy
	}
	raw, ok := f.documents[path]
	if !ok {
		return Document{}, &Failure{Path: path, Class: "version_unsupported", Code: "resource_unsupported", HTTPStatus: 404}
	}
	return Document{Path: path, Body: append([]byte(nil), raw...), ReceivedAt: time.Now().UTC()}, nil
}

// NewSimReader synthesizes only explicitly supplied inventory identity. It does
// not infer healthy power, sensors, GPU count, links or hardware capabilities.
func NewSimReader(target Target) (*FixtureReader, error) {
	if e := target.Validate(); e != nil {
		return nil, e
	}
	documents := map[string][]byte{}
	add := func(path string, value any) { raw, _ := json.Marshal(value); documents[path] = raw }
	add("/redfish/v1/", map[string]any{"@odata.id": "/redfish/v1/", "@odata.type": "#ServiceRoot.v1_0_0.ServiceRoot", "RedfishVersion": "1.0.0", "Systems": map[string]string{"@odata.id": "/redfish/v1/Systems"}, "Managers": map[string]string{"@odata.id": "/redfish/v1/Managers"}, "Chassis": map[string]string{"@odata.id": "/redfish/v1/Chassis"}})
	for _, kind := range []string{"Systems", "Managers", "Chassis"} {
		members := []map[string]string{}
		if kind == "Systems" {
			members = append(members, map[string]string{"@odata.id": "/redfish/v1/Systems/sim"})
		}
		add("/redfish/v1/"+kind, map[string]any{"@odata.id": "/redfish/v1/" + kind, "@odata.type": "#" + strings.TrimSuffix(kind, "s") + "Collection.Collection", "Members": members, "Members@odata.count": len(members)})
	}
	add("/redfish/v1/Systems/sim", map[string]any{"@odata.id": "/redfish/v1/Systems/sim", "@odata.type": "#ComputerSystem.v1_0_0.ComputerSystem", "Id": "sim", "SerialNumber": target.Serial})
	return NewFixtureReader(documents, nil), nil
}

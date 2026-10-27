package wiring

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

// EvaluateArchive is the only archive-to-verdict path. Expectation and binding
// come from the immutable command, not the capture. Historical evaluation uses
// its original stop time; callers separately assess CURRENT gate admissibility.
func EvaluateArchive(raw []byte, expected Expectation, binding ReadbackBinding, hostOrigin string) (*Archive, Result, error) {
	var result Result
	archive, e := DecodeArchive(raw)
	if e != nil {
		return nil, result, e
	}
	want, _ := json.Marshal(expected)
	got, _ := json.Marshal(archive.Expectation)
	if expected.Validate() != nil || !bytes.Equal(want, got) || archive.CommandID != binding.CommandID || expected.ProjectID != binding.ProjectID || hostOrigin != "real" && hostOrigin != "sim" || archive.Capture.Origin != hostOrigin {
		return nil, result, ErrCaptureRejected
	}
	if archive.Readback != nil {
		want, _ = json.Marshal(binding)
		got, _ = json.Marshal(archive.Readback.Binding)
		if !bytes.Equal(want, got) || archive.Readback.Origin != binding.Origin {
			return nil, result, ErrCaptureRejected
		}
	}
	switches := map[string]SwitchEvidence{}
	if archive.Readback != nil {
		for i := range archive.Readback.Devices {
			sample := &archive.Readback.Devices[i]
			switches[sample.Serial] = SwitchEvidence{Origin: archive.Readback.Origin, Snapshot: sample}
		}
	}
	at := archive.Capture.AdvertisingStoppedAt
	if at.IsZero() || at.Before(archive.Capture.StartedAt) || at.Sub(archive.Capture.StartedAt) > 2*time.Minute {
		return nil, result, ErrCaptureRejected
	}
	result = Evaluate(expected, &archive.Capture, switches, at)
	return archive, result, nil
}

// HostEvidenceHash binds host-side original bytes/state independently of late
// original-command collector queries. Attaching a retained switch response can
// never change the host identity, observations, stop time or restoration proof.
func HostEvidenceHash(archive *Archive) (string, error) {
	if archive == nil {
		return "", ErrCaptureRejected
	}
	copy := *archive
	copy.Readback = nil
	projected, e := projectArchive(&copy)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(projected)
	if e != nil {
		return "", e
	}
	return Hash(raw), nil
}

// AttachOriginalReadback joins already completed ORIGINAL commands after an
// unknown read. It never recollects. Partial/stopped requests leave a blocked
// archive; any appended evidence must lie inside the original advertising window.
func AttachOriginalReadback(original *Archive, binding ReadbackBinding, exchanges []ReadbackExchange) (*Archive, error) {
	raw, e := json.Marshal(original)
	if e != nil {
		return nil, e
	}
	archive, e := DecodeArchive(raw)
	if e != nil {
		return nil, e
	}
	if archive.CommandID != binding.CommandID || archive.Expectation.ProjectID != binding.ProjectID || !entityID.MatchString(binding.ExecutionID) || !entityID.MatchString(binding.AttemptID) || binding.FencingToken < 1 || binding.Origin != "device" && binding.Origin != "protocol_fixture" {
		return nil, ErrCaptureRejected
	}
	before, e := HostEvidenceHash(archive)
	if e != nil {
		return nil, e
	}
	scope := ReadbackScope{PlanDigest: archive.PlanDigest, CommandID: archive.CommandID, ScopeHash: archive.Expectation.ScopeHash, AdvertisingAt: archive.Capture.AdvertisingAt, Ports: archive.Expectation.Ports}
	requests, e := BuildReadbackRequests(binding, scope)
	if e != nil {
		if len(exchanges) != 0 {
			return nil, ErrCaptureRejected
		}
		// Host observation can fail before a valid advertising window exists.
		return archive, nil
	}
	allowed := map[string]fabricboot.CollectorRecollectRequest{}
	for _, request := range requests {
		allowed[request.CommandID] = request
	}
	byID := map[string]ReadbackExchange{}
	for _, exchange := range exchanges {
		want, exists := allowed[exchange.Request.CommandID]
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(exchange.Request)
		if _, duplicate := byID[exchange.Request.CommandID]; duplicate || !exists || !bytes.Equal(a, b) || exchange.Response.Validate(want, archive.Capture.AdvertisingStoppedAt) != nil {
			return nil, ErrCaptureRejected
		}
		byID[exchange.Request.CommandID] = exchange
	}
	archive.Readback = nil
	if len(byID) == len(requests) {
		ordered := make([]ReadbackExchange, 0, len(requests))
		for _, request := range requests {
			ordered = append(ordered, byID[request.CommandID])
		}
		devices, e := projectReadback(binding, scope, ordered, archive.Capture.AdvertisingStoppedAt)
		if e != nil {
			return nil, e
		}
		archive.Readback = &Readback{Binding: binding, PlanDigest: scope.PlanDigest, CommandID: scope.CommandID, ScopeHash: scope.ScopeHash, Origin: binding.Origin, Exchanges: ordered, Devices: devices}
	}
	projected, e := projectArchive(archive)
	if e != nil {
		return nil, e
	}
	after, e := HostEvidenceHash(projected)
	if e != nil || before != after {
		return nil, ErrCaptureRejected
	}
	return projected, nil
}

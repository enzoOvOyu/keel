package wiring

import (
	"bytes"
	"encoding/json"
	"time"
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

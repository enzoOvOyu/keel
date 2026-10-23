package platform

import (
	"context"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

type ZTPSimRunInput struct {
	ExpectedVersion   int64                   `json:"expected_version"`
	DORA              *fabricboot.DORARequest `json:"dora"`
	CredentialVerdict string                  `json:"credential_verdict,omitempty"`
}

// RunZTPSimSession drives an existing simulation through its remaining stages.
// Each stage reuses the same gate as manual advance and remains independently audited.
func (s *Store) RunZTPSimSession(ctx context.Context, a *AuthSession, project *ProjectSummary, sessionID string, input ZTPSimRunInput, trace string) (*ZTPSessionDetail, error) {
	if input.ExpectedVersion < 1 || input.DORA == nil {
		return nil, apiError(400, "ZTP_SIM_RUN_INPUT_INVALID", "Expected version and mock DORA request are required")
	}
	session, err := s.GetZTPSession(ctx, a, project, sessionID)
	if err != nil {
		return nil, err
	}
	if session.StateVersion != input.ExpectedVersion {
		return nil, apiError(409, "ZTP_STATE_VERSION_CONFLICT", "ZTP session state changed")
	}
	if nextZTPStage(session.State) == "" {
		return nil, apiError(409, "ZTP_TRANSITION_INVALID", "No next ZTP stage")
	}
	for i := 0; i < 8; i++ {
		next := nextZTPStage(session.State)
		if next == "" {
			return session, nil
		}
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: session.StateVersion, EvidenceRef: "sim-auto:" + next}
		if next == "dhcp" {
			action.DORA = input.DORA
		}
		if next == "managed" {
			action.CredentialVerdict = input.CredentialVerdict
		}
		advanced, advanceErr := s.AdvanceZTPSimSession(ctx, a, project, sessionID, action, trace)
		if advanceErr == nil {
			session = advanced
			continue
		}
		var gate *Error
		if !errors.As(advanceErr, &gate) || gate.Status != 409 || gate.Code == "ZTP_STATE_VERSION_CONFLICT" {
			return nil, advanceErr
		}
		failure := ZTPSessionAction{Action: "fail", ExpectedVersion: session.StateVersion, Phenomenon: fmt.Sprintf("Simulation stage %s blocked", next), Cause: gate.Code, NextAction: "Correct mock plan, approval or BOM data, then retry the simulation"}
		return s.AdvanceZTPSimSession(ctx, a, project, sessionID, failure, trace)
	}
	if nextZTPStage(session.State) == "" {
		return session, nil
	}
	return nil, apiError(500, "ZTP_SIM_RUN_LIMIT", "Simulation stage limit exceeded")
}

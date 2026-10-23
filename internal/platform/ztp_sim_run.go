package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"time"
)

type ZTPSimRunInput struct {
	ExpectedVersion   int64                   `json:"expected_version"`
	DORA              *fabricboot.DORARequest `json:"dora"`
	CredentialVerdict string                  `json:"credential_verdict,omitempty"`
}

func simRunAction(session *ZTPSessionDetail, input ZTPSimRunInput) ZTPSessionAction {
	next := nextZTPStage(session.State)
	action := ZTPSessionAction{Action: "advance", ExpectedVersion: session.StateVersion, EvidenceRef: "sim-auto:" + next}
	if next == "dhcp" {
		action.DORA = input.DORA
	}
	if next == "managed" {
		action.CredentialVerdict = input.CredentialVerdict
	}
	return action
}

func simRunGateFailure(session *ZTPSessionDetail, next string, err error) (*ZTPSessionAction, bool) {
	var gate *Error
	if !errors.As(err, &gate) || gate.Status != 409 || gate.Code == "ZTP_STATE_VERSION_CONFLICT" {
		return nil, false
	}
	action := ZTPSessionAction{Action: "fail", ExpectedVersion: session.StateVersion, Phenomenon: fmt.Sprintf("Simulation stage %s blocked", next), Cause: gate.Code, NextAction: "Correct mock plan, approval or BOM data, then retry the simulation"}
	return &action, true
}

// RunZTPSimSession persists safe simulation inputs, then drives remaining stages.
// Every stage uses the same backend gate as manual advance; a retry worker can
// later resume the saved fixture without an operator or borrowed identity.
func (s *Store) RunZTPSimSession(ctx context.Context, a *AuthSession, project *ProjectSummary, sessionID string, input ZTPSimRunInput, trace string) (*ZTPSessionDetail, error) {
	if input.ExpectedVersion < 1 || input.DORA == nil {
		return nil, apiError(400, "ZTP_SIM_RUN_INPUT_INVALID", "Expected version and mock DORA request are required")
	}
	if input.CredentialVerdict != "" && input.CredentialVerdict != "reachable" && input.CredentialVerdict != "expired" && input.CredentialVerdict != "unreachable" {
		return nil, apiError(400, "ZTP_SIM_CREDENTIAL_VERDICT_INVALID", "Invalid simulated credential verdict")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	session, err := getZTPSessionTx(ctx, tx, project.ID, sessionID, true)
	if err != nil {
		return nil, err
	}
	if session.Mode != "sim" {
		return nil, apiError(409, "ZTP_DEVICE_SESSION_READ_ONLY", "Simulation actions cannot change a device session")
	}
	if session.StateVersion != input.ExpectedVersion {
		return nil, apiError(409, "ZTP_STATE_VERSION_CONFLICT", "ZTP session state changed")
	}
	if nextZTPStage(session.State) == "" && !(session.State == "failed" && session.NextRetryAt != nil && session.Attempt <= session.RetryMax) {
		return nil, apiError(409, "ZTP_TRANSITION_INVALID", "No next ZTP stage")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if session.State == "failed" {
		now := time.Now().UTC().Truncate(time.Microsecond)
		event := ZTPSessionEvent{Seq: session.StateVersion + 1, FromState: "failed", ToState: "failed", EventCode: "sim.run_input_updated", Attempt: session.Attempt, OccurredAt: now}
		if err = insertZTPEvent(ctx, tx, sessionID, event); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE ztp_sessions SET sim_run_input=$2,state_version=$3,updated_at=$4 WHERE id=$1`, sessionID, raw, event.Seq, now); err != nil {
			return nil, err
		}
		session.StateVersion = event.Seq
		session.UpdatedAt = now
		if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.sim.run_input_update", sessionID, trace, map[string]any{"credential_verdict": input.CredentialVerdict}); err != nil {
			return nil, err
		}
		if err = loadZTPEvents(ctx, tx, session); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return session, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ztp_sessions SET sim_run_input=$2 WHERE id=$1`, sessionID, raw); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.sim.run_input_set", sessionID, trace, map[string]any{"credential_verdict": input.CredentialVerdict}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for i := 0; i < 8; i++ {
		next := nextZTPStage(session.State)
		if next == "" {
			return session, nil
		}
		advanced, advanceErr := s.AdvanceZTPSimSession(ctx, a, project, sessionID, simRunAction(session, input), trace)
		if advanceErr == nil {
			session = advanced
			continue
		}
		failure, ok := simRunGateFailure(session, next, advanceErr)
		if !ok {
			return nil, advanceErr
		}
		return s.AdvanceZTPSimSession(ctx, a, project, sessionID, *failure, trace)
	}
	if nextZTPStage(session.State) == "" {
		return session, nil
	}
	return nil, apiError(500, "ZTP_SIM_RUN_LIMIT", "Simulation stage limit exceeded")
}

func (s *Store) resumeZTPSimRunTx(ctx context.Context, tx *sql.Tx, projectID, sessionID string, input ZTPSimRunInput) error {
	for i := 0; i < 8; i++ {
		session, err := getZTPSessionTx(ctx, tx, projectID, sessionID, true)
		if err != nil {
			return err
		}
		next := nextZTPStage(session.State)
		if next == "" {
			return nil
		}
		_, advanceErr := s.advanceZTPSimSessionTx(ctx, tx, projectID, sessionID, simRunAction(session, input), "", "")
		if advanceErr == nil {
			continue
		}
		failure, ok := simRunGateFailure(session, next, advanceErr)
		if !ok {
			return advanceErr
		}
		_, err = s.advanceZTPSimSessionTx(ctx, tx, projectID, sessionID, *failure, "", "")
		return err
	}
	session, err := getZTPSessionTx(ctx, tx, projectID, sessionID, true)
	if err != nil {
		return err
	}
	if nextZTPStage(session.State) == "" {
		return nil
	}
	return apiError(500, "ZTP_SIM_RUN_LIMIT", "Simulation stage limit exceeded")
}

package execution

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

type CreateProvisionInstallationInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	ScopeID         string `json:"scope_id"`
	Reason          string `json:"reason"`
}

// CreateProvisionInstallation starts a distinct no-retry native execution.
// Disk selection is deliberately later, after authenticated read-only facts;
// this action neither erases a disk nor reuses the preparation's released lock.
func (s *Service) CreateProvisionInstallation(ctx context.Context, a *Actor, project, requestID string, in CreateProvisionInstallationInput, key, trace string) (*Execution, error) {
	if s.ProvisionStore == nil || !s.ProvisionStore.ProvisionInstallEnabled || s.ProvisionStore.MediaObjects == nil || len(s.ProvisionStore.ProvisionImagePublicKey) != ed25519.PublicKeySize {
		return nil, failure(503, "PROVISION_INSTALL_DISABLED", "Controlled installation worker and independent image trust are disabled")
	}
	if !opaqueID.MatchString(requestID) || !opaqueID.MatchString(in.ScopeID) || in.ExpectedVersion < 1 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 500 || strings.ContainsAny(in.Reason, "\x00\r\n") {
		return nil, invalid("Exact prepared request version, approved boot scope and reason required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); err != nil {
		return nil, err
	}
	idem, prior, err := idempotency(ctx, tx, a, "provision.install:"+project+":"+requestID, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	if err = advisory(ctx, tx, "provision.install.consume:"+requestID); err != nil {
		return nil, err
	}
	// Source identity is immutable. Read it without a row lock, then take the
	// shared machine-admission mutex before pinning the request, matching source
	// preparation's expiry path (machine mutex, then request UPDATE).
	var machineID string
	if err = tx.QueryRowContext(ctx, "SELECT machine_id FROM provision_requests WHERE project_id=$1 AND id=$2", project, requestID).Scan(&machineID); err != nil {
		if err == sql.ErrNoRows {
			return nil, platform.ErrNotFound
		}
		return nil, err
	}
	if err = platform.LockProvisionMachineTx(ctx, tx, machineID); err != nil {
		return nil, err
	}
	request, err := platform.LoadProvisionRequestTx(ctx, tx, project, requestID)
	if err != nil {
		return nil, err
	}
	if request.State != "prepared" || request.StateVersion != in.ExpectedVersion || request.ExecutionID == nil || !platform.ProvisionSnapshotValid(request) || request.Snapshot.Media.ValidateInstallable() != nil {
		return nil, conflict("Current prepared source with an explicit installable recipe required")
	}
	if err = platform.ValidateProvisionRequestMetadataTx(ctx, tx, request, time.Now()); err != nil {
		return nil, err
	}
	scope, err := platform.LoadApprovedProvisionScopeTx(ctx, tx, project, in.ScopeID)
	if err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installations WHERE request_id=$1 OR (machine_id=$2 AND state NOT IN('registered','failed','canceled')))`, requestID, request.MachineID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, conflict("Prepared request already consumed or machine has an active/unknown installation")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	until := now.Add(30 * time.Minute)
	if request.ExpiresAt.Before(until) {
		until = request.ExpiresAt
	}
	if !until.After(now.Add(2 * time.Minute)) {
		return nil, conflict("Preparation expires too soon; obtain a fresh preparation request")
	}
	installationID := platform.RandomID()
	x := &Execution{ID: platform.RandomID(), ProjectID: project, Mode: "provision", State: "created", StateVersion: 1, DesiredState: "running", Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	target := Target{ProjectID: project, ResourceKey: request.MachineID, Mode: "provision", Name: request.Snapshot.Target.Hostname, Revision: request.Snapshot.Target.AssetVersion, ModelVersion: request.Snapshot.Target.ModelVersionID, CreatedAt: now, ProvisionRequestID: requestID, ProvisionSnapshotSHA256: request.SnapshotSHA256, ProvisionInstallationID: installationID}
	if err = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key)VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key)DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, request.MachineID).Scan(&target.ID); err != nil {
		return nil, err
	}
	task := Task{ID: "provision-install", Type: "provision.install", TimeoutMS: int(until.Sub(now) / time.Millisecond), Idempotent: false, Retry: 0, Resources: []string{"device"}}
	definition := Definition{APIVersion: "keel.provision-installation/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "Provision installation " + installationID[:8], Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	x.FlowVersionID = flow.ID
	x.TargetSetHash = hash(jsonBytes([]string{target.ID}))
	x.Parameters = map[string]any{"provision_installation_id": installationID, "provision_request_id": requestID, "provision_snapshot_sha256": request.SnapshotSHA256}
	x.Snapshot = Snapshot{Mode: "provision", Flow: flow, Targets: []Target{target}, Parameters: x.Parameters, TargetSetHash: x.TargetSetHash, FrozenAt: now, Admission: "Independent prepared source, approved current boot domain and fresh exclusive machine lease. Authenticated read-only boot precedes independent disk-plan approval. No automatic write permission or host trust."}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$5)", flow.FlowID, project, flow.Name, a.User.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at)VALUES($1,$2,$3,1,$4,$5,$6,$7)", flow.ID, flow.FlowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at)VALUES($1,$2,$3,'provision',$4,$5,$6,1,100,$7,$8)`, x.ID, project, flow.ID, jsonBytes(x.Parameters), jsonBytes(x.Snapshot), x.TargetSetHash, a.User.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_execution_targets(execution_id,target_id,snapshot)VALUES($1,$2,$3)", x.ID, target.ID, jsonBytes(target)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at)VALUES($1,$2,$3,'provision-install',0,1,$4,'provision',$5)`, platform.RandomID(), x.ID, target.ID, platform.RandomID(), now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO provision_installations(id,project_id,request_id,machine_id,execution_id,scope_id,scope_sha256,snapshot_sha256,grant_id,created_by,reason,created_at,expires_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$12)`, installationID, project, requestID, request.MachineID, x.ID, scope.ID, scope.ScopeSHA256, request.SnapshotSHA256, platform.RandomID(), a.User.ID, in.Reason, now, until); err != nil {
		return nil, err
	}
	if err = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "provision", "installation_id": installationID, "installation_performed": false}); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "provision.installation.created", installationID, trace, map[string]any{"execution_id": x.ID, "request_id": requestID, "scope_id": scope.ID, "snapshot_sha256": request.SnapshotSHA256}); err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, in, x); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

func (s *Service) runProvisionInstallationClaim(ctx context.Context, c *Claim) error {
	if s.ProvisionStore == nil || c.Task.Type != "provision.install" || c.Target.ProvisionInstallationID == "" {
		return invalid("Controlled installation adapter unavailable")
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var beat sync.WaitGroup
	beat.Add(1)
	go func() {
		defer beat.Done()
		ticker := time.NewTicker(c.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				check, done := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				kill, err := s.Heartbeat(check, c)
				done()
				if err != nil || kill {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(stop); beat.Wait() }()
	started := time.Now().UTC().Truncate(time.Microsecond)
	verified, err := s.ProvisionStore.VerifyProvisionInstallationSources(work, c.Target.ProjectID, c.Target.ProvisionInstallationID)
	if err != nil {
		quarantine, done := context.WithTimeout(context.Background(), 5*time.Second)
		err = s.ProvisionStore.RecordMediaIntegrityFailure(quarantine, err, "", "worker")
		done()
	}
	if err == nil {
		_, err = s.ProvisionStore.ActivateProvisionInstallation(work, c.Target.ProjectID, c.Target.ProvisionInstallationID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, verified)
	}
	if err == nil {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for err == nil {
			select {
			case <-work.Done():
				err = work.Err()
			case <-ticker.C:
				check, done := context.WithTimeout(work, 3*time.Second)
				tx, e := s.DB.BeginTx(check, nil)
				if e == nil {
					var installation *platform.ProvisionInstallation
					installation, e = platform.LoadProvisionInstallationBindingTx(check, tx, c.Target.ProjectID, c.Target.ProvisionInstallationID)
					if e == nil && installation.State == "registered" {
						tx.Rollback()
						done()
						return s.Complete(work, c)
					}
					if e == nil && installation.State != "failed" && installation.State != "canceled" {
						_, _, _, e = platform.ValidateProvisionInstallationTx(check, tx, installation, time.Now())
					}
					if e == nil && (installation.State == "failed" || installation.State == "canceled") {
						e = platform.ErrProvisionBootDenied
					}
					tx.Rollback()
				}
				done()
				if e == nil {
					finished, sshErr := s.runProvisionFirstSSH(work, c)
					if finished {
						return sshErr
					}
					if sshErr != nil {
						e = sshErr
					}
				}
				if e != nil {
					err = e
				}
			}
		}
	}
	// Without the genuine first-SSH terminal proof, only a pre-write stop is
	// safely classified. Any issued permit retains the resource as unknown;
	// never turn a timeout into an invented installation failure or success.
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	reason := "boot_prewrite_stopped"
	if ctx.Err() != nil || work.Err() != nil {
		reason = "boot_prewrite_context_stopped"
	}
	x, stopErr := s.ProvisionStore.StopProvisionBeforeWrite(finish, c.Target.ProjectID, c.Target.ProvisionInstallationID, c.Attempt.ExecutionID, work.Err() != nil, reason)
	if stopErr != nil {
		return stopErr
	}
	result := "failed"
	if x.State == "canceled" {
		result = "cancelled"
	}
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: result, StartedAt: started, FinishedAt: time.Now().UTC().Truncate(time.Microsecond), Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": x.ID, "request_id": x.RequestID, "snapshot_sha256": x.SnapshotSHA256, "reason": reason, "installation_performed": false}}
	if err = s.RecordReceipt(finish, receipt); err != nil {
		return err
	}
	return s.Complete(finish, c)
}

func installationReceiptTarget(ctx context.Context, tx *sql.Tx, target *Target, a Attempt, r Receipt) error {
	x, err := platform.LoadProvisionInstallationTx(ctx, tx, target.ProjectID, target.ProvisionInstallationID)
	if err != nil {
		return err
	}
	if x.State == "registered" {
		return registeredInstallationReceiptTarget(ctx, tx, target, a, r, x)
	}
	if x.ExecutionID != a.ExecutionID || x.RequestID != target.ProvisionRequestID || x.SnapshotSHA256 != target.ProvisionSnapshotSHA256 || x.PermitIssuedAt != nil || (x.State != "failed" && x.State != "canceled") || r.Output["schema"] != "keel.provision-installation-proof/v1" || r.Output["installation_id"] != x.ID || r.Output["request_id"] != x.RequestID || r.Output["snapshot_sha256"] != x.SnapshotSHA256 || r.Output["installation_performed"] != false || r.Result == "success" || (r.Result == "cancelled") != (x.State == "canceled") {
		return conflict("Known pre-write stop proof does not match installation evidence")
	}
	if x.AttemptID != nil && (*x.AttemptID != a.ID || x.CommandID == nil || *x.CommandID != a.CommandID || x.FencingToken == nil || *x.FencingToken != a.FencingToken) {
		return conflict("Installation attempt/fence differs")
	}
	return nil
}

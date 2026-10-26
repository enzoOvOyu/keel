package execution

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

type SSHCreateInput struct {
	ArtifactMediaID     string   `json:"artifact_media_id,omitempty"`
	PreferScriptCache   bool     `json:"prefer_script_cache,omitempty"`
	CacheCursor         string   `json:"cache_cursor,omitempty"`
	CacheManifestSHA256 string   `json:"cache_manifest_sha256,omitempty"`
	ScriptResultFormat  string   `json:"script_result_format,omitempty"`
	ScriptMediaID       string   `json:"script_media_id,omitempty"`
	MachineIDs          []string `json:"machine_ids"`
	Operation           string   `json:"operation"`
	TimeoutMS           int      `json:"timeout_ms"`
	WaitMS              int      `json:"wait_ms"`
}

func (s *Service) sshTargetTx(ctx context.Context, tx *sql.Tx, project, machine string) (Target, error) {
	if s.SSHRunner == nil {
		return Target{}, failure(503, "SSH_DISABLED", "SSH runner is not configured")
	}
	b, e := s.SSHRunner.Binding(project, machine)
	if e != nil {
		return Target{}, failure(409, "SSH_TARGET_UNAVAILABLE", "No approved SSH binding for this target")
	}
	t := Target{ProjectID: project, ResourceKey: machine, Mode: "ssh", SSH: &b}
	var trust, pin, state, source string
	e = tx.QueryRowContext(ctx, `SELECT a.hostname,a.version,COALESCE(a.model_version_id,''),a.created_at,a.state,m.ssh_trust,m.host_key_fingerprint,m.ssh_trust_source FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1 AND m.id=$2 FOR SHARE OF a,m`, project, machine).Scan(&t.Name, &t.Revision, &t.ModelVersion, &t.CreatedAt, &state, &trust, &pin, &source)
	if errors.Is(e, sql.ErrNoRows) {
		return t, failure(404, "NOT_FOUND", "Project machine not found")
	}
	if e != nil {
		return t, e
	}
	if state != "provisioned" || trust != "established" || source != "ssh" || pin != b.HostKey {
		return t, failure(409, "SSH_TRUST_REQUIRED", "Provisioned target with established matching host key is required")
	}
	if e = platform.EnforceSSHDomainBinding(ctx, tx, s.SSHRunner, project, machine); e != nil {
		return t, e
	}
	return t, nil
}

func (s *Service) sshFrozenTx(ctx context.Context, tx *sql.Tx, t Target) error {
	if t.SSHTrustRequestID != "" {
		return s.sshTrustFrozenTx(ctx, tx, t)
	}
	current, e := s.sshTargetTx(ctx, tx, t.ProjectID, t.ResourceKey)
	if e != nil {
		return e
	}
	if t.SSH == nil || *current.SSH != *t.SSH || current.Revision != t.Revision || current.ModelVersion != t.ModelVersion {
		return conflict("SSH target binding or asset revision changed")
	}
	return nil
}

func (s *Service) CreateSSH(ctx context.Context, a *Actor, project string, in SSHCreateInput, key, trace string) (*Execution, error) {
	if in.ArtifactMediaID != "" || in.PreferScriptCache || in.ScriptMediaID != "" || in.ScriptResultFormat != "" || in.Operation != "host.identity" && in.Operation != "host.wait" && in.Operation != "host.cache.inventory" && in.Operation != "host.artifact.inventory" {
		return nil, invalid("Only allowlisted bounded read-only host queries are admitted by this endpoint")
	}
	return s.createSSH(ctx, a, project, in, "", 0, key, trace)
}

func (s *Service) CreateSSHTrustVerification(ctx context.Context, a *Actor, project, requestID string, version int64, key, trace string) (*Execution, error) {
	if !opaqueID.MatchString(requestID) || version < 1 {
		return nil, invalid("Valid request ID and expected_version are required")
	}
	return s.createSSH(ctx, a, project, SSHCreateInput{Operation: "trust.verify", TimeoutMS: 5000}, requestID, version, key, trace)
}

func (s *Service) createSSH(ctx context.Context, a *Actor, project string, in SSHCreateInput, requestID string, version int64, key, trace string) (_ *Execution, resultErr error) {
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 5000
	}
	if err := validateArtifactCreate(in, requestID); err != nil {
		return nil, err
	}
	check := taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: project, MachineID: "00000000000000000000000000000000", CommandID: "00000000000000000000000000000000", AttemptID: "00000000000000000000000000000000", FencingToken: 1, Operation: in.Operation, TimeoutMS: in.TimeoutMS, WaitMS: in.WaitMS, CacheCursor: in.CacheCursor, CacheManifestSHA256: in.CacheManifestSHA256}
	if (in.Operation != "script.run" && in.Operation != "media.stage" && (in.PreferScriptCache || check.Validate() != nil)) || (in.Operation == "script.run" && (in.CacheCursor != "" || in.CacheManifestSHA256 != "" || in.ScriptResultFormat != "" && in.ScriptResultFormat != "json" || in.TimeoutMS < 100 || in.TimeoutMS > 30000 || in.WaitMS != 0 || requestID != "" || !opaqueID.MatchString(in.ScriptMediaID))) || (requestID == "" && len(in.MachineIDs) == 0) || len(in.MachineIDs) > 32 || in.CacheCursor != "" && len(in.MachineIDs) != 1 {
		return nil, invalid("1..32 machine IDs and a bounded host.identity or host.wait operation are required")
	}
	if e := s.cacheInventoryAdmission(in.Operation); e != nil {
		return nil, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer func() {
		tx.Rollback()
		if resultErr != nil && s.MediaStore != nil && a != nil {
			resultErr = s.MediaStore.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace)
		}
	}()
	roles := []string{"Admin", "DE", "DL", "SRE"}
	if requestID != "" {
		roles = []string{"Admin", "DL", "SRE"}
	}
	if e = authorize(ctx, tx, a, project, roles...); e != nil {
		return nil, e
	}
	ids := append([]string{}, in.MachineIDs...)
	sort.Strings(ids)
	in.MachineIDs = ids
	scope := "ssh.execute:" + project
	if requestID != "" {
		scope = "ssh.trust.execute:" + requestID
	}
	payload := struct {
		Input     SSHCreateInput `json:"input"`
		RequestID string         `json:"request_id"`
		Version   int64          `json:"expected_version"`
	}{in, requestID, version}
	idem, prior, e := idempotency(ctx, tx, a, scope, key, payload)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	var artifact *platform.TaskArtifact
	if in.Operation == "media.stage" {
		if s.MediaStore == nil {
			return nil, failure(409, "ARTIFACT_TRANSFERS_DISABLED", "Artifact media service unavailable")
		}
		artifact, e = s.MediaStore.ResolveTaskArtifactTx(ctx, tx, project, in.ArtifactMediaID, nil)
		if e != nil {
			return nil, e
		}
	}
	var script *platform.TaskScriptArtifact
	if in.Operation == "script.run" {
		if s.MediaStore == nil {
			return nil, failure(409, "SSH_SCRIPTS_DISABLED", "Approved script media service is not configured")
		}
		script, _, e = s.MediaStore.ResolveTaskScriptTx(ctx, tx, project, in.ScriptMediaID, nil, true)
		if e != nil {
			return nil, e
		}
	}
	if requestID != "" {
		var machine string
		var currentVersion int64
		e = tx.QueryRowContext(ctx, "SELECT machine_id,state_version FROM ssh_trust_requests WHERE id=$1 AND project_id=$2 AND state='approved' FOR UPDATE", requestID, project).Scan(&machine, &currentVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, conflict("Approved SSH trust request required")
		}
		if e != nil {
			return nil, e
		}
		if currentVersion != version {
			return nil, conflict("SSH trust request version changed")
		}
		if e = advisory(ctx, tx, "ssh.trust.consume:"+project+":"+machine); e != nil {
			return nil, e
		}
		var consumed bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ssh_trust_requests used JOIN ssh_trust_requests current ON current.id=$1 WHERE used.project_id=current.project_id AND used.machine_id=current.machine_id AND used.credential_ref=current.credential_ref AND used.credential_version=current.credential_version AND used.execution_id IS NOT NULL)`, requestID).Scan(&consumed)
		if e != nil {
			return nil, e
		}
		if consumed {
			return nil, conflict("SSH credential version has already been consumed for this target")
		}

		ids = []string{machine}
	}
	targets := []Target{}
	targetIDs := []string{}
	for i, machine := range ids {
		if !opaqueID.MatchString(machine) {
			return nil, invalid("Invalid machine ID")
		}
		if i > 0 && machine == ids[i-1] {
			return nil, invalid("Duplicate machine ID")
		}
		var t Target
		if requestID != "" {
			t, e = s.sshTrustTargetTx(ctx, tx, project, requestID, "approved")
		} else {
			t, e = s.sshTargetTx(ctx, tx, project, machine)
		}
		if e != nil {
			return nil, e
		}
		e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, machine).Scan(&t.ID)
		if e != nil {
			return nil, e
		}
		targets = append(targets, t)
		targetIDs = append(targetIDs, t.ID)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if requestID != "" && targets[0].SSHInstallKey != nil {
		in.Operation = "trust.install"
	}
	task := Task{Artifact: artifact, ScriptPreferCache: in.PreferScriptCache, ScriptResultFormat: in.ScriptResultFormat, Script: script, ID: "ssh-query", Type: in.Operation, Idempotent: true, Retry: 0, TimeoutMS: in.TimeoutMS, Resources: []string{"device", "ssh"}}
	if artifact != nil {
		task.ID = "ssh-artifact"
		task.Idempotent = false
	}
	if script != nil {
		task.ID = "ssh-script"
		task.Idempotent = false
	}
	def := Definition{APIVersion: "keel.ssh/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "SSH " + in.Operation, Version: 1, Definition: def, ContentHash: hash(jsonBytes(def)), CreatedAt: now}
	flow.Name += " " + flow.FlowID[:8]
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flow.FlowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, flow.ID, flow.FlowID, project, jsonBytes(def), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	params := map[string]any{"ssh_wait_ms": in.WaitMS}
	if in.CacheCursor != "" {
		params["cache_cursor"] = in.CacheCursor
	}
	if in.CacheManifestSHA256 != "" {
		params["cache_manifest_sha256"] = in.CacheManifestSHA256
	}
	x := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "ssh", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now, TargetSetHash: hash(jsonBytes(targetIDs))}
	x.Snapshot = Snapshot{Mode: "ssh", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: x.TargetSetHash, Admission: "Read-only allowlisted operation, provisioned project asset, exact operator binding and established pinned host key; not hardware acceptance", FrozenAt: now}
	if artifact != nil {
		x.Snapshot.Admission = "Independently approved immutable media; exclusive target lease and original bounded authorization; resumable owned fixed-cache checkpoints; success requires full SHA256/size verification; no installation or execution"
	}
	if script != nil {
		x.Snapshot.Admission = "Independently approved immutable Linux script artifact; fixed non-root interpreter; bounded execution and complete process-tree stop proof required; no automatic replay or rollback claim"
	}
	if requestID != "" {
		x.Snapshot.Admission = "Independently approved, time-limited pinned first-connection verification of an already installed platform key/helper; no key installation, password change or arbitrary repair"
		if targets[0].SSHInstallKey != nil {
			x.Snapshot.Admission = "Independently approved public key append through a configured temporary non-root SSH channel; fresh SSH handshake using the frozen project-domain key must query the same durable command before trust; customer keys preserved"
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'ssh',$4,$5,$6,1,100,$7,$8)`, x.ID, project, flow.ID, jsonBytes(params), jsonBytes(x.Snapshot), x.TargetSetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	if requestID != "" {
		if _, e = tx.ExecContext(ctx, "UPDATE ssh_trust_requests SET state='consumed',state_version=state_version+1,execution_id=$2 WHERE id=$1", requestID, x.ID); e != nil {
			return nil, e
		}
	}
	for _, t := range targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, x.ID, t.ID, jsonBytes(t)); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,$4,0,1,$5,'ssh',$6)`, platform.RandomID(), x.ID, t.ID, task.ID, platform.RandomID(), now); e != nil {
			return nil, e
		}
	}
	if e = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "ssh", "target_count": len(targets), "operation": in.Operation}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "execution.ssh.create", x.ID, trace, map[string]any{"operation": in.Operation, "target_count": len(targets), "target_hash": x.TargetSetHash}); e != nil {
		return nil, e
	}
	if artifact != nil {
		if e = audit(ctx, tx, a.User.ID, project, "media.distribute", artifact.MediaID, trace, map[string]any{"execution_id": x.ID, "transport": x.Mode, "sha256": artifact.SHA256, "size": artifact.Size, "object_version": artifact.ObjectVersion, "target_count": len(targets), "target_hash": x.TargetSetHash}); e != nil {
			return nil, e
		}
	}
	if e = remember(ctx, tx, idem, payload, x); e != nil {
		return nil, e
	}
	return x, tx.Commit()
}

func sshCommand(c *Claim) taskrunner.TaskCommand {
	wait := 0
	parameter := "ssh_wait_ms"
	if c.Target.Mode == "agent" {
		parameter = "agent_wait_ms"
	}
	switch v := c.Parameters[parameter].(type) {
	case float64:
		wait = int(v)
	case int:
		wait = v
	}
	command := taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: c.Target.ProjectID, MachineID: c.Target.ResourceKey, CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Operation: c.Task.Type, TimeoutMS: c.Task.TimeoutMS, WaitMS: wait}
	command.CacheCursor, _ = c.Parameters["cache_cursor"].(string)
	command.CacheManifestSHA256, _ = c.Parameters["cache_manifest_sha256"].(string)
	if c.Target.SSHInstallKey != nil {
		command.PublicKey = c.Target.SSHInstallKey.PublicKey
		if c.Target.SSHTrustExpiresAt != nil {
			command.AuthorizationExpiresAt = c.Target.SSHTrustExpiresAt.UnixMicro()
		}
	}
	if c.Task.Artifact != nil {
		command = c.Task.Artifact.Command(command)
		command.AuthorizationExpiresAt = 1
	}
	if c.Task.Script != nil {
		command = c.Task.Script.Command(command)
		command.ScriptResultFormat = c.Task.ScriptResultFormat
		command.ScriptCacheAllowed = c.Task.ScriptPreferCache
	}
	return command
}

func (s *Service) verifySSHInstallation(ctx context.Context, c *Claim, o taskrunner.Observation) (taskrunner.Observation, error) {
	if c.Target.SSHInstallKey == nil || o.State != "success" {
		return o, nil
	}
	key := *c.Target.SSHInstallKey
	if c.Target.SSHTrustExpiresAt == nil || !time.Now().Before(*c.Target.SSHTrustExpiresAt) {
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	public, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if parseErr != nil || o.Output["public_key_fingerprint"] != ssh.FingerprintSHA256(public) {
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	provider, ok := s.SSHRunner.(interface {
		QueryInstalledCredential(context.Context, taskrunner.Request, taskrunner.InstallCredential) (taskrunner.Observation, error)
	})
	if !ok || o.Output["key_installed"] != true {
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	verified, err := provider.QueryInstalledCredential(ctx, taskrunner.Request{Action: "query", Command: sshCommand(c)}, key)
	if err != nil || verified.State != "success" || hash(jsonBytes(verified)) != hash(jsonBytes(o)) || !time.Now().Before(*c.Target.SSHTrustExpiresAt) {
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	verified.Output["public_key_verified"] = true
	verified.Output["verified_credential_id"] = key.ID
	return verified, nil
}

func (s *Service) recordSSH(ctx context.Context, o taskrunner.Observation) error {
	if !o.Terminal() {
		return taskrunner.ErrUnknown
	}
	return s.recordHost(ctx, "ssh", o)
}

func (s *Service) recordHost(ctx context.Context, mode string, o taskrunner.Observation) error {
	if !o.Terminal() {
		return taskrunner.ErrUnknown
	}
	output := map[string]any{"machine_id": o.Command.MachineID, "mode": mode, "source": "host", "transport": mode, "hardware_evidence": false, "operation": o.Command.Operation, "observation": o.Output}
	return s.RecordReceipt(ctx, Receipt{CommandID: o.Command.CommandID, AttemptID: o.Command.AttemptID, FencingToken: o.Command.FencingToken, Result: o.State, StartedAt: o.StartedAt, FinishedAt: o.FinishedAt, Output: output})
}

func (s *Service) runSSHClaim(ctx context.Context, c *Claim) error {
	return s.runHostClaim(ctx, c, "ssh")
}

// Both host transports share lease renewal, cancellation, one query-only recovery
// and immutable execution receipts; transport choice cannot bypass the kernel.
func (s *Service) runHostClaim(ctx context.Context, c *Claim, mode string) error {
	runner := s.SSHRunner
	if mode == "agent" {
		runner = s.AgentRunner
	}
	if runner == nil {
		return taskrunner.ErrRejected
	}
	// Recheck current submitter, membership, project, binding and host trust at
	// the last control-plane boundary before opening a transport session.
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	command := sshCommand(c)
	var scriptBody []byte
	var creator string
	e = tx.QueryRowContext(ctx, `SELECT created_by FROM exec_executions WHERE id=$1`, c.Attempt.ExecutionID).Scan(&creator)
	if e == nil {
		e = s.cacheInventoryAdmission(c.Task.Type)
	}
	if e == nil {
		if mode == "agent" {
			e = s.agentFrozenTx(ctx, tx, c.Target)
		} else {
			e = s.sshFrozenTx(ctx, tx, c.Target)
		}
	}
	if e == nil && c.Task.Artifact != nil {
		e = s.artifactFrozenTx(ctx, tx, c.Target.ProjectID, c.Task)
	}
	if e == nil && c.Task.Script != nil {
		if e = s.scriptFrozenTx(ctx, tx, c.Target.ProjectID, mode, c.Task); e != nil {
			// Revocation/approval changes stop admission before body delivery.
		} else if s.MediaStore == nil {
			e = taskrunner.ErrRejected
		} else {
			_, scriptBody, e = s.MediaStore.ResolveTaskScriptTx(ctx, tx, c.Target.ProjectID, c.Task.Script.MediaID, c.Task.Script, true)
		}
	}
	if e == nil {
		var ok bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id WHERE a.id=$1 AND a.fencing_token=$2 AND a.lease_owner=$3 AND a.state='running' AND a.lease_until>now() AND x.desired_state='running' AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DE','DL','SRE'))))`, c.Attempt.ID, c.Attempt.FencingToken, c.Attempt.LeaseOwner).Scan(&ok)
		if e == nil && !ok {
			e = taskrunner.ErrRejected
		}
	}
	if e == nil && (c.Task.Script != nil || c.Task.Artifact != nil) {
		kind := "script.dispatch_committed"
		if c.Task.Artifact != nil {
			kind = "artifact.dispatch_committed"
		}
		command, e = s.commitBoundedDispatchTx(ctx, tx, c, kind)
		if e == nil {
			e = tx.Commit()
		}
	}
	tx.Rollback()
	if e != nil {
		if s.MediaStore != nil {
			e = s.MediaStore.RecordMediaIntegrityFailure(ctx, e, creator, "")
		}
		return e
	} // No remote work, no fabricated stop proof.
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.Target.SSHInstallKey != nil && c.Target.SSHTrustExpiresAt != nil {
		var stop context.CancelFunc
		work, stop = context.WithDeadline(work, *c.Target.SSHTrustExpiresAt)
		defer stop()
	}
	if c.Task.Artifact != nil {
		var deadlineCancel context.CancelFunc
		work, deadlineCancel = context.WithDeadline(work, time.UnixMicro(command.AuthorizationExpiresAt))
		defer deadlineCancel()
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(c.LeaseDuration / 3)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				beat, done := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				kill, e := s.Heartbeat(beat, c)
				done()
				if e != nil || kill {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	run := taskrunner.Request{Action: "run", Command: command, Script: scriptBody}
	if command.ScriptCacheAllowed {
		// The dispatch is already consumed. Probe is metadata only; there is
		// still exactly one run, with no body fallback after that run is sent.
		probe, done := context.WithTimeout(work, time.Second)
		cached, err := runner.Do(probe, taskrunner.Request{Action: "cache", Command: command})
		done()
		if hit, valid := taskrunner.ScriptCacheProbeHit(cached, command); err == nil && valid == nil && hit {
			run.Script = nil
			run.UseCachedScript = true
		}
	}
	if work.Err() != nil || command.Operation == "script.run" && !time.UnixMicro(command.AuthorizationExpiresAt).After(time.Now()) {
		return taskrunner.ErrUnknown
	}
	var o taskrunner.Observation
	if c.Task.Artifact != nil {
		o, e = s.streamArtifact(work, runner, c, command, creator)
	} else {
		o, e = runner.Do(work, run)
	}

	if e != nil || !o.Terminal() {
		// One bounded status/cancel query, never a second run. Terminal proof can
		// arrive after the worker's cancellation but before lease reconciliation.
		query, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		action := "query"
		if work.Err() != nil || c.Task.Artifact != nil {
			action = "cancel"
		}
		o, e = runner.Do(query, taskrunner.Request{Action: action, Command: command})
		for e == nil && !o.Terminal() && action == "cancel" {
			select {
			case <-query.Done():
				return taskrunner.ErrUnknown
			case <-time.After(50 * time.Millisecond):
			}
			o, e = runner.Do(query, taskrunner.Request{Action: "query", Command: command})
		}
	}
	if e != nil || !o.Terminal() {
		return taskrunner.ErrUnknown
	}
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if o.Validate(command) != nil {
		return taskrunner.ErrRejected
	}
	if mode == "ssh" {
		o, e = s.verifySSHInstallation(finish, c, o)
		if e != nil {
			return e
		}
	}
	if e = s.recordHost(finish, mode, o); e != nil {
		return e
	}
	return s.Complete(finish, c)
}

// FetchSSHReceipt retrieves actual terminal proof for an unknown attempt. It
// never submits work and uses the same authorized reconciliation transaction.
func (s *Service) FetchSSHReceipt(ctx context.Context, a *Actor, project, id, attemptID string, version int64, key, trace string) (*Execution, error) {
	if version < 1 {
		return nil, invalid("expected_version must be positive")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); e != nil {
		return nil, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE project_id=$1 AND id=$2", project, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if x.Mode != "ssh" {
		return nil, conflict("SSH receipt recovery requires an SSH execution")
	}
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND id=$2", id, attemptID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	// Reuse durable proof before contacting the target, including retries of a
	// successful recovery. Reconcile owns the idempotency key and payload check.
	var recorded []byte
	e = tx.QueryRowContext(ctx, "SELECT payload FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3", attempt.CommandID, attempt.ID, attempt.FencingToken).Scan(&recorded)
	if e == nil {
		r, err := decode[Receipt](recorded)
		if err != nil {
			return nil, err
		}
		tx.Rollback()
		return s.Reconcile(ctx, a, project, id, attemptID, ReconcileInput{ExpectedVersion: version, Result: r.Result, Evidence: "Verified pinned SSH wrapper terminal receipt for command " + attempt.CommandID}, key, trace)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if attempt.State != "unknown" || attempt.StateVersion != version {
		return nil, conflict("Attempt is not unknown or its version changed")
	}
	var target Target
	for _, t := range x.Snapshot.Targets {
		if t.ID == attempt.TargetID {
			target = t
		}
	}
	if e = s.sshFrozenTx(ctx, tx, target); e != nil {
		return nil, e
	}
	task, e := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
	if e != nil {
		return nil, e
	}
	c := &Claim{Attempt: attempt, Target: target, Task: task, Parameters: x.Parameters}
	command := sshCommand(c)
	if task.Artifact != nil {
		command, e = originalDispatchCommandTx(ctx, tx, c, "artifact.dispatch_committed")
		if e != nil {
			return nil, e
		}
	}
	if task.Script != nil {
		command, e = originalScriptCommandTx(ctx, tx, c)
		if e != nil {
			return nil, failure(409, "SSH_RECEIPT_PENDING", "Original script dispatch evidence is required; resource remains quarantined")
		}
	}
	tx.Rollback()
	query, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	o, e := s.SSHRunner.Do(query, taskrunner.Request{Action: "query", Command: command})
	if e != nil || !o.Terminal() {
		return nil, failure(409, "SSH_RECEIPT_PENDING", "No terminal SSH receipt; resource remains quarantined")
	}
	o, e = s.verifySSHInstallation(query, c, o)
	if e != nil {
		return nil, failure(409, "SSH_INSTALL_VERIFICATION_PENDING", "New key has no verified fresh SSH receipt; resources remain quarantined")
	}
	if e = s.recordSSH(ctx, o); e != nil {
		return nil, e
	}
	return s.Reconcile(ctx, a, project, id, attemptID, ReconcileInput{ExpectedVersion: version, Result: o.State, Evidence: "Verified pinned SSH wrapper terminal receipt for command " + attempt.CommandID}, key, trace)
}

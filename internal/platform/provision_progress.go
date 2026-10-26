package platform

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Progress is a projection of durable facts, never a source of write authority.
// Missing earlier evidence stays missing even when a later stage is verified.
type ProvisionProgressEvidence struct {
	ID   int64     `json:"id"`
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
}
type ProvisionProgressStage struct {
	ID       string                      `json:"id"`
	Status   string                      `json:"status"`
	Reason   string                      `json:"reason"`
	Evidence []ProvisionProgressEvidence `json:"evidence"`
}
type ProvisionProgressResource struct {
	MachineReserved        bool       `json:"machine_reserved"`
	LeaseState             string     `json:"lease_state"`
	LeaseExpiresAt         *time.Time `json:"lease_expires_at,omitempty"`
	LeaseExpired           bool       `json:"lease_expired"`
	ReconciliationRequired bool       `json:"reconciliation_required"`
}
type ProvisionInstallationProgress struct {
	InstallationID string                    `json:"installation_id"`
	State          string                    `json:"state"`
	StateVersion   int64                     `json:"state_version"`
	ObservedAt     time.Time                 `json:"observed_at"`
	Stages         []ProvisionProgressStage  `json:"stages"`
	Resource       ProvisionProgressResource `json:"resource"`
	SSHRepair      *ProvisionSSHRepair       `json:"ssh_repair,omitempty"`
	FailureReason  *string                   `json:"failure_reason,omitempty"`
}
type provisionProgressFact struct {
	ProvisionProgressEvidence
	Outcome  string
	Resource string
}

var provisionProgressKinds = []string{
	"boot.dhcp_observed", "boot.enrolled", "boot.file_served", "preflight.reported", "storage.selected", "storage.approved",
	"write.permit_issued", "write.receipt_reported", "write.result_unknown", "installation.stopped_before_write",
	"first_boot.identity_reported", "first_boot.hostname_mismatch", "first_boot.ssh_started", "first_boot.ssh_verified", "first_boot.ssh_repair_required",
}

func projectProvisionProgress(x *ProvisionInstallation, facts []provisionProgressFact, resource ProvisionProgressResource, now time.Time) *ProvisionInstallationProgress {
	// A registered domain projection may precede native execution settlement.
	// Only a released actual lease removes that reservation; state names alone
	// cannot erase an exclusive claim left by a crashed worker.
	if resource.LeaseState == "held" || resource.LeaseState == "reconciling" {
		resource.MachineReserved = true
	}
	out := &ProvisionInstallationProgress{InstallationID: x.ID, State: x.State, StateVersion: x.StateVersion, ObservedAt: now, Resource: resource, FailureReason: x.FailureReason}
	for _, id := range []string{"dhcp", "ipxe", "distribution", "installation", "first_boot", "registration", "ssh"} {
		out.Stages = append(out.Stages, ProvisionProgressStage{ID: id, Status: "pending", Reason: "evidence_missing", Evidence: []ProvisionProgressEvidence{}})
	}
	apply := func(index int, f provisionProgressFact, status, reason string) {
		s := &out.Stages[index]
		s.Status = status
		s.Reason = reason
		s.Evidence = append(s.Evidence, f.ProvisionProgressEvidence)
	}
	// Events are ordered by durable database sequence, not client timestamps.
	for _, f := range facts {
		switch f.Kind {
		case "boot.dhcp_observed":
			apply(0, f, "observed", "dhcp_auxiliary_only")
		case "boot.file_served":
			switch f.Resource {
			case "boot.ipxe", "ipxe.efi", "undionly.kpxe":
				apply(1, f, "observed", "boot_file_delivered")
			case "rootfs":
				apply(2, f, "reported", "rootfs_delivery_reported")
			case "kernel", "initrd", "manifest.json", "preflight.json", "user-data", "meta-data", "preflight", "ssh-helper":
				if out.Stages[2].Status == "reported" {
					out.Stages[2].Evidence = append(out.Stages[2].Evidence, f.ProvisionProgressEvidence)
				} else {
					apply(2, f, "observed", "server_delivery_recorded")
				}
			}
		case "boot.enrolled":
			apply(1, f, "observed", "authenticated_environment_enrolled")
		case "preflight.reported":
			if f.Outcome == "PASS" {
				apply(3, f, "observed", "preflight_reported")
			} else {
				apply(3, f, "failed", "preflight_failed")
			}
		case "storage.selected":
			apply(3, f, "observed", "storage_review_required")
		case "storage.approved":
			apply(3, f, "observed", "storage_approved")
		case "write.permit_issued":
			apply(3, f, "observed", "write_permit_issued")
		case "write.receipt_reported":
			if f.Outcome == "written" {
				apply(3, f, "reported", "write_readback_reported")
			} else {
				apply(3, f, "unknown", "write_outcome_unknown")
			}
		case "write.result_unknown":
			apply(3, f, "unknown", "write_outcome_unknown")
		case "installation.stopped_before_write":
			apply(3, f, "failed", "stopped_before_write")
		case "first_boot.identity_reported":
			apply(4, f, "reported", "new_boot_identity_reported")
			apply(5, f, "verified", "signed_registration_accepted")
		case "first_boot.hostname_mismatch":
			apply(5, f, "failed", "hostname_mismatch")
		case "first_boot.ssh_started":
			apply(6, f, "observed", "ssh_verification_started")
		case "first_boot.ssh_verified":
			apply(6, f, "verified", "ssh_transport_verified")
		case "first_boot.ssh_repair_required":
			apply(6, f, "unknown", "ssh_repair_required")
		}
	}
	// Unknown retains the machine reservation even if a timed lease has expired.
	// Actual lease state is read separately; no state-name inference claims it held.
	if x.State == "unknown" || resource.LeaseState == "reconciling" || out.Stages[6].Status == "unknown" || ((out.Stages[6].Status == "observed" || x.State == "registered") && resource.LeaseState == "held" && resource.LeaseExpired) {
		out.Resource.ReconciliationRequired = true
	}
	return out
}

func (s *Store) ReadProvisionInstallationProgress(ctx context.Context, a *AuthSession, project, id string) (*ProvisionInstallationProgress, error) {
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	columns := strings.Replace(provisionInstallationColumns, "latest_report,", "NULL::bytea AS latest_report,", 1)
	x, err := scanProvisionInstallation(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM provision_installations WHERE project_id=$1 AND id=$2", project, id))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	resource := ProvisionProgressResource{MachineReserved: x.State != "registered" && x.State != "failed" && x.State != "canceled", LeaseState: "none"}
	if x.AttemptID != nil {
		var expires time.Time
		err = tx.QueryRowContext(ctx, `SELECT state,expires_at FROM exec_resource_leases WHERE resource_id=$1 AND attempt_id=$2`, "device:machine:"+x.MachineID, *x.AttemptID).Scan(&resource.LeaseState, &expires)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			resource.LeaseExpiresAt = &expires
			resource.LeaseExpired = !expires.After(now)
		}
	}
	// At most one latest fact per allowlisted type. Never return raw payloads:
	// these can contain credentials, manifests or signed identity reports.
	rows, err := tx.QueryContext(ctx, `SELECT id,event_type,created_at,outcome,resource FROM (
 SELECT DISTINCT ON(event_type,CASE WHEN event_type='boot.file_served' THEN payload->>'resource' ELSE '' END) id,event_type,created_at,CASE WHEN event_type='boot.file_served' AND payload->>'full_response'='true' AND payload->>'sha256' ~ '^[a-f0-9]{64}$' AND payload->>'bytes' ~ '^[1-9][0-9]*$' THEN payload->>'resource' ELSE '' END AS resource,COALESCE(payload#>>'{receipt,outcome}',payload->>'status','') AS outcome
 FROM provision_installation_events WHERE installation_id=$1 AND event_type=ANY($2::text[]) ORDER BY event_type,CASE WHEN event_type='boot.file_served' THEN payload->>'resource' ELSE '' END,id DESC
 ) facts ORDER BY id`, id, provisionProgressKinds)
	if err != nil {
		return nil, err
	}
	facts := []provisionProgressFact{}
	for rows.Next() {
		var f provisionProgressFact
		if err = rows.Scan(&f.ID, &f.Kind, &f.At, &f.Outcome, &f.Resource); err != nil {
			rows.Close()
			return nil, err
		}
		facts = append(facts, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := projectProvisionProgress(x, facts, resource, now)
	out.SSHRepair, err = readProvisionSSHRepairTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

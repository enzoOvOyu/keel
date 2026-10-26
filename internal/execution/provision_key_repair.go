package execution

import (
	"context"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func (s *Service) runProvisionKeyRepair(ctx context.Context, c *Claim) (bool, error) {
	var active bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_key_repairs WHERE installation_id=$1 AND state IN('pending','approved','consumed','unknown'))`, c.Target.ProvisionInstallationID).Scan(&active); err != nil {
		return false, err
	}
	if !active {
		return false, nil
	}
	work, err := s.ProvisionStore.PrepareProvisionKeyRepair(ctx, c.Target.ProjectID, c.Target.ProvisionInstallationID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	if err != nil {
		return true, err
	}
	if work == nil {
		return true, nil
	}
	runner, ok := s.SSHRunner.(interface {
		RepairProvisionKey(context.Context, taskrunner.TaskCommand, taskrunner.ProvisionSSHPlan, taskrunner.ProvisionBootstrap, string) (taskrunner.ProvisionKeyRepairProof, error)
	})
	var proof *taskrunner.ProvisionKeyRepairProof
	if ok {
		result, e := runner.RepairProvisionKey(ctx, work.Command, work.Plan, work.Bootstrap, work.Mode)
		if e == nil {
			proof = &result
		}
	}
	finish, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// An absent proof records uncertainty only; it never releases the device or
	// schedules another run. Orphan consumed intents are explicit-query-only.
	return true, s.ProvisionStore.FinishProvisionKeyRepair(finish, c.Target.ProjectID, c.Target.ProvisionInstallationID, *work, proof)
}

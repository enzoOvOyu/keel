package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// The public identity is stored by actual new-key proof, never inferred from a
// configured credential or a legacy/sim trust flag. Once installed, short-lived
// bootstrap access cannot become an ordinary SSH execution credential.
func EnforceSSHDomainBinding(ctx context.Context, tx *sql.Tx, runner taskrunner.Runner, project, machine string) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT m.ssh_domain_credential FROM machines m JOIN project_assets a ON a.machine_id=m.id WHERE a.project_id=$1 AND m.id=$2 FOR SHARE OF m`, project, machine).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var expected *taskrunner.InstallCredential
	if err := json.Unmarshal(raw, &expected); err != nil {
		return err
	}
	if expected == nil {
		return nil
	}
	key, err := ReadSSHInstallCredential(ctx, tx, project, *expected, time.Now())
	if err != nil {
		return err
	}
	checker, ok := runner.(interface {
		CheckActiveCredential(context.Context, string, string, taskrunner.InstallCredential) error
	})
	if !ok || checker.CheckActiveCredential(ctx, project, machine, *key) != nil {
		return apiError(409, "SSH_DOMAIN_BINDING_REQUIRED", "Configure the proven project-domain vault credential before ordinary SSH operations")
	}
	return nil
}

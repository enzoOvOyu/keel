package platform

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// ExecutionCircuitBreakerPolicy is an admission-time catalog fact, not a live
// pointer. Changing or retiring a key cannot rewrite an accepted execution.
type ExecutionCircuitBreakerPolicy struct {
	DefaultPercent int        `json:"default_percent"`
	Source         string     `json:"source"`
	ParamKeyID     string     `json:"param_key_id,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

func ResolveExecutionCircuitBreakerTx(ctx context.Context, tx *sql.Tx, project string) (*ExecutionCircuitBreakerPolicy, error) {
	out := &ExecutionCircuitBreakerPolicy{DefaultPercent: 5, Source: "built_in"}
	var value, state, scope string
	var updated time.Time
	err := tx.QueryRowContext(ctx, `SELECT id,default_value,state,scope_key,updated_at FROM param_keys
 WHERE key='exec.circuit_breaker_threshold' AND scope_key IN('global',$1)
 ORDER BY (scope_key=$1) DESC LIMIT 1 FOR SHARE`, "project:"+project).
		Scan(&out.ParamKeyID, &value, &state, &scope, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if state != "active" {
		return nil, apiError(409, "PARAM_KEY_RETIRED", "Execution circuit breaker policy is retired")
	}
	percent, err := strconv.Atoi(value)
	if err != nil || percent < 1 || percent > 100 {
		return nil, apiError(409, "EXECUTION_POLICY_INVALID", "Execution circuit breaker default must be an integer percentage from 1 to 100")
	}
	out.DefaultPercent, out.UpdatedAt = percent, &updated
	out.Source = "global"
	if scope == "project:"+project {
		out.Source = "project"
	}
	return out, nil
}

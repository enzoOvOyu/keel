// Package provisionwriter executes one explicitly approved, fenced installation
// without retries. The Linux adapter is restricted to an accepted ephemeral
// initramfs; the state machine can be verified with in-memory software doubles.
package provisionwriter

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

var ErrDenied = errors.New("controlled installation writer unavailable")

var stages = []string{"gpt.write", "efi.format", "root.format", "root.extract", "boot.install", "seed.install", "final.sync", "final.verify"}

// Authority must authenticate each response, including its fresh request nonce,
// over the fixed machine TLS session. It must not cache an older authorization.
type Authority interface {
	CheckWrite(context.Context, string) (*provisionboot.WriteAuthorization, error)
}

type Backend interface {
	VerifyBeforeWrite(context.Context, provisionboot.WritePreparation) error
	Perform(context.Context, string) (*provisionboot.WriteReadback, error)
	// Quarantine preserves local resources after an uncertain operation. A
	// caller must not close the device/mounts merely because Run returned.
	Quarantine()
}

type Result struct {
	Outcome  string                       `json:"outcome"`
	Stage    string                       `json:"stage"`
	Receipt  *provisionboot.WriteReceipt  `json:"receipt,omitempty"`
	Readback *provisionboot.WriteReadback `json:"readback,omitempty"`
}

func samePreparation(a, b provisionboot.WritePreparation) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func permitMatches(p *provisionboot.WritePermit, request provisionboot.WritePreparation, now time.Time) bool {
	if p == nil {
		return false
	}
	proof := provisionboot.BootProof{InstallationID: p.InstallationID, BootID: p.BootID, Sequence: p.Sequence}
	return provisionboot.ValidateWritePermit(p, proof, request, now) == nil
}

// Run never issues a permit. Once one exists, every error is conservatively
// unknown, including a stop before the first write: only separate authenticated
// reconciliation can release the controller's resource after permit issuance.
func Run(ctx context.Context, request provisionboot.WritePreparation, permit *provisionboot.WritePermit, authority Authority, backend Backend) (out Result, resultErr error) {
	out.Outcome = "unknown"
	if permit == nil || authority == nil || backend == nil {
		return out, ErrDenied
	}
	// Freeze caller-owned fields before asynchronous operations.
	p := *permit
	out.Receipt = &provisionboot.WriteReceipt{PermitID: p.ID, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, Outcome: "unknown", Reason: "writer did not complete verified installation"}
	defer func() {
		if out.Outcome != "written" {
			backend.Quarantine()
		}
	}()
	if ctx.Err() != nil || !permitMatches(&p, request, time.Now()) || backend.VerifyBeforeWrite(ctx, request) != nil {
		return out, ErrDenied
	}
	if bound, ok := backend.(interface {
		bindRun(provisionboot.WritePreparation, *provisionboot.WritePermit) error
	}); ok {
		if bound.bindRun(request, &p) != nil {
			return out, ErrDenied
		}
	}
	for _, stage := range stages {
		out.Stage = stage
		readback, err := runStage(ctx, &p, authority, backend, stage)
		if err != nil {
			return out, ErrDenied
		}
		if stage == "final.verify" {
			if readback == nil || readback.SHA256() == "" || readback.GPTSHA256 != p.GPTSHA256 || readback.RootFSSHA256 != p.RootFSSHA256 || readback.TargetUKISHA256 != p.TargetUKISHA256 {
				return out, ErrDenied
			}
			out.Readback = readback
		}
	}
	if ctx.Err() != nil || out.Readback == nil {
		return out, ErrDenied
	}
	out.Outcome = "written"
	out.Receipt.Outcome = "written"
	out.Receipt.ReadbackSHA256 = out.Readback.SHA256()
	out.Receipt.Reason = "fixed layout and critical installed content read back and synchronized"
	return out, nil
}

func currentAuthorization(ctx context.Context, permit *provisionboot.WritePermit, authority Authority, stage string) (*provisionboot.WriteAuthorization, error) {
	a, err := authority.CheckWrite(ctx, stage)
	if err != nil || ctx.Err() != nil || a == nil {
		return nil, ErrDenied
	}
	// The transport adapter verified the exact fresh nonce it generated. Here
	// independently check the complete permit/stage and short time bounds.
	query := provisionboot.WriteCheckQuery{PermitID: permit.ID, Nonce: a.Nonce, Stage: stage}
	if provisionboot.ValidateWriteAuthorization(a, permit, query, time.Now()) != nil {
		return nil, ErrDenied
	}
	copy := *a
	return &copy, nil
}

type stageResult struct {
	readback *provisionboot.WriteReadback
	err      error
}

func runStage(ctx context.Context, permit *provisionboot.WritePermit, authority Authority, backend Backend, stage string) (*provisionboot.WriteReadback, error) {
	if ctx.Err() != nil || !permit.ExpiresAt.After(time.Now()) {
		return nil, ErrDenied
	}
	firstCtx, firstCancel := context.WithDeadline(ctx, permit.ExpiresAt)
	initial, err := currentAuthorization(firstCtx, permit, authority, stage)
	firstCancel()
	if err != nil {
		return nil, err
	}
	opCtx, stop := context.WithCancel(ctx)
	defer stop()
	// This timer cancels an in-flight tool even if the next network check is
	// slow. A later authorization can never revive an expired operation.
	watchdog := time.AfterFunc(time.Until(initial.ValidUntil), stop)
	defer watchdog.Stop()
	done := make(chan stageResult, 1)
	go func() {
		r, err := backend.Perform(opCtx, stage)
		done <- stageResult{readback: r, err: err}
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	until := initial.ValidUntil
	failed := false
	for {
		select {
		case result := <-done:
			if failed || opCtx.Err() != nil || result.err != nil || !until.After(time.Now()) {
				return nil, ErrDenied
			}
			// The last check after operation completion avoids accepting stale
			// authority merely because the operation finished between ticks.
			checkCtx, cancel := context.WithDeadline(opCtx, until)
			_, err := currentAuthorization(checkCtx, permit, authority, stage)
			cancel()
			if err != nil {
				return nil, ErrDenied
			}
			return result.readback, nil
		case <-ticker.C:
			checkCtx, cancel := context.WithDeadline(opCtx, until)
			a, err := currentAuthorization(checkCtx, permit, authority, stage)
			cancel()
			if err != nil || opCtx.Err() != nil {
				failed = true
				stop()
				continue
			}
			until = a.ValidUntil
			watchdog.Reset(time.Until(until))
		case <-opCtx.Done():
			// A tool is killed by its context. A kernel write/fsync already
			// submitted cannot be recalled; wait for that call to return and
			// keep the outcome unknown rather than launch another operation.
			result := <-done
			_ = result
			return nil, ErrDenied
		}
	}
}

package provisionwriter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

func writerFixture() (provisionboot.WritePreparation, *provisionboot.WritePermit, provisionboot.WriteReadback) {
	now := time.Now().UTC()
	r := provisionboot.WritePreparation{Nonce: strings.Repeat("1", 32), StoragePlanSHA256: strings.Repeat("a", 64), GPTIdentity: provisionstorage.GPTIdentity{Disk: "01234567-89ab-4cde-8012-3456789abcde", EFI: "11234567-89ab-4cde-9012-3456789abcde", Root: "21234567-89ab-4cde-a012-3456789abcde"}, GPTSHA256: strings.Repeat("b", 64), RootFSSHA256: strings.Repeat("c", 64), TargetUKISHA256: strings.Repeat("d", 64), RuntimeInputSHA256: strings.Repeat("e", 64)}
	p := &provisionboot.WritePermit{Schema: provisionboot.WritePermitSchema, ID: strings.Repeat("2", 32), InstallationID: strings.Repeat("3", 32), BootID: strings.Repeat("4", 32), Sequence: 4, Nonce: r.Nonce, StoragePlanSHA256: r.StoragePlanSHA256, GPTSHA256: r.GPTSHA256, RootFSSHA256: r.RootFSSHA256, TargetUKISHA256: r.TargetUKISHA256, RuntimeInputSHA256: r.RuntimeInputSHA256, IssuedAt: now.Add(-time.Millisecond), ExpiresAt: now.Add(time.Minute)}
	readback := provisionboot.WriteReadback{GPTSHA256: r.GPTSHA256, RootFSSHA256: r.RootFSSHA256, TargetUKISHA256: r.TargetUKISHA256, SSHHelperSHA256: strings.Repeat("f", 64), UserDataSHA256: strings.Repeat("0", 64), MetaDataSHA256: strings.Repeat("1", 64)}
	return r, p, readback
}

type authorityFixture struct {
	mu         sync.Mutex
	permit     *provisionboot.WritePermit
	checkCount int
	failAfter  int
	validFor   time.Duration
	change     func(*provisionboot.WriteAuthorization)
}

func (a *authorityFixture) CheckWrite(ctx context.Context, stage string) (*provisionboot.WriteAuthorization, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkCount++
	if ctx.Err() != nil || (a.failAfter > 0 && a.checkCount > a.failAfter) {
		return nil, ErrDenied
	}
	valid := a.validFor
	if valid == 0 {
		valid = time.Second
	}
	now := time.Now().UTC()
	p := a.permit
	value := &provisionboot.WriteAuthorization{PermitID: p.ID, InstallationID: p.InstallationID, BootID: p.BootID, Sequence: p.Sequence, Nonce: fmt.Sprintf("%032x", a.checkCount), Stage: stage, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, CheckedAt: now, ValidUntil: now.Add(valid)}
	if a.change != nil {
		a.change(value)
	}
	return value, nil
}

type backendFixture struct {
	request     provisionboot.WritePreparation
	readback    provisionboot.WriteReadback
	consumed    bool
	quarantined bool
	called      []string
	failStage   string
	waitStage   string
}

func (b *backendFixture) VerifyBeforeWrite(ctx context.Context, r provisionboot.WritePreparation) error {
	if ctx.Err() != nil || b.consumed || !samePreparation(b.request, r) {
		return ErrDenied
	}
	b.consumed = true
	return nil
}
func (b *backendFixture) Perform(ctx context.Context, stage string) (*provisionboot.WriteReadback, error) {
	b.called = append(b.called, stage)
	if stage == b.waitStage {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if stage == b.failStage {
		return nil, ErrDenied
	}
	if stage == "final.verify" {
		copy := b.readback
		return &copy, nil
	}
	return nil, nil
}
func (b *backendFixture) Quarantine() { b.quarantined = true }

func TestWriterOrderedStagesAndVerifiedReceiptStopBeforeFirstBoot(t *testing.T) {
	r, p, readback := writerFixture()
	a := &authorityFixture{permit: p}
	b := &backendFixture{request: r, readback: readback}
	result, err := Run(context.Background(), r, p, a, b)
	if err != nil || result.Outcome != "written" || result.Stage != "final.verify" || result.Readback == nil || result.Receipt == nil || result.Receipt.ReadbackSHA256 != readback.SHA256() || b.quarantined {
		t.Fatalf("bad writer result: %#v %v", result, err)
	}
	if strings.Join(b.called, ",") != strings.Join(stages, ",") || a.checkCount != 2*len(stages) {
		t.Fatalf("stages/checks %v %d", b.called, a.checkCount)
	}
	if provisionboot.ValidateWriteReceipt(*result.Receipt, p) != nil {
		t.Fatal("writer receipt not protocol-compatible")
	}
	// A completed local writer is never replayed, even with identical inputs.
	count := len(b.called)
	if replay, err := Run(context.Background(), r, p, a, b); err == nil || replay.Outcome != "unknown" || len(b.called) != count {
		t.Fatal("writer performed an automatic retry")
	}
}

func TestWriterStopsAtEveryPartialFailureAndKeepsUnknown(t *testing.T) {
	for index, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			r, p, readback := writerFixture()
			a := &authorityFixture{permit: p}
			b := &backendFixture{request: r, readback: readback, failStage: stage}
			result, err := Run(context.Background(), r, p, a, b)
			if err == nil || result.Outcome != "unknown" || result.Stage != stage || !b.quarantined || len(b.called) != index+1 || result.Receipt == nil || result.Receipt.Outcome != "unknown" || result.Receipt.ReadbackSHA256 != "" {
				t.Fatalf("unsafe partial write result: %#v %v calls=%v", result, err, b.called)
			}
		})
	}
}

func TestWriterRejectsChangedPreparationAndWrongCurrentAuthority(t *testing.T) {
	for name, change := range map[string]func(*provisionboot.WriteAuthorization){
		"other_permit":    func(a *provisionboot.WriteAuthorization) { a.PermitID = strings.Repeat("9", 32) },
		"other_boot":      func(a *provisionboot.WriteAuthorization) { a.BootID = strings.Repeat("9", 32) },
		"other_stage":     func(a *provisionboot.WriteAuthorization) { a.Stage = "root.extract" },
		"wrong_gpt":       func(a *provisionboot.WriteAuthorization) { a.GPTSHA256 = strings.Repeat("9", 64) },
		"expired":         func(a *provisionboot.WriteAuthorization) { a.ValidUntil = time.Now().Add(-time.Second) },
		"long_permission": func(a *provisionboot.WriteAuthorization) { a.ValidUntil = time.Now().Add(time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			r, p, readback := writerFixture()
			a := &authorityFixture{permit: p, change: change}
			b := &backendFixture{request: r, readback: readback}
			result, err := Run(context.Background(), r, p, a, b)
			if err == nil || result.Outcome != "unknown" || len(b.called) != 0 || !b.quarantined {
				t.Fatal("write began under mismatched authority")
			}
		})
	}
	r, p, readback := writerFixture()
	a := &authorityFixture{permit: p}
	b := &backendFixture{request: r, readback: readback}
	r.TargetUKISHA256 = strings.Repeat("0", 64)
	if result, err := Run(context.Background(), r, p, a, b); err == nil || result.Outcome != "unknown" || len(b.called) != 0 {
		t.Fatal("changed sources reached a write")
	}
}

func TestWriterWatchdogCancelsToolBeforeNextPollingTick(t *testing.T) {
	r, p, readback := writerFixture()
	a := &authorityFixture{permit: p, validFor: 40 * time.Millisecond}
	b := &backendFixture{request: r, readback: readback, waitStage: "gpt.write"}
	started := time.Now()
	result, err := Run(context.Background(), r, p, a, b)
	if err == nil || result.Outcome != "unknown" || !b.quarantined || time.Since(started) > time.Second || a.checkCount != 1 {
		t.Fatalf("expired tool was not stopped: %#v %v", result, err)
	}
}

func TestWriterRevocationStopsInflightOperationWithoutNextStage(t *testing.T) {
	r, p, readback := writerFixture()
	a := &authorityFixture{permit: p, failAfter: 1}
	b := &backendFixture{request: r, readback: readback, waitStage: "gpt.write"}
	result, err := Run(context.Background(), r, p, a, b)
	if err == nil || result.Outcome != "unknown" || len(b.called) != 1 || !b.quarantined {
		t.Fatalf("revoked operation continued: %#v %v", result, err)
	}
}

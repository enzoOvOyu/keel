//go:build linux

package provisionwriter

import (
	"context"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"golang.org/x/sys/unix"
)

func validRebootAcknowledgment(ack *provisionboot.BootAcknowledgment, permit *provisionboot.WritePermit, expires time.Time, now time.Time) bool {
	return ack != nil && permit != nil && ack.RebootBefore != nil && ack.InstallationID == permit.InstallationID && ack.BootID == permit.BootID && ack.Sequence == permit.Sequence+1 && ack.State == "awaiting_first_boot" && ack.ExpiresAt.Equal(expires) && ack.RebootBefore.After(now) && !ack.RebootBefore.After(now.Add(2*time.Second)) && !ack.RebootBefore.After(permit.ExpiresAt) && !ack.RebootBefore.After(expires)
}

// Reboot is the sole transition from a verified local write to a restart. The
// caller must have authenticated and retained the matching one-shot written
// receipt ACK. Its short current-lease deadline is checked before closing clean
// mappings and again immediately before the syscall. Missing/old ACKs only
// leave the machine waiting; a failed attempt is never retried automatically.
func (w *Linux) Reboot(ctx context.Context, ack *provisionboot.BootAcknowledgment) error {
	if w == nil || requireInitramfs() != nil {
		return ErrDenied
	}
	if ctx.Err() != nil {
		w.Quarantine()
		return ErrDenied
	}
	w.mu.Lock()
	if !w.completed || w.quarantined || w.rebootAttempted || w.disk == nil || w.nextStage != len(stages) || w.readback == nil || !validRebootAcknowledgment(ack, w.permit, w.plan.Spec.ExpiresAt, time.Now()) {
		w.mu.Unlock()
		w.Quarantine()
		return ErrDenied
	}
	for _, mounted := range w.mounted {
		if mounted {
			w.mu.Unlock()
			w.Quarantine()
			return ErrDenied
		}
	}
	deadline := *ack.RebootBefore
	w.rebootAttempted = true
	w.mu.Unlock()
	// All filesystem sync/unmount and on-device readback already succeeded in
	// Run. Close only detaches these owned clean loop mappings and descriptors;
	// it does not run a global sync or touch unrelated filesystems.
	if w.Close() != nil {
		w.Quarantine()
		return ErrDenied
	}
	w.mu.Lock()
	if w.quarantined || ctx.Err() != nil || !deadline.After(time.Now()) {
		w.mu.Unlock()
		w.Quarantine()
		return ErrDenied
	}
	// RequireInitramfs excludes ordinary hosts/containers. This syscall has no
	// shell, selectable reboot command, retry or fallback power-control path.
	err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	w.mu.Unlock()
	// Success does not return. Even an unexpected nil return is uncertain.
	_ = err
	w.Quarantine()
	return ErrDenied
}

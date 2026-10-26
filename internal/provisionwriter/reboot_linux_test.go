//go:build linux

package provisionwriter

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

func TestRebootRequiresFreshMatchingWrittenReceiptAcknowledgment(t *testing.T) {
	_, p, _ := writerFixture()
	now := time.Now()
	before := now.Add(time.Second)
	ack := provisionboot.BootAcknowledgment{InstallationID: p.InstallationID, BootID: p.BootID, Sequence: p.Sequence + 1, State: "awaiting_first_boot", ExpiresAt: p.ExpiresAt, RebootBefore: &before}
	if !validRebootAcknowledgment(&ack, p, p.ExpiresAt, now) {
		t.Fatal("fresh matching ACK rejected")
	}
	for name, edit := range map[string]func(*provisionboot.BootAcknowledgment){
		"missing_authorization":           func(a *provisionboot.BootAcknowledgment) { a.RebootBefore = nil },
		"wrong_boot":                      func(a *provisionboot.BootAcknowledgment) { a.BootID = "other" },
		"wrong_installation":              func(a *provisionboot.BootAcknowledgment) { a.InstallationID = "other" },
		"wrong_sequence":                  func(a *provisionboot.BootAcknowledgment) { a.Sequence++ },
		"unknown_write":                   func(a *provisionboot.BootAcknowledgment) { a.State = "unknown" },
		"registered_is_not_authorization": func(a *provisionboot.BootAcknowledgment) { a.State = "registered" },
		"expired":                         func(a *provisionboot.BootAcknowledgment) { v := now; a.RebootBefore = &v },
		"long_permission":                 func(a *provisionboot.BootAcknowledgment) { v := now.Add(3 * time.Second); a.RebootBefore = &v },
		"changed_expiry":                  func(a *provisionboot.BootAcknowledgment) { a.ExpiresAt = a.ExpiresAt.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := ack
			edit(&copy)
			if validRebootAcknowledgment(&copy, p, p.ExpiresAt, now) {
				t.Fatal("unsafe restart was authorized")
			}
		})
	}
	if validRebootAcknowledgment(&ack, p, p.ExpiresAt, now.Add(2*time.Second)) {
		t.Fatal("replayed expired reboot ACK")
	}
}

func TestRebootAdapterRejectsOrdinaryHostBeforeAnyDeviceOperation(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("ordinary process guard")
	}
	if (&Linux{}).Reboot(context.Background(), nil) == nil {
		t.Fatal("ordinary host accepted a restart")
	}
}

func TestRebootMappingCleanupPreservesExclusiveDescriptorOnUnknown(t *testing.T) {
	// A regular temporary file stands in only for descriptor ownership. This
	// regression test never opens a block device or creates a loop mapping.
	disk, err := os.CreateTemp(t.TempDir(), "retained-descriptor")
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	w := &Linux{disk: disk, completed: true}
	w.mu.Lock()
	err = w.closeOwnedLoopsLocked()
	w.mu.Unlock()
	if err != nil || w.disk != disk {
		t.Fatal("pre-reboot mapping cleanup dropped the exclusive descriptor")
	}
	w.Quarantine()
	defer func() {
		heldUnknown.Lock()
		delete(heldUnknown.writers, w)
		heldUnknown.Unlock()
	}()
	if w.Close() == nil {
		t.Fatal("uncertain reboot released descriptor ownership")
	}
	if _, err = disk.Stat(); err != nil {
		t.Fatal("uncertain reboot closed the retained descriptor", err)
	}
}

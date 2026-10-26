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

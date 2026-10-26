package provisionboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"testing"
)

// These exercise the private socket-evidence boundary without opening sockets.
// sent models a confirmed full UDP write; ACKs model an already checked TID.
func TestTFTPDeliveryRequiresFullWritesAndFinalACK(t *testing.T) {
	for _, size := range []int{1, 511, 512, 513, 1024} {
		for _, mode := range []string{"complete", "no_send_confirmation", "missing_final_ack", "closed_early", "revoked_after_ack", "canceled_after_ack", "evidence_failure"} {
			t.Run(fmt.Sprintf("%d/%s", size, mode), func(t *testing.T) {
				scope, base := fixture()
				base.data = bytes.Repeat([]byte{42}, size)
				a := &deliveryFixture{authorityFixture: base, failRecord: mode == "evidence_failure"}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				request := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/undionly.kpxe\x00octet\x00")...)
				transfer, err := OpenTFTP(ctx, scope, a, netip.MustParseAddr("192.0.2.22"), request)
				if err != nil {
					t.Fatal(err)
				}
				defer transfer.Close()
				packet, err := transfer.Next(nil)
				if err != nil {
					t.Fatal(err)
				}
				for !transfer.Done() {
					if mode != "no_send_confirmation" {
						if err = transfer.sent(packet); err != nil {
							t.Fatal(err)
						}
						// A timeout retry confirms the same DATA without double-counting it.
						retry, e := transfer.Retry()
						if e != nil || !bytes.Equal(retry, packet) {
							t.Fatal("retry changed current data", e)
						}
						if err = transfer.sent(retry); err != nil {
							t.Fatal(err)
						}
					}
					transfer.recordAcknowledged()
					if len(a.recorded) != 0 {
						t.Fatal("recorded before final ACK")
					}
					if mode == "closed_early" {
						transfer.Close()
						break
					}
					if len(packet) < 516 && mode == "missing_final_ack" {
						break
					}
					ack := []byte{0, 4, packet[2], packet[3]}
					packet, err = transfer.Next(ack)
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "revoked_after_ack" {
					a.mu.Lock()
					a.denied = true
					a.mu.Unlock()
				}
				if mode == "canceled_after_ack" {
					cancel()
				}
				transfer.recordAcknowledged()
				transfer.recordAcknowledged()
				want := mode == "complete" || mode == "evidence_failure"
				if (len(a.recorded) == 1) != want {
					t.Fatal("unproven or duplicate delivery", mode, len(a.recorded))
				}
				if want {
					sum := sha256.Sum256(base.data)
					d := a.recorded[0]
					if d.Resource != "undionly.kpxe" || d.Bytes != int64(size) || d.SHA256 != hex.EncodeToString(sum[:]) || d.ObjectVersion != "immutable-fixture-v1" || transfer.sentBytes != int64(size) {
						t.Fatal("TFTP identity or byte count changed")
					}
				}
			})
		}
	}
}

func TestTFTPDeliveryRejectsChangedOrSkippedWrites(t *testing.T) {
	scope, base := fixture()
	base.data = bytes.Repeat([]byte{42}, 1024)
	a := &deliveryFixture{authorityFixture: base}
	request := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/ipxe.efi\x00octet\x00")...)
	transfer, err := OpenTFTP(context.Background(), scope, a, netip.MustParseAddr("192.0.2.22"), request)
	if err != nil {
		t.Fatal(err)
	}
	defer transfer.Close()
	packet, err := transfer.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte{}, packet...)
	changed[4] ^= 1
	if transfer.sent(changed) == nil {
		t.Fatal("different bytes accepted as a sent DATA block")
	}
	ack := []byte{0, 4, 0, 1}
	packet, err = transfer.Next(ack)
	if err != nil || binary.BigEndian.Uint16(packet[2:4]) != 2 {
		t.Fatal("missing second block", err)
	}
	if transfer.sent(packet) == nil {
		t.Fatal("unsent prior block accepted")
	}
	transfer.Close()
	transfer.recordAcknowledged()
	if len(a.recorded) != 0 {
		t.Fatal("incomplete transfer created evidence")
	}
}

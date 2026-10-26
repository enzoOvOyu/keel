package fabricboot

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDHCPWireDORA(t *testing.T) {
	for _, scenario := range []string{"ok", "dual-ok", "dual-mismatch", "vendor-ok", "vendor-missing", "vendor-wrong-ack", "wrong-url", "wrong-identity", "nak", "changed-server", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			server, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if e != nil {
				t.Fatal(e)
			}
			defer server.Close()
			local, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if e != nil {
				t.Fatal(e)
			}
			listen := local.LocalAddr().String()
			local.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				buf := make([]byte, 4096)
				server.SetReadDeadline(time.Now().Add(2 * time.Second))
				for i := 0; i < 2; i++ {
					n, peer, e := server.ReadFromUDP(buf)
					if e != nil {
						return
					}
					request := append([]byte(nil), buf[:n]...)
					if len(request) < 240 {
						return
					}
					// A real UDP packet exchange, keeping xid/chaddr/giaddr from the request.
					response := append([]byte(nil), request[:240]...)
					response[0] = 2
					copy(response[16:20], []byte{192, 0, 2, 250})
					kind := byte(2)
					if i == 1 {
						kind = 5
					}
					if scenario == "nak" {
						kind = 6
					}
					if scenario == "wrong-identity" {
						response[4] ^= 1
					}
					url := "https://boot.example.invalid/ztp"
					if scenario == "wrong-url" {
						url += "-foreign"
					}
					sid := []byte{127, 0, 0, 1}
					if scenario == "changed-server" && i == 1 {
						sid = []byte{127, 0, 0, 2}
					}
					response = append(response, 53, 1, kind, 54, 4)
					response = append(response, sid...)
					response = append(response, 51, 4)
					lease := make([]byte, 4)
					binary.BigEndian.PutUint32(lease, 600)
					response = append(response, lease...)
					response = append(response, 239, byte(len(url)))
					response = append(response, []byte(url)...)
					if strings.HasPrefix(scenario, "dual-") {
						uri := "https://sztp.example.invalid:8443"
						if scenario == "dual-mismatch" {
							uri = "https://foreign.example.invalid:8443"
						}
						data, _ := EncodeSZTPRedirect([]string{uri})
						response = append(response, 143, byte(len(data)))
						response = append(response, data...)
					}
					if strings.HasPrefix(scenario, "vendor-") && scenario != "vendor-missing" {
						data, _ := testDHCPVendorProfile().Payload()
						if scenario == "vendor-wrong-ack" && i == 1 {
							data[len(data)-1] ^= 1
						}
						response = append(response, 43, byte(len(data)))
						response = append(response, data...)
					}
					response = append(response, 255)
					if scenario == "truncated" {
						response = response[:len(response)-3]
					}
					server.WriteToUDP(response, peer)
				}
			}()
			cfg := DHCPProbeConfig{Server: server.LocalAddr().String(), Listen: listen, RelayIP: "127.0.0.1", MAC: "02:00:00:00:ff:01", Address: "192.0.2.250", TimeoutSeconds: 1}
			var redirects []string
			if strings.HasPrefix(scenario, "dual-") {
				redirects = []string{"https://sztp.example.invalid:8443"}
			}
			var proof *DHCPProof
			if strings.HasPrefix(scenario, "vendor-") {
				proof, e = cfg.VerifyVendorBootstrap(context.Background(), "https://boot.example.invalid/ztp", redirects, testDHCPVendorProfile())
			} else {
				proof, e = cfg.VerifyBootstrap(context.Background(), "https://boot.example.invalid/ztp", "cumulus-linux x86_64", redirects)
			}
			if scenario == "vendor-ok" && !testDHCPVendorProfile().MatchesProof(proof) {
				t.Fatal("missing exact vendor observation")
			}
			if scenario == "ok" || scenario == "dual-ok" || scenario == "vendor-ok" {
				if e != nil || proof == nil || len(proof.TranscriptSHA256) != 64 || proof.LeaseSeconds != 600 {
					t.Fatalf("proof=%+v err=%v", proof, e)
				}
			} else if e == nil {
				t.Fatal("invalid DHCP exchange accepted")
			}
			server.Close()
			<-done
		})
	}
}
func TestDHCPProbeConfigAndCancellation(t *testing.T) {
	cfg := DHCPProbeConfig{Server: "127.0.0.1:67", Listen: "127.0.0.1:1067", RelayIP: "127.0.0.1", MAC: "02:00:00:00:ff:01", Address: "192.0.2.250"}
	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
	bad := cfg
	bad.MAC = "ff:ff:ff:ff:ff:ff"
	if bad.Validate() == nil {
		t.Fatal("multicast canary accepted")
	}
	bad = cfg
	bad.Listen = "0.0.0.0:67"
	if bad.Validate() == nil {
		t.Fatal("ambiguous relay accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, e := cfg.Verify239(ctx, "https://boot.example.invalid/ztp", "")
	if e == nil || time.Since(start) > time.Second {
		t.Fatalf("cancellation err=%v", e)
	}
	if _, e = cfg.Verify239(context.Background(), strings.Repeat("x", 256), ""); e == nil {
		t.Fatal("oversize option accepted")
	}
}

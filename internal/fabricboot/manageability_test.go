package fabricboot

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManageabilityRequiresBoundAuthenticatedProof(t *testing.T) {
	input := ManageabilityRequest{Version: "ztp-manageability-request/v1", ID: strings.Repeat("a", 32), ProjectID: strings.Repeat("b", 32), SwitchID: strings.Repeat("c", 32), Serial: "SN-1", ManagementMAC: "02:00:00:00:00:01", PlanHash: strings.Repeat("d", 64), OfferHash: strings.Repeat("e", 64), BootEvidenceSHA256: strings.Repeat("f", 64), ScriptSHA256: strings.Repeat("a", 64), CredentialBatchRef: "vault:switch:1", Hostname: "dc1-p1-leaf-r1-1", Address: "192.0.2.10", PrefixBits: 24, Gateway: "192.0.2.1"}
	mode := "ready"
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/projects/"+input.ProjectID+"/ztp/manageability" || len(r.TLS.PeerCertificates) == 0 {
			t.Error("not a fixed authenticated gateway request")
		}
		raw, _ := io.ReadAll(r.Body)
		hash := sha256.Sum256(raw)
		proof := ManageabilityProof{Version: "ztp-manageability/v1", RequestSHA256: hex.EncodeToString(hash[:]), ProjectID: input.ProjectID, SwitchID: input.SwitchID, Serial: input.Serial, Source: "protocol_fixture", Code: "ready", CredentialBatchRef: input.CredentialBatchRef, At: time.Now().UTC(), Hostname: input.Hostname, Address: input.Address, PrefixBits: input.PrefixBits, Gateway: input.Gateway, MgmtVRF: "mgmt", HostKeySHA256: strings.Repeat("b", 64), HostKeyTrustRef: "approved-host-key:1", SSHAuthenticated: true, CredentialPurpose: "ztp-management-read", ConfigImported: true, ServicesRestarted: true, FrontPanelIsolated: true, DHCPDORAVerified: true, DHCPClientMAC: input.ManagementMAC, DHCPLeaseIP: input.Address, DHCPProofSHA256: strings.Repeat("c", 64), EvidenceRefs: []string{strings.Repeat("d", 64)}}
		switch mode {
		case "wrong-request":
			proof.RequestSHA256 = strings.Repeat("0", 64)
		case "stale":
			proof.At = time.Now().Add(-2 * time.Minute)
		case "wrong-device":
			proof.Serial = "OTHER"
		case "untrusted-ssh":
			proof.HostKeyTrustRef = ""
		case "not-isolated":
			proof.FrontPanelIsolated = false
		case "dhcp-canary-only":
			proof.DHCPClientMAC = "02:00:00:00:00:99"
		case "expired":
			proof.Code = "credential_expired"
			proof.SSHAuthenticated = false
		case "redirect":
			http.Redirect(w, r, "/other", 302)
			return
		}
		json.NewEncoder(w).Encode(proof)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.Certificates = server.TLS.Certificates
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	suite := &ProbeSuite{targets: map[string]environmentProbe{"network": {target: ProbeTarget{URL: server.URL + "/readyz"}, client: client}}}
	for _, name := range []string{"ready", "wrong-request", "stale", "wrong-device", "untrusted-ssh", "not-isolated", "dhcp-canary-only", "expired", "redirect"} {
		t.Run(name, func(t *testing.T) {
			mode = name
			proof, e := suite.VerifyManageability(context.Background(), input)
			if name == "ready" || name == "expired" {
				if e != nil {
					t.Fatal(e)
				}
				if proof.Source != "protocol_fixture" {
					t.Fatal("source was rewritten as a device")
				}
			} else if e == nil {
				t.Fatal("unsafe proof accepted")
			}
		})
	}
}

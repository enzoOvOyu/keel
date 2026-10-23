package fabricboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

type ManageabilityRequest struct {
	Version            string `json:"version"`
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	BatchRef           string `json:"batch_ref"`
	SwitchID           string `json:"switch_id"`
	Serial             string `json:"serial_number"`
	ManagementMAC      string `json:"management_mac"`
	PlanHash           string `json:"plan_hash"`
	OfferHash          string `json:"offer_hash"`
	BootEvidenceSHA256 string `json:"boot_evidence_sha256"`
	ScriptSHA256       string `json:"script_sha256"`
	CredentialBatchRef string `json:"credential_batch_ref"`
	Hostname           string `json:"hostname"`
	Address            string `json:"management_ip"`
	PrefixBits         int    `json:"prefix_bits"`
	Gateway            string `json:"gateway"`
}
type ManageabilityStageEvidence struct {
	State  string    `json:"state"`
	At     time.Time `json:"at"`
	SHA256 string    `json:"sha256"`
}
type ManageabilityProof struct {
	StageEvidence      []ManageabilityStageEvidence `json:"stage_evidence,omitempty"`
	Version            string                       `json:"version"`
	RequestSHA256      string                       `json:"request_sha256"`
	ProjectID          string                       `json:"project_id"`
	SwitchID           string                       `json:"switch_id"`
	Serial             string                       `json:"serial_number"`
	Source             string                       `json:"evidence_source"`
	Code               string                       `json:"code"`
	CredentialBatchRef string                       `json:"credential_batch_ref"`
	At                 time.Time                    `json:"at"`
	Hostname           string                       `json:"hostname"`
	Address            string                       `json:"management_ip"`
	PrefixBits         int                          `json:"prefix_bits"`
	Gateway            string                       `json:"gateway"`
	MgmtVRF            string                       `json:"management_vrf"`
	HostKeySHA256      string                       `json:"host_key_sha256"`
	HostKeyTrustRef    string                       `json:"host_key_trust_ref"`
	SSHAuthenticated   bool                         `json:"ssh_authenticated"`
	CredentialPurpose  string                       `json:"credential_purpose"`
	ConfigImported     bool                         `json:"config_imported"`
	ServicesRestarted  bool                         `json:"services_restarted"`
	FrontPanelIsolated bool                         `json:"front_panel_isolated"`
	DHCPDORAVerified   bool                         `json:"dhcp_dora_verified"`
	DHCPClientMAC      string                       `json:"dhcp_client_mac"`
	DHCPLeaseIP        string                       `json:"dhcp_lease_ip"`
	DHCPProofSHA256    string                       `json:"dhcp_proof_sha256"`
	EvidenceRefs       []string                     `json:"evidence_refs"`
}

func (s *ProbeSuite) ManageabilityConfigured() bool {
	if s == nil {
		return false
	}
	p, ok := s.targets["network"]
	if !ok {
		return false
	}
	t, ok := p.client.Transport.(*http.Transport)
	return ok && t.TLSClientConfig != nil && len(t.TLSClientConfig.Certificates) > 0
}

// VerifyManageability is a read-only mTLS gateway operation. The gateway resolves
// the credential reference server-side and must verify a trusted SSH host key.
func (s *ProbeSuite) VerifyManageability(ctx context.Context, input ManageabilityRequest) (*ManageabilityProof, error) {
	if !s.ManageabilityConfigured() {
		return nil, errors.New("manageability adapter is not configured")
	}
	if input.Version != "ztp-manageability-request/v1" || !projectPattern.MatchString(input.ProjectID) || !projectPattern.MatchString(input.SwitchID) || !projectPattern.MatchString(input.ID) || !keaHashPattern.MatchString(input.PlanHash) || !keaHashPattern.MatchString(input.OfferHash) || !keaHashPattern.MatchString(input.ScriptSHA256) || !keaHashPattern.MatchString(input.BootEvidenceSHA256) || input.CredentialBatchRef == "" {
		return nil, errors.New("invalid manageability request")
	}
	probe := s.targets["network"]
	origin, e := url.Parse(probe.target.URL)
	if e != nil {
		return nil, e
	}
	origin.Path = "/v1/projects/" + input.ProjectID + "/ztp/manageability"
	origin.RawPath = ""
	origin.RawQuery = ""
	raw, e := json.Marshal(input)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := *probe.client
	client.Timeout = 8 * time.Second
	response, e := client.Do(req)
	if e != nil {
		return nil, errors.New("manageability adapter unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("manageability adapter rejected the request")
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(body) > 65536 {
		return nil, errors.New("manageability proof is unreadable or oversized")
	}
	var proof ManageabilityProof
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proof) != nil {
		return nil, errors.New("manageability proof has invalid fields")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("manageability proof has trailing data")
	}
	if e = ValidateManageabilityProof(input, &proof, time.Now().UTC()); e != nil {
		return nil, e
	}
	return &proof, nil
}

func ValidateManageabilityProof(input ManageabilityRequest, proof *ManageabilityProof, now time.Time) error {
	if proof == nil {
		return errors.New("manageability proof is absent")
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if proof.Version != "ztp-manageability/v1" || proof.RequestSHA256 != hash || proof.ProjectID != input.ProjectID || proof.SwitchID != input.SwitchID || proof.Serial != input.Serial || proof.CredentialBatchRef != input.CredentialBatchRef || proof.At.Before(now.Add(-time.Minute)) || proof.At.After(now.Add(5*time.Second)) || proof.Source != "device" && proof.Source != "protocol_fixture" {
		return errors.New("manageability proof identity, freshness or request binding mismatch")
	}
	switch proof.Code {
	case "ready", "credential_expired", "unreachable", "not_ready":
	default:
		return errors.New("unsupported manageability result")
	}

	if len(proof.StageEvidence) > 0 {
		stages := []string{"powered", "dhcp", "booturl", "identity", "lookup", "fetch", "config", "restart", "managed"}
		if len(proof.StageEvidence) != len(stages) {
			return errors.New("incomplete detailed stage evidence")
		}
		previous := now.Add(-24 * time.Hour)
		for i, stage := range proof.StageEvidence {
			if stage.State != stages[i] || stage.At.Before(previous) || stage.At.After(proof.At.Add(5*time.Second)) || !keaHashPattern.MatchString(stage.SHA256) {
				return errors.New("invalid detailed stage order, timestamp or evidence hash")
			}
			previous = stage.At
		}
	}
	if proof.Code == "ready" {
		if proof.Hostname != input.Hostname || proof.Address != input.Address || proof.PrefixBits != input.PrefixBits || proof.Gateway != input.Gateway || proof.MgmtVRF != "mgmt" || !proof.SSHAuthenticated || proof.CredentialPurpose != "ztp-management-read" || !proof.ConfigImported || !proof.ServicesRestarted || !proof.FrontPanelIsolated || !proof.DHCPDORAVerified || proof.DHCPClientMAC != input.ManagementMAC || proof.DHCPLeaseIP != input.Address || !keaHashPattern.MatchString(proof.DHCPProofSHA256) || !keaHashPattern.MatchString(proof.HostKeySHA256) || proof.HostKeyTrustRef == "" || len(proof.HostKeyTrustRef) > 160 || len(proof.EvidenceRefs) == 0 || len(proof.EvidenceRefs) > 32 {
			return errors.New("manageability proof lacks authenticated configuration, isolation or service evidence")
		}
		for _, ref := range proof.EvidenceRefs {
			if !keaHashPattern.MatchString(ref) {
				return errors.New("invalid manageability evidence reference")
			}
		}
	}
	return nil
}

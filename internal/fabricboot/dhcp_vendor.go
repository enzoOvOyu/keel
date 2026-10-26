package fabricboot

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

// DHCPVendorProfile is operator supplied, public protocol metadata. Suboption
// numbers have no platform defaults: their meaning belongs to the target NOS.
// SourceRef identifies the vendor contract/PoC record used to configure them.
type DHCPVendorProfile struct {
	Version          string `json:"version"`
	SourceRef        string `json:"source_ref"`
	VCI              string `json:"vci"`
	NOS              string `json:"nos"`
	VersionIntent    string `json:"version_intent"`
	NOSSuboption     int    `json:"nos_suboption"`
	VersionSuboption int    `json:"version_suboption"`
}

func (p DHCPVendorProfile) Payload() ([]byte, error) {
	if p.Version != "dhcp-vendor-tlv/v1" || p.NOSSuboption < 1 || p.NOSSuboption > 254 || p.VersionSuboption < 1 || p.VersionSuboption > 254 || p.NOSSuboption == p.VersionSuboption {
		return nil, errors.New("vendor profile needs an explicit version and distinct suboption codes 1..254")
	}
	for _, value := range []string{p.SourceRef, p.VCI, p.NOS, p.VersionIntent} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 128 || !utf8.ValidString(value) {
			return nil, errors.New("invalid vendor profile text")
		}
		for _, r := range value {
			if r < 32 || r == 127 {
				return nil, errors.New("vendor profile contains control characters")
			}
		}
	}
	if len(p.NOS)+len(p.VersionIntent)+4 > 255 {
		return nil, errors.New("vendor suboptions exceed the single option43 capability")
	}
	data := []byte{byte(p.NOSSuboption), byte(len(p.NOS))}
	data = append(data, p.NOS...)
	data = append(data, byte(p.VersionSuboption), byte(len(p.VersionIntent)))
	return append(data, p.VersionIntent...), nil
}

// MatchesProof requires the exact vendor contract and the real Offer/Ack
// observation. This only proves the canary exchange, never device acceptance.
func (p DHCPVendorProfile) MatchesProof(proof *DHCPProof) bool {
	data, err := p.Payload()
	return err == nil && proof != nil && proof.Option60VCI == p.VCI && proof.VendorProfileSHA256 == ManagementPlanDigest(p) && proof.Option43Hex == hex.EncodeToString(data)
}

func verifyVendorReply(options map[byte][]byte, expected []byte) error {
	if len(expected) > 0 && !bytes.Equal(options[43], expected) {
		return errors.New("DHCP vendor suboptions differ from the frozen vendor profile")
	}
	return nil
}

func (s *ProbeSuite) DHCPVendorProfile() *DHCPVendorProfile {
	if s == nil || s.vendor == nil {
		return nil
	}
	p := *s.vendor
	return &p
}

package fabricboot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const ManagementRendererV1 = "cumulus-nvue-minimum-management/v1"

type ManagementScript struct {
	SwitchID           string `json:"switch_id"`
	Serial             string `json:"serial_number"`
	CredentialBatchRef string `json:"credential_batch_ref"`
	SHA256             string `json:"sha256"`
	Size               int    `json:"size"`
	Body               string `json:"body,omitempty"`
}

// RenderManagementScript has no caller-provided commands or credentials. It
// preserves the existing mgmt VRF, disables front-panel ports and installs only
// the approved hostname and eth0 IPv4 route. Production configuration is absent.
func RenderManagementScript(r ManagementRules, a ManagementAssignment, credentialRef string) (*ManagementScript, error) {
	if e := ValidateManagementRules(r); e != nil {
		return nil, e
	}
	prefix, _ := netip.ParsePrefix(r.CIDR)
	ip, e := netip.ParseAddr(a.Address)
	if e != nil || !usableManagementIP(prefix, ip) || a.Gateway != r.Gateway || !hostnamePart.MatchString(a.Hostname) || len(a.Hostname) > 63 || a.Serial == "" || a.SwitchID == "" || a.Batch == "" {
		return nil, errors.New("management assignment does not match approved rules")
	}
	if len(credentialRef) < 1 || len(credentialRef) > 120 || strings.ContainsAny(credentialRef, "\r\n\x00") {
		return nil, errors.New("a registered switch credential batch is required")
	}
	commands := fmt.Sprintf("nv set system hostname %s\nnv unset interface eth0 ip address dhcp\nnv set interface eth0 ip address %s/%d\nnv set interface eth0 ip gateway %s\n", a.Hostname, ip.String(), prefix.Bits(), a.Gateway)
	body := strings.Replace(IsolationScriptV1, "nv config apply --assume-yes\n", commands+"nv config apply --assume-yes\nnv config save\n", 1)
	sum := sha256.Sum256([]byte(body))
	return &ManagementScript{SwitchID: a.SwitchID, Serial: a.Serial, CredentialBatchRef: credentialRef, SHA256: hex.EncodeToString(sum[:]), Size: len(body), Body: body}, nil
}

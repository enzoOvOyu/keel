package fabricboot

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSZTPRedirectEncoding(t *testing.T) {
	uris := []string{"https://boot.example.invalid:8443", "https://[2001:db8::1]:8443"}
	data, e := EncodeSZTPRedirect(uris)
	if e != nil {
		t.Fatal(e)
	}
	if int(binary.BigEndian.Uint16(data)) != len(uris[0]) {
		t.Fatal("not RFC8572 length prefix")
	}
	decoded, e := DecodeSZTPRedirect(data)
	if e != nil || len(decoded) != 2 || decoded[1] != uris[1] {
		t.Fatal(decoded, e)
	}
	packet, e := EncodeSZTPv6Option(uris)
	if e != nil || binary.BigEndian.Uint16(packet) != 136 || int(binary.BigEndian.Uint16(packet[2:])) != len(data) || !bytes.Equal(packet[4:], data) {
		t.Fatal("invalid option136")
	}
	for _, u := range []string{"http://boot.example.invalid", "https://boot.example.invalid/", "https://user@boot.example.invalid", "https://boot.example.invalid?secret=1"} {
		if _, e := EncodeSZTPRedirect([]string{u}); e == nil {
			t.Fatal("invalid origin accepted", u)
		}
	}
	if _, e := DecodeSZTPRedirect(data[:len(data)-1]); e == nil {
		t.Fatal("truncated redirect accepted")
	}
}
func TestKeaDualBootstrapMerge(t *testing.T) {
	live := json.RawMessage(`{"Dhcp4":{"subnet4":[{"id":1,"subnet":"192.0.2.0/24"}]}}`)
	binding := []KeaReservation{{SwitchID: "switch", MAC: "02:00:00:00:00:01", IP: "192.0.2.2", Hostname: "switch"}}
	uris := []string{"https://sztp.example.invalid:8443"}
	result, e := MergeBootstrapDual(live, "192.0.2.0/24", "https://ztp.example.invalid/script", "owner", binding, uris)
	if e != nil {
		t.Fatal(e)
	}
	var config struct {
		Dhcp4 struct {
			Subnet4 []struct {
				Reservations []struct {
					Options []struct {
						Code int    `json:"code"`
						Data string `json:"data"`
					} `json:"option-data"`
				} `json:"reservations"`
			} `json:"subnet4"`
		} `json:"Dhcp4"`
	}
	if json.Unmarshal(result, &config) != nil {
		t.Fatal("invalid result")
	}
	options := config.Dhcp4.Subnet4[0].Reservations[0].Options
	if len(options) != 2 || options[1].Code != 143 {
		t.Fatal(options)
	}
	expected, _ := EncodeSZTPRedirect(uris)
	if options[1].Data != hex.EncodeToString(expected) {
		t.Fatal("wrong wire redirect")
	}
	if _, e = MergeBootstrapDual(result, "192.0.2.0/24", "https://ztp.example.invalid/script", "owner", binding, uris); e != nil {
		t.Fatal("owned dual reservation not reusable", e)
	}
	if _, e = MergeBootstrap239(result, "192.0.2.0/24", "https://ztp.example.invalid/script", "owner", binding); e == nil {
		t.Fatal("dual option silently dropped")
	}
}
func TestReadOnlyLiveControlCannotFallbackToMock(t *testing.T) {
	suite := &ProbeSuite{targets: map[string]environmentProbe{"kea": {target: ProbeTarget{AllowConfigWrite: false}}}}
	if !suite.LiveBootConfigured() {
		t.Fatal("disabling writes must not reopen mock routes")
	}
}

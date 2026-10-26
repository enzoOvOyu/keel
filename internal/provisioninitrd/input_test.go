package provisioninitrd

import (
	"net/netip"
	"strings"
	"testing"
)

func mustAddr(raw string) netip.Addr     { return netip.MustParseAddr(raw) }
func mustPrefix(raw string) netip.Prefix { return netip.MustParsePrefix(raw) }
func validCommandLine() string {
	return "console=ttyS0 ip=off BOOTIF=01-02-aa-bb-cc-dd-ee keel.protocol=keel-initrd/v1 keel.server_ipv4=192.0.2.1 keel.client_cidr=192.0.2.0/24 keel.manifest=https://boot.example.test/boot/" + strings.Repeat("3", 32) + "/manifest.json keel.manifest_sha256=" + strings.Repeat("a", 64)
}
func TestInitrdCommandLineRequiresExactControlledInputs(t *testing.T) {
	raw := validCommandLine()
	i, err := ParseCommandLine(raw)
	if err != nil || i.BootMAC != "02:aa:bb:cc:dd:ee" || i.Server != mustAddr("192.0.2.1") {
		t.Fatal("controlled input rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(raw, "ip=off", "ip=dhcp", 1), raw + " ip=off", raw + " keel.unreviewed=yes", raw + " keel.manifest_sha256=" + strings.Repeat("a", 64),
		strings.Replace(raw, "keel.protocol=", "Keel.protocol=", 1), strings.Replace(raw, "https://", "http://", 1), strings.Replace(raw, "manifest.json ", "manifest.json?token=not-allowed ", 1), strings.Replace(raw, "192.0.2.0/24", "198.51.100.0/24", 1), strings.Replace(raw, "01-02-aa", "01-03-aa", 1), strings.Replace(raw, "192.0.2.1 ", "127.0.0.1 ", 1), strings.Replace(raw, "https://boot.example.test/", "https://boot.example.test:65536/", 1), raw + "\x00",
	} {
		if _, err := ParseCommandLine(bad); err == nil {
			t.Fatal("unsafe kernel command line accepted")
		}
	}
}

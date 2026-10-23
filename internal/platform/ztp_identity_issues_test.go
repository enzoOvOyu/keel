package platform

import "testing"

func TestZTPIssueIdentityKeyNormalizesMACOnlyClaims(t *testing.T) {
	a := ztpIssueIdentityKey(ZTPIdentityInput{MgmtMAC: "AA-BB-CC-DD-EE-01"})
	b := ztpIssueIdentityKey(ZTPIdentityInput{MgmtMAC: "aa:bb:cc:dd:ee:01"})
	c := ztpIssueIdentityKey(ZTPIdentityInput{MgmtMAC: "aa:bb:cc:dd:ee:02"})
	if a != b || a == c || a != "mgmt:aa:bb:cc:dd:ee:01" {
		t.Fatalf("identity keys %q %q %q", a, b, c)
	}
	if got := ztpIssueIdentityKey(ZTPIdentityInput{OtherMACs: []string{"AA-BB-CC-DD-EE-03"}}); got != "other:aa:bb:cc:dd:ee:03" {
		t.Fatal(got)
	}
}

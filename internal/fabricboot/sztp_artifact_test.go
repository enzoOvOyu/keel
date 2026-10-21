package fabricboot

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestUnsignedIsolationOnboardingCMS(t *testing.T) {
	if der, err := UnsignedIsolationOnboarding(true); err == nil || der != nil {
		t.Fatal("signed-data-preferred accepted unsigned onboarding")
	}
	der, err := UnsignedIsolationOnboarding(false)
	if err != nil {
		t.Fatal(err)
	}
	var ci struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue
	}
	rest, err := asn1.Unmarshal(der, &ci)
	if err != nil || len(rest) != 0 || !ci.ContentType.Equal(oidSZTPConveyedInfoJSON) || ci.Content.Class != asn1.ClassContextSpecific || ci.Content.Tag != 0 || !ci.Content.IsCompound {
		t.Fatalf("invalid CMS ContentInfo: %+v %v", ci, err)
	}
	var conveyed []byte
	rest, err = asn1.Unmarshal(ci.Content.Bytes, &conveyed)
	if err != nil || len(rest) != 0 {
		t.Fatalf("invalid encapsulated OCTET STRING: %v", err)
	}
	var payload map[string]map[string]string
	if err = json.Unmarshal(conveyed, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 {
		t.Fatalf("unexpected conveyed information: %s", conveyed)
	}
	onboarding := payload["ietf-sztp-conveyed-info:onboarding-information"]
	if len(onboarding) != 1 {
		t.Fatalf("unexpected onboarding data: %s", conveyed)
	}
	script, err := base64.StdEncoding.DecodeString(onboarding["pre-configuration-script"])
	if err != nil || string(script) != IsolationScriptV1 {
		t.Fatalf("isolation script mismatch: %v", err)
	}
}

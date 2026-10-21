package fabricboot

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// RFC 8572 section 10.3 assigns .43 to JSON conveyed information.
var oidSZTPConveyedInfoJSON = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 43}

// UnsignedIsolationOnboarding encodes only the reviewed isolation script as
// RFC 8572 onboarding information. The caller must establish that the device
// trusts the bootstrap server; unsigned onboarding cannot satisfy a request
// carrying signed-data-preferred. It must not be used as a 239 shell response.
func UnsignedIsolationOnboarding(signedDataPreferred bool) ([]byte, error) {
	if signedDataPreferred {
		return nil, errors.New("signed onboarding data is not available")
	}
	conveyed, err := json.Marshal(map[string]any{
		"ietf-sztp-conveyed-info:onboarding-information": map[string]string{
			"pre-configuration-script": base64.StdEncoding.EncodeToString([]byte(IsolationScriptV1)),
		},
	})
	if err != nil {
		return nil, err
	}
	octets, err := asn1.Marshal(conveyed)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue
	}{
		ContentType: oidSZTPConveyedInfoJSON,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: octets},
	})
}

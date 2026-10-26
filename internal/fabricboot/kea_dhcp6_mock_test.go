package fabricboot

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMock136StagingCommitAndImmutableDUIDOffer(t *testing.T) {
	mock := NewMockKea()
	config := sampleOptions()
	config.DHCP6 = &DHCP6MockOptions{ManagementCIDR: "2001:db8:1::/64", Bindings: []KeaDHCP6Reservation{dhcp6Binding()}}
	request := sampleDiscover()
	request.LinkIP = "2001:db8:1::1"
	request.DUID = dhcp6Client
	staged, err := mock.Stage(config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := mock.DORA(request)
	if err != nil || before.Outcome != "no_offer" {
		t.Fatal("staged candidate leaked into offers", before, err)
	}
	// A caller changing its slice cannot alter committed reservations or digest.
	config.DHCP6.Bindings[0].Address = "2001:db8:1::11"
	if err = mock.Commit(config.ProjectID, config.BatchRef, config.StageRef, staged); err != nil {
		t.Fatal(err)
	}
	offer, err := mock.DORA(request)
	if err != nil || offer.Outcome != "ack" || offer.Protocol != "dhcp6" || offer.Address != "2001:db8:1::10" || offer.SwitchID != dhcp6Switch || offer.ConfigHash != staged || !reflect.DeepEqual(offer.Option136URIs, config.Option143URIs) {
		t.Fatal("136 did not bind the staged DUID/address", offer, err)
	}
	if offer.Option239URL != "" || len(offer.Option143URIs) != 0 || offer.Option60VCI != "" || len(offer.Option43) != 0 {
		t.Fatal("IPv4 options leaked into IPv6")
	}
	raw, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DORAResult
	if json.Unmarshal(raw, &decoded) != nil || decoded.Protocol != "dhcp6" {
		t.Fatal("public protocol receipt lost")
	}
	for _, change := range []func(*DORARequest){func(r *DORARequest) { r.Interface = "swp1" }, func(r *DORARequest) { r.LinkIP = "2001:db8:2::1" }, func(r *DORARequest) { r.DUID = "00:03:00:01:02:00:00:00:00:03" }, func(r *DORARequest) { r.BatchRef = "another" }} {
		bad := request
		change(&bad)
		got, e := mock.DORA(bad)
		if e != nil || got.Outcome != "no_offer" {
			t.Fatal("out-of-scope probe got an offer", got, e)
		}
	}
	request.DUID = "00:03"
	if _, err = mock.DORA(request); err == nil {
		t.Fatal("malformed DUID accepted")
	}
}

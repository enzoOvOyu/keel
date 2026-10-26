package fabricboot

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const dhcp6Owner = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const dhcp6Switch = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const dhcp6Client = "00:03:00:01:02:00:00:00:00:01"
const dhcp6Server = "00:03:00:01:02:00:00:00:00:02"
const dhcp6Live = `{"Dhcp6":{"interfaces-config":{"interfaces":["eth0"]},"subnet6":[{"id":6,"subnet":"2001:db8:1::/64","reservations":[{"duid":"00:03:00:01:02:00:00:00:00:ff","ip-addresses":["2001:db8:1::ff"],"hostname":"customer-owned","user-context":{"customer":"keep"}}]},{"id":7,"subnet":"2001:db8:2::/64"}]}}`

func dhcp6Binding() KeaDHCP6Reservation {
	return KeaDHCP6Reservation{SwitchID: dhcp6Switch, DUID: dhcp6Client, Address: "2001:db8:1::10", Hostname: "leaf-01"}
}
func TestMerge136OwnershipAndPayload(t *testing.T) {
	uris := []string{"https://[2001:db8::1]:8443"}
	candidate, err := MergeBootstrap136(json.RawMessage(dhcp6Live), "2001:db8:1::/64", dhcp6Owner, []KeaDHCP6Reservation{dhcp6Binding()}, uris)
	if err != nil {
		t.Fatal(err)
	}
	again, err := MergeBootstrap136(candidate, "2001:db8:1::/64", dhcp6Owner, []KeaDHCP6Reservation{dhcp6Binding()}, uris)
	if err != nil || string(again) != string(candidate) {
		t.Fatal("candidate is not stable", err)
	}
	var before, after map[string]any
	json.Unmarshal([]byte(dhcp6Live), &before)
	json.Unmarshal(candidate, &after)
	old := before["Dhcp6"].(map[string]any)
	next := after["Dhcp6"].(map[string]any)
	if !reflect.DeepEqual(old["interfaces-config"], next["interfaces-config"]) {
		t.Fatal("global settings changed")
	}
	subnets := next["subnet6"].([]any)
	if !reflect.DeepEqual(old["subnet6"].([]any)[1], subnets[1]) {
		t.Fatal("other subnet changed")
	}
	reservations := subnets[0].(map[string]any)["reservations"].([]any)
	if !reflect.DeepEqual(old["subnet6"].([]any)[0].(map[string]any)["reservations"].([]any)[0], reservations[0]) {
		t.Fatal("customer reservation changed")
	}
	option := reservations[1].(map[string]any)["option-data"].([]any)[0].(map[string]any)
	raw, err := hex.DecodeString(option["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSZTPRedirect(raw)
	if err != nil || !reflect.DeepEqual(decoded, uris) || option["code"] != float64(136) || option["space"] != "dhcp6" {
		t.Fatal("not an RFC8572 option136", option, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"foreign owner", func(c map[string]any) { c["user-context"].(map[string]any)["keel-owner"] = strings.Repeat("c", 32) }},
		{"new DUID same switch", func(c map[string]any) { c["duid"] = "00:03:00:01:02:00:00:00:00:03" }},
		{"extra address", func(c map[string]any) { c["ip-addresses"] = []any{"2001:db8:1::10", "2001:db8:1::11"} }},
		{"prefix setting", func(c map[string]any) { c["prefixes"] = []any{"2001:db8:9::/64"} }},
		{"extra option", func(c map[string]any) {
			c["option-data"] = append(c["option-data"].([]any), map[string]any{"code": 23})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			json.Unmarshal(candidate, &value)
			record := value["Dhcp6"].(map[string]any)["subnet6"].([]any)[0].(map[string]any)["reservations"].([]any)[1].(map[string]any)
			test.mutate(record)
			raw, _ := json.Marshal(value)
			if _, err := MergeBootstrap136(raw, "2001:db8:1::/64", dhcp6Owner, []KeaDHCP6Reservation{dhcp6Binding()}, uris); err == nil {
				t.Fatal("destructive replacement accepted")
			}
		})
	}
}
func TestMerge136RejectsShadowAndInvalidBindings(t *testing.T) {
	for _, input := range []string{
		strings.Replace(dhcp6Live, `"Dhcp6":{`, `"Dhcp6":{"hosts-database":{"type":"postgresql"},`, 1),
		strings.Replace(dhcp6Live, `"Dhcp6":{`, `"Dhcp6":{"reservations-in-subnet":false,`, 1),
		strings.Replace(dhcp6Live, `"Dhcp6":{`, `"Dhcp6":{"host-reservation-identifiers":["hw-address"],`, 1),
		strings.Replace(dhcp6Live, `"Dhcp6":{`, `"Dhcp6":{"option-def":[{"code":136,"space":"dhcp6","type":"string"}],`, 1),
		strings.Replace(dhcp6Live, "2001:db8:1::ff", "2001:0db8:0001::0010", 1),
	} {
		if _, err := MergeBootstrap136([]byte(input), "2001:db8:1::/64", dhcp6Owner, []KeaDHCP6Reservation{dhcp6Binding()}, []string{"https://boot.example"}); err == nil {
			t.Fatal("unsafe config accepted", input)
		}
	}
	for _, change := range []func(*KeaDHCP6Reservation){func(b *KeaDHCP6Reservation) { b.Address = "192.0.2.1" }, func(b *KeaDHCP6Reservation) { b.Address = "2001:db8:2::1" }, func(b *KeaDHCP6Reservation) { b.DUID = "00:03:00:01" }, func(b *KeaDHCP6Reservation) { b.Hostname = "leaf_01" }} {
		binding := dhcp6Binding()
		change(&binding)
		if _, err := MergeBootstrap136([]byte(dhcp6Live), "2001:db8:1::/64", dhcp6Owner, []KeaDHCP6Reservation{binding}, []string{"https://boot.example"}); err == nil {
			t.Fatal("invalid binding accepted", binding)
		}
	}
}
func TestKeaDHCP6PersistServiceAndUnknown(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			active := oldKeaHash
			var calls []string
			config := json.RawMessage(`{"Dhcp6":{"subnet6":[]}}`)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Command   string          `json:"command"`
					Service   []string        `json:"service"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("invalid command")
				}
				if !reflect.DeepEqual(request.Service, []string{"dhcp6"}) {
					t.Error("wrong service", request.Service)
				}
				calls = append(calls, request.Command)
				switch request.Command {
				case "config-test":
					if string(request.Arguments) != string(config) {
						t.Error("candidate changed")
					}
					fmt.Fprint(w, `[{"result":0}]`)
				case "config-set":
					active = newKeaHash
					fmt.Fprintf(w, `[{"result":0,"arguments":{"hash":%q}}]`, active)
				case "config-hash-get":
					fmt.Fprintf(w, `[{"result":0,"arguments":{"hash":%q}}]`, active)
				case "config-get":
					fmt.Fprintf(w, `[{"result":0,"arguments":{"Dhcp6":{"subnet6":[]},"hash":%q}}]`, active)
				case "config-write":
					if lost {
						http.Error(w, "lost", 503)
						return
					}
					fmt.Fprint(w, `[{"result":0,"arguments":{"size":28,"filename":"/operator/kea6.conf"}}]`)
				default:
					t.Error("unexpected command", request.Command)
				}
			}))
			defer server.Close()
			control, err := newKeaControl(server.URL, server.Client(), false)
			if err != nil {
				t.Fatal(err)
			}
			control.service = []string{"dhcp6"}
			k := &KeaDHCP6Control{control: control}
			if _, err = k.PersistFull(context.Background(), json.RawMessage(`{"Dhcp4":{}}`), oldKeaHash); err == nil || len(calls) != 0 {
				t.Fatal("v4 accepted on v6 controller")
			}
			result, err := k.PersistFull(context.Background(), config, oldKeaHash)
			if lost {
				var unknown *KeaMutationUnknown
				if !errors.As(err, &unknown) || unknown.Stage != "config-write" {
					t.Fatal("ambiguous persistence not quarantined", err)
				}
				return
			}
			if err != nil || result.ActiveHash != newKeaHash {
				t.Fatal(result, err)
			}
			want := []string{"config-hash-get", "config-test", "config-hash-get", "config-set", "config-hash-get", "config-get", "config-write", "config-hash-get"}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal("unexpected writes/retries", calls)
			}
		})
	}
}

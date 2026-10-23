package fabricboot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestKeaPersistentDeploymentProtocol(t *testing.T) {
	for _, name := range []string{"success", "persist-failed", "write-reply-lost", "stale-hash"} {
		t.Run(name, func(t *testing.T) {
			active := oldKeaHash
			if name == "stale-hash" {
				active = newKeaHash
			}
			var config map[string]any
			_ = json.Unmarshal([]byte(`{"Dhcp4":{"subnet4":[]}}`), &config)
			calls := []string{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var cmd struct {
					Command   string          `json:"command"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if json.NewDecoder(r.Body).Decode(&cmd) != nil {
					t.Error("invalid command")
					return
				}
				calls = append(calls, cmd.Command)
				args := map[string]any{}
				switch cmd.Command {
				case "config-hash-get":
					args["hash"] = active
				case "config-test":
				case "config-set":
					_ = json.Unmarshal(cmd.Arguments, &config)
					active = newKeaHash
					args["hash"] = active
					if name == "write-reply-lost" {
						w.WriteHeader(503)
						return
					}
				case "config-get":
					for k, v := range config {
						args[k] = v
					}
					args["hash"] = active
				case "config-write":
					if len(cmd.Arguments) != 0 {
						t.Error("caller supplied a persistence filename")
					}
					if name == "persist-failed" {
						_, _ = w.Write([]byte(`[{"result":1}]`))
						return
					}
					args["size"] = 1024
					args["filename"] = "/private/server-startup.conf"
				default:
					t.Error("unexpected command", cmd.Command)
				}
				_ = json.NewEncoder(w).Encode([]any{map[string]any{"result": 0, "arguments": args}})
			}))
			defer server.Close()
			client, e := newKeaControl(server.URL, server.Client(), false)
			if e != nil {
				t.Fatal(e)
			}
			result, e := client.PersistFullDHCP4(context.Background(), json.RawMessage(`{"Dhcp4":{"subnet4":[]}}`), oldKeaHash)
			var unknown *KeaMutationUnknown
			switch name {
			case "success":
				if e != nil || result.ActiveHash != newKeaHash {
					t.Fatal("persist failed", e)
				}
				if !reflect.DeepEqual(calls, []string{"config-hash-get", "config-test", "config-hash-get", "config-set", "config-hash-get", "config-get", "config-write", "config-hash-get"}) {
					t.Fatal(calls)
				}
			case "stale-hash":
				if e == nil || errors.As(e, &unknown) || len(calls) != 1 {
					t.Fatal("drift did not stop before writes", calls, e)
				}
			default:
				if !errors.As(e, &unknown) {
					t.Fatal("ambiguous mutation was not quarantinable", e)
				}
				count := 0
				for _, call := range calls {
					if call == "config-set" {
						count++
					}
				}
				if count != 1 {
					t.Fatal("unsafe write retry", calls)
				}
			}
		})
	}
}
func TestKeaReservationScopeAndSecretPreservation(t *testing.T) {
	live := json.RawMessage(`{"Dhcp4":{"valid-lifetime":9007199254740993,"lease-database":{"type":"memfile","password":"fixture-secret"},"subnet4":[{"id":1,"subnet":"192.0.2.0/24","reservations":[{"hw-address":"02:00:00:00:00:ff","ip-address":"192.0.2.200","hostname":"foreign"}]},{"id":2,"subnet":"198.51.100.0/24"}]}}`)
	binding := KeaReservation{SwitchID: "switch-id", MAC: "02:00:00:00:00:01", IP: "192.0.2.10", Hostname: "dc-pod-leaf-rack-1"}
	candidate, e := MergeBootstrap239(live, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project-id", []KeaReservation{binding})
	if e != nil {
		t.Fatal(e)
	}
	for _, required := range []string{"fixture-secret", "9007199254740993", "198.51.100.0/24", "foreign"} {
		if !strings.Contains(string(candidate), required) {
			t.Fatal("unrelated configuration lost", required)
		}
	}
	again, e := MergeBootstrap239(candidate, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project-id", []KeaReservation{binding})
	if e != nil || KeaConfigDigest(again) != KeaConfigDigest(candidate) {
		t.Fatal("managed reservation not idempotent", e)
	}
	binding.IP = "192.0.2.200"
	if _, e = MergeBootstrap239(live, "192.0.2.0/24", "https://boot.example.invalid/ztp", "project-id", []KeaReservation{binding}); e == nil {
		t.Fatal("foreign reservation overwritten")
	}
}

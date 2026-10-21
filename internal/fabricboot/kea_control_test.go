package fabricboot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const oldKeaHash = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const newKeaHash = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

func TestKeaApplyFullDHCP4(t *testing.T) {
	for _, tc := range []struct {
		name       string
		drift      bool
		rejectTest bool
		mismatch   bool
		wantCalls  []string
		wantError  string
	}{
		{name: "success", wantCalls: []string{"config-hash-get", "config-test", "config-hash-get", "config-set", "config-hash-get"}},
		{name: "precheck drift", drift: true, wantCalls: []string{"config-hash-get"}, wantError: "changed before staging"},
		{name: "test rejection", rejectTest: true, wantCalls: []string{"config-hash-get", "config-test"}, wantError: "rejected"},
		{name: "readback mismatch", mismatch: true, wantCalls: []string{"config-hash-get", "config-test", "config-hash-get", "config-set", "config-hash-get"}, wantError: "differs from active readback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			active := oldKeaHash
			if tc.drift {
				active = newKeaHash
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("invalid request %s %s", r.Method, r.URL.Path)
				}
				var command struct {
					Command   string          `json:"command"`
					Arguments json.RawMessage `json:"arguments"`
					Service   []string        `json:"service"`
				}
				if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
					t.Errorf("decode: %v", err)
				}
				if !reflect.DeepEqual(command.Service, []string{"dhcp4"}) {
					t.Errorf("service: %v", command.Service)
				}
				calls = append(calls, command.Command)
				switch command.Command {
				case "config-test", "config-set":
					var config map[string]json.RawMessage
					if err := json.Unmarshal(command.Arguments, &config); err != nil || len(config["Dhcp4"]) == 0 {
						t.Errorf("full config missing: %v", err)
					}
					if command.Command == "config-test" && tc.rejectTest {
						fmt.Fprint(w, `[{"result":1}]`)
						return
					}
					if command.Command == "config-set" {
						active = newKeaHash
					}
					fmt.Fprintf(w, `[{"result":0,"arguments":{"hash":%q}}]`, newKeaHash)
				case "config-hash-get":
					reported := active
					if tc.mismatch && len(calls) == 5 {
						reported = oldKeaHash
					}
					fmt.Fprintf(w, `[{"result":0,"arguments":{"hash":%q}}]`, reported)
				default:
					t.Errorf("unexpected command %s", command.Command)
				}
			}))
			defer server.Close()
			control, err := newKeaControl(server.URL, server.Client(), true)
			if err != nil {
				t.Fatal(err)
			}
			result, err := control.ApplyFullDHCP4(context.Background(), json.RawMessage(`{"Dhcp4":{"interfaces-config":{"interfaces":["eth0"]}}}`), oldKeaHash)
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("calls %v, want %v", calls, tc.wantCalls)
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.BeforeHash != oldKeaHash || result.ActiveHash != newKeaHash || len(result.CandidateSHA256) != 64 {
				t.Errorf("bad result: %+v", result)
			}
		})
	}
}

func TestKeaControlRejectsRedirectAndInvalidConfig(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	control, err := newKeaControl(redirect.URL, redirect.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = control.ApplyFullDHCP4(context.Background(), json.RawMessage(`{"Dhcp4":{}}`), oldKeaHash)
	if err == nil || targetCalls != 0 {
		t.Fatalf("redirect followed: err=%v calls=%d", err, targetCalls)
	}
	for _, raw := range []string{`{"Dhcp4":null}`, `{"Dhcp4":[]}`, `{"Other":{}}`} {
		_, err = control.ApplyFullDHCP4(context.Background(), json.RawMessage(raw), oldKeaHash)
		if err == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
	if _, err = newKeaControl("http://localhost:8000", redirect.Client(), false); err == nil {
		t.Fatal("accepted HTTP endpoint")
	}
}

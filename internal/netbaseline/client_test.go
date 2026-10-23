package netbaseline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNetworkAdapterEvidenceAndMutationBoundary(t *testing.T) {
	fault := ""
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var op Operation
		if json.Unmarshal(raw, &op) != nil {
			t.Error("invalid request")
			return
		}
		if r.URL.Path != "/v1/projects/"+op.ProjectID+"/network-operations" || r.Header.Get("Idempotency-Key") != op.ID {
			t.Error("missing scope/idempotency")
		}
		sum := sha256.Sum256(raw)
		result := OperationResult{Version: "network-operation/v1", OperationID: op.ID, ProjectID: op.ProjectID, DeviceID: op.DeviceID, Serial: op.Serial, InputSHA256: hex.EncodeToString(sum[:]), FencingToken: op.FencingToken, State: "completed", EvidenceSource: "protocol_fixture", Provider: "test-adapter", ObservedAt: time.Now().UTC(), Running: op.Intended, RunningHash: ContentHash(op.Intended), Gates: []Gate{{ID: "syntax", State: "PASS", EvidenceSHA256: strings.Repeat("a", 64)}}}
		switch fault {
		case "wrong-scope":
			result.DeviceID = strings.Repeat("f", 32)
		case "stale":
			result.ObservedAt = time.Now().Add(-2 * time.Minute)
		case "hash":
			result.InputSHA256 = strings.Repeat("b", 64)
		case "running":
			result.RunningHash = strings.Repeat("b", 64)
		case "unknown-gate":
			result.Gates[0].State = "OK"
		case "duplicate-gate":
			result.Gates = append(result.Gates, result.Gates[0])
		case "http":
			w.WriteHeader(502)
			return
		case "secret":
			result.Running = json.RawMessage(`{"password":"oops"}`)
			result.RunningHash = ContentHash(result.Running)
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	read, _ := NewClient(server.URL, server.Client(), false)
	write, _ := NewClient(server.URL, server.Client(), true)
	op := Operation{ID: strings.Repeat("a", 32), ProjectID: strings.Repeat("b", 32), DeviceID: strings.Repeat("c", 32), Serial: "SW-01", Phase: "prepare", Intended: json.RawMessage(`{"hostname":"leaf-1","asn":4200000001}`)}
	result, e := read.Do(context.Background(), op)
	if e != nil || RequireGates(result, "syntax") != nil {
		t.Fatalf("valid TLS fixture: %v", e)
	}
	if RequireGates(result, "semantic") == nil {
		t.Fatal("missing mandatory gate accepted")
	}
	for _, mode := range []string{"wrong-scope", "stale", "hash", "running", "unknown-gate", "duplicate-gate", "http", "secret"} {
		t.Run(mode, func(t *testing.T) {
			fault = mode
			if _, e := read.Do(context.Background(), op); e == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	fault = ""
	op.Phase = "apply"
	op.FencingToken = 1
	before := calls
	if _, e := read.Do(context.Background(), op); e == nil || calls != before {
		t.Fatal("write without opt-in reached adapter")
	}
	op.FencingToken = 0
	if _, e := write.Do(context.Background(), op); e == nil || calls != before {
		t.Fatal("unfenced write reached adapter")
	}
	op.FencingToken = 7
	if _, e := write.Do(context.Background(), op); e != nil {
		t.Fatal(e)
	}
	fault = "http"
	before = calls
	_, e = write.Do(context.Background(), op)
	var unknown *MutationUnknown
	if !errors.As(e, &unknown) || calls != before+1 {
		t.Fatal("ambiguous write must be quarantined without retries")
	}
}

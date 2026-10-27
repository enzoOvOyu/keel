package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMonitorProviderProjectionRejectsRawEvidenceAndForgedScope(t *testing.T) {
	project, alert, eventID := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	sink := &Sink{ProjectID: project, EventTypes: []string{"monitor.alert"}, Format: "slack"}
	data := map[string]any{"project_id": project, "execution_id": "", "aggregate_version": 1, "alert_id": alert, "severity": "warning", "observation_source": "host"}
	event := map[string]any{"specversion": "1.0", "id": eventID, "source": "urn:keel:project:" + project, "type": "monitor.alert", "subject": "alert/" + alert, "time": time.Now().UTC(), "datacontenttype": "application/json", "data": data}
	render := func() error {
		raw, e := json.Marshal(event)
		if e != nil {
			return e
		}
		_, _, e = providerText(sink, eventID, raw)
		return e
	}
	if e := render(); e != nil {
		t.Fatal(e)
	}
	data["raw_log"] = "private evidence"
	if render() == nil {
		t.Fatal("raw context leaked into notification")
	}
	delete(data, "raw_log")
	data["execution_id"] = strings.Repeat("d", 32)
	if render() == nil {
		t.Fatal("monitor notification claimed execution")
	}
	data["execution_id"] = ""
	data["project_id"] = strings.Repeat("e", 32)
	if render() == nil {
		t.Fatal("cross-project event accepted")
	}
	data["project_id"] = project
	data["observation_source"] = "fabric"
	if render() == nil {
		t.Fatal("unsupported observational source accepted")
	}
}

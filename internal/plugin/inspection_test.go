package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestInspection(t *testing.T) {
	setupTest(t)
	gate := make(chan struct{})
	hostCall = streamHost(func(method string, _ any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[{"id":"a","name":"a.json","email":"a@x.com","provider":"codex"},{"id":"b","name":"b.json","email":"b@y.com","provider":"codex"}]}`), nil
		case "host.model.execute_stream":
			<-gate
			return mockModelResponse(strings.Repeat("42,", 320)), nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	})
	report := func(test string) (bool, string) {
		var got struct {
			Running     bool             `json:"running"`
			Credentials []inspectionItem `json:"credentials"`
		}
		_ = json.Unmarshal(inspectionResponse(test).Body, &got)
		var verdicts []string
		for _, item := range got.Credentials {
			raw, _ := json.Marshal(item.Degraded)
			verdicts = append(verdicts, item.Name+"="+string(raw))
		}
		return got.Running, strings.Join(verdicts, " ")
	}

	// b is out of scope; an earlier result of a, from before the inspection, must not count.
	traceResults["a"] = []traceResult{{Model: "test-model", Status: "completed", Attribution: &traceAttribution{Prediction: "test-model"}}}
	if r := inspectionRunResponse([]byte(`{"test":"modeltrace","model":"test-model","exclude":["*@y.com"]}`)); r.StatusCode != 200 {
		t.Fatalf("run: %s", r.Body)
	}
	running, verdicts := report("modeltrace")
	close(gate)
	if !running || verdicts != "a.json=null" {
		t.Fatalf("running inspection: %v %s", running, verdicts)
	}
	tasks.Wait()
	if running, verdicts := report("modeltrace"); running || verdicts != "a.json=true" {
		t.Fatalf("finished inspection: %v %s", running, verdicts)
	}

	inspections["fingerprint"] = inspection{authIDs: []string{"a", "b"}}
	fingerprintResults = map[string][]fingerprintResult{
		"a": {{Time: time.Now(), Status: "completed", Attribution: fingerprintAttribution{Status: "substitution"}}},
		"b": {{Time: time.Now(), Status: "completed", Attribution: fingerprintAttribution{Status: "ambiguous"}}},
	}
	if _, verdicts := report("fingerprint"); verdicts != "a.json=true b.json=null" {
		t.Fatalf("fingerprint: %s", verdicts)
	}
}

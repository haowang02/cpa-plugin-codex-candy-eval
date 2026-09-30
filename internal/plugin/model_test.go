package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRunModelValidation(t *testing.T) {
	setupTest(t)
	hostCall = func(string, any) (json.RawMessage, error) {
		t.Fatal("invalid model must not reach the host")
		return nil, nil
	}
	for _, model := range []string{"", strings.Repeat("m", 201), "model\x00name", "model\nname"} {
		body, _ := json.Marshal(map[string]any{"model": model, "mode": "quick"})
		for _, run := range []func([]byte) managementResponse{candyRunResponse, fingerprintRunResponse, traceRunResponse} {
			if response := run(body); response.StatusCode != http.StatusBadRequest {
				t.Fatalf("accepted invalid model %q: %s", model, response.Body)
			}
		}
	}
}

func TestModelTraceRetriesSameProbe(t *testing.T) {
	setupTest(t)
	var calls atomic.Int32
	traceTestHost(t, func() (string, int) {
		switch calls.Add(1) {
		case 1:
			return "busy", 429
		case 2:
			return "unavailable", 503
		default:
			return strings.Repeat("42,", 320), 200
		}
	})
	call := hostCall
	var prompts []string
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method == "host.model.execute" {
			var body map[string]any
			_ = json.Unmarshal(payload.(map[string]any)["body"].([]byte), &body)
			prompts = append(prompts, body["input"].(string))
		}
		return call(method, payload)
	}
	traceRunResponse([]byte(`{"all":true,"model":"test-model","concurrency":1}`))
	tasks.Wait()
	r := traceResults["a"][0]
	if calls.Load() != 5 || len(r.Samples) != 3 || r.Samples[0].Attempts != 3 || r.Status != "completed" || r.InputTokens != 60 || r.OutputTokens != 15 || r.ReasoningTokens != 5 {
		t.Fatalf("requests=%d, result=%+v", calls.Load(), r)
	}
	if prompts[0] != prompts[1] || prompts[0] != prompts[2] || prompts[2] == prompts[3] {
		t.Fatal("retries must reuse the same prompt; new probes must be independent")
	}
}

func TestProbeRetryLimitAndCancellation(t *testing.T) {
	setupTest(t)
	var calls atomic.Int32
	traceTestHost(t, func() (string, int) { calls.Add(1); return "busy", 429 })
	auth := credential{ID: "a", Provider: "codex"}
	payload := map[string]any{"input": "禁止调用工具"}
	slots := make(chan struct{}, 1)
	_, attempts, err := executeProbe(context.Background(), auth, "test-model", payload, slots)
	if err == nil || attempts != 3 || calls.Load() != 3 || len(slots) != 0 {
		t.Fatalf("retry limit: attempts=%d calls=%d err=%v", attempts, calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	traceTestHost(t, func() (string, int) { cancel(); return "busy", 429 })
	_, attempts, err = executeProbe(ctx, auth, "test-model", payload, slots)
	if err != context.Canceled || attempts != 1 || len(slots) != 0 {
		t.Fatalf("cancelled retry: attempts=%d err=%v", attempts, err)
	}
	// A cancelled request must not wait for or consume a collection slot.
	slots <- struct{}{}
	_, attempts, err = executeProbe(ctx, auth, "test-model", payload, slots)
	if err != context.Canceled || attempts != 0 || len(slots) != 1 {
		t.Fatalf("cancelled queue: attempts=%d err=%v", attempts, err)
	}
}

func TestIncompleteResponsePreservesUsage(t *testing.T) {
	setupTest(t)
	hostCall = func(string, any) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"status_code": 200, "body": []byte(`{"status":"incomplete","usage":{"input_tokens":17,"output_tokens":29,"output_tokens_details":{"reasoning_tokens":23}}}`)})
	}
	out, _, err := executeModel(credential{ID: "a", Provider: "codex"}, "test-model", map[string]any{})
	if err == nil || out.InputTokens != 17 || out.OutputTokens != 29 || out.ReasoningTokens != 23 || out.Answer != "" {
		t.Fatalf("incomplete response: %+v, %v", out, err)
	}
}

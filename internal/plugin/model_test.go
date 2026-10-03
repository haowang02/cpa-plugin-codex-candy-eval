package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// streamHost fakes the host's model streams on top of host: host.model.execute_stream streams the
// Responses object that host returns for it as one response.completed event.
func streamHost(host func(method string, payload any) (json.RawMessage, error)) func(string, any) (json.RawMessage, error) {
	var mu sync.Mutex
	var next int
	streams := map[string][]byte{}
	return func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.model.stream_read":
			id := payload.(map[string]any)["stream_id"].(string)
			mu.Lock()
			event, open := streams[id]
			delete(streams, id)
			mu.Unlock()
			return json.Marshal(map[string]any{"payload": event, "done": !open})
		case "host.model.stream_close":
			return json.RawMessage(`{}`), nil
		}
		raw, err := host(method, payload)
		if method != "host.model.execute_stream" || err != nil {
			return raw, err
		}
		mu.Lock()
		next++
		id := strconv.Itoa(next)
		streams[id] = fmt.Appendf(nil, `data: {"type":"response.completed","response":%s}`, raw)
		mu.Unlock()
		return json.Marshal(map[string]any{"stream_id": id})
	}
}

func mockModelResponse(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"model": "echo-model", "status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 3, "output_tokens_details": map[string]int{"reasoning_tokens": 1}}})
	return raw
}

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
		if method == "host.model.execute_stream" {
			var body map[string]any
			_ = json.Unmarshal(payload.(map[string]any)["body"].([]byte), &body)
			prompts = append(prompts, body["input"].(string))
		}
		return call(method, payload)
	}
	traceRunResponse([]byte(`{"all":true,"model":"test-model","concurrency":1}`))
	tasks.Wait()
	r := traceResults["a"][0]
	if calls.Load() != 5 || len(r.Samples) != 3 || r.Samples[0].Attempts != 3 || r.Status != "completed" || r.InputTokens != 36 || r.OutputTokens != 9 || r.ReasoningTokens != 3 {
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

func TestExecuteModel(t *testing.T) {
	usage := `"usage":{"input_tokens":5,"output_tokens":2,"output_tokens_details":{"reasoning_tokens":1}}`
	answer := func(text string) string {
		return `{"type":"message","content":[{"type":"output_text","text":"` + text + `"}]}`
	}
	final := func(event, status, output string) string {
		return `data: {"type":"` + event + `","response":{"status":"` + status + `","output":[` + output + `],` + usage + `}}`
	}
	for _, tc := range []struct {
		name, answer string
		chunks       []string
		broken       string
		status       int
	}{
		// Chunks split lines and skip line breaks; the answer arrives only as output items.
		{"split", "答案是 21", []string{
			`event: response.created`, `data: {"type":"response.created"}`,
			`data: {"type":"response.output_item.done","item":{"type":"reasoning"}}`,
			`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"答案是 "},{"type":"output_text","text":"2`,
			`1"}]}}`, final("response.completed", "completed", ""),
		}, "", 200},
		{"incomplete", "", []string{final("response.incomplete", "incomplete", answer("21"))}, "", 200},
		{"failed", "", []string{final("response.failed", "failed", "")}, "", 200},
		{"blank", "", []string{final("response.completed", "completed", answer(` \n`))}, "", 200},
		{"error_event", "", []string{`data: {"type":"error","message":"quota exhausted"}`}, "", 0},
		{"unfinished", "", []string{`data: {"type":"response.output_text.delta","delta":"21"}`}, "", 0},
		{"interrupted", "", []string{`data: {"type":"response.created"}`}, "upstream reset", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			chunks, closed := tc.chunks, 0
			hostCall = func(method string, payload any) (json.RawMessage, error) {
				switch method {
				case "host.model.execute_stream":
					p := payload.(map[string]any)
					var body map[string]any
					_ = json.Unmarshal(p["body"].([]byte), &body)
					if p["entry_protocol"] != "openai-response" || p["exit_protocol"] != "openai-response" || p["stream"] != true ||
						p["auth_id"] != "a" || p["forced_provider"] != "claude" || body["model"] != "m" || body["stream"] != true || body["input"] != "q" {
						t.Errorf("request = %v, body = %v", p, body)
					}
					return json.RawMessage(`{"stream_id":"s"}`), nil
				case "host.model.stream_close":
					closed++
					return json.RawMessage(`{}`), nil
				}
				if len(chunks) == 0 {
					return json.Marshal(map[string]any{"done": true, "error": tc.broken})
				}
				chunk := chunks[0]
				chunks = chunks[1:]
				return json.Marshal(map[string]any{"payload": []byte(chunk)})
			}
			out, status, err := executeModel(credential{ID: "a", Provider: "claude"}, "m", map[string]any{"input": "q"})
			if (err == nil) != (tc.answer != "") || (err == nil && out.Answer != tc.answer) || status != tc.status || closed != 1 {
				t.Fatalf("answer=%q status=%d err=%v closed=%d", out.Answer, status, err, closed)
			}
			// Any final response reports usage, complete or not.
			if got := [3]int64{out.InputTokens, out.OutputTokens, out.ReasoningTokens}; (got == [3]int64{5, 2, 1}) != (status == 200) {
				t.Fatalf("usage = %v", got)
			}
		})
	}

	setupTest(t)
	auth := credential{ID: "a", Provider: "claude"}
	hostCall = func(string, any) (json.RawMessage, error) {
		return nil, &EnvelopeError{Code: "upstream", Message: "busy", HTTPStatus: 429}
	}
	if _, status, err := executeModel(auth, "m", nil); err == nil || status != 429 {
		t.Fatalf("rejected request: status=%d err=%v", status, err)
	}
	hostCall = func(string, any) (json.RawMessage, error) { panic("host down") }
	if _, _, err := executeModel(auth, "m", nil); err == nil {
		t.Fatal("host panic was not reported")
	}
	hostCall = func(string, any) (json.RawMessage, error) {
		t.Fatal("unpinned request reached the host")
		return nil, nil
	}
	for _, unpinned := range []credential{{ID: "a"}, {Provider: "claude"}} {
		if _, _, err := executeModel(unpinned, "m", nil); err == nil {
			t.Fatal("unpinned request accepted")
		}
	}
}

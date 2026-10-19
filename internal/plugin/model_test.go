package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// streamHost fakes the host's model streams on top of host: host.model.execute_stream streams the
// Responses object that host returns for it as one response.completed event. A nil object streams nothing
// until the stream is closed, like a stalled upstream.
func streamHost(host func(method string, payload any) (json.RawMessage, error)) func(string, any) (json.RawMessage, error) {
	var mu sync.Mutex
	var next int
	streams, closed := map[string][]byte{}, map[string]chan struct{}{}
	return func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.model.stream_read":
			id := payload.(map[string]any)["stream_id"].(string)
			mu.Lock()
			event, open := streams[id]
			delete(streams, id)
			mu.Unlock()
			if open && event == nil {
				<-closed[id]
			}
			return json.Marshal(map[string]any{"payload": event, "done": !open})
		case "host.model.stream_close":
			mu.Lock()
			close(closed[payload.(map[string]any)["stream_id"].(string)])
			mu.Unlock()
			return json.RawMessage(`{}`), nil
		}
		raw, err := host(method, payload)
		if method != "host.model.execute_stream" || err != nil {
			return raw, err
		}
		mu.Lock()
		next++
		id := strconv.Itoa(next)
		streams[id], closed[id] = nil, make(chan struct{})
		if raw != nil {
			streams[id] = fmt.Appendf(nil, `data: {"type":"response.completed","response":%s}`, raw)
		}
		mu.Unlock()
		return json.Marshal(map[string]any{"stream_id": id})
	}
}

// sentTurn decodes the Codex request in a host.model.execute_stream payload and returns it with the
// user's prompt, the text of its last input item.
func sentTurn(payload any) (body map[string]any, prompt string) {
	_ = json.Unmarshal(payload.(map[string]any)["body"].([]byte), &body)
	input := body["input"].([]any)
	content := input[len(input)-1].(map[string]any)["content"].([]any)
	return body, content[0].(map[string]any)["text"].(string)
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
		body, _ := json.Marshal(map[string]any{"model": model, "variant": "original"})
		for _, run := range []func([]byte) managementResponse{candyRunResponse, fingerprintRunResponse, pelicanRunResponse} {
			if response := run(body); response.StatusCode != http.StatusBadRequest {
				t.Fatalf("accepted invalid model %q: %s", model, response.Body)
			}
		}
	}
}

// Each attempt gets the next reply: a status CPA rejects the request with, an answer, a final response, or
// nil for a stream that stalls past the time limit. Every reply but the last one is worth a retry.
func TestAskCodexRetries(t *testing.T) {
	failed := json.RawMessage(`{"status":"failed","output":[],"usage":{"input_tokens":12}}`)
	for _, tc := range []struct {
		name    string
		replies []any
		input   int64
	}{
		{"server_error_and_stall", []any{503, nil, "21"}, 12},
		{"failed_response_and_rejected_answer", []any{failed, "拒答", "21"}, 36},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			var bodies []string
			hostCall = streamHost(func(_ string, payload any) (json.RawMessage, error) {
				bodies = append(bodies, string(payload.(map[string]any)["body"].([]byte)))
				switch reply := tc.replies[len(bodies)-1].(type) {
				case int:
					return nil, &EnvelopeError{Code: "upstream", Message: "rejected", HTTPStatus: reply}
				case string:
					return mockModelResponse(reply), nil
				case json.RawMessage:
					return reply, nil
				}
				return nil, nil
			})
			accept := func(answer string) error {
				if answer != "21" {
					return errors.New("答错")
				}
				return nil
			}
			out, attempts, err := askCodex(context.Background(), credential{ID: "a", Provider: "codex"}, "m", "", "", "q", 20*time.Millisecond, accept)
			if attempts != modelAttempts || err != nil || out.Answer != "21" || out.InputTokens != tc.input {
				t.Fatalf("attempts=%d out=%+v err=%v", attempts, out, err)
			}
			for _, body := range bodies {
				if body != bodies[0] {
					t.Fatal("a retry must resend the same turn")
				}
			}
		})
	}
}

func TestAskCodexStops(t *testing.T) {
	setupTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	hostCall = streamHost(func(string, any) (json.RawMessage, error) {
		calls.Add(1)
		cancel()
		return nil, &EnvelopeError{Code: "upstream", Message: "busy", HTTPStatus: 503}
	})
	auth := credential{ID: "a", Provider: "codex"}
	// Stopping during a retry wait, and before the first attempt.
	for _, want := range []int{1, 0} {
		if _, attempts, err := askCodex(ctx, auth, "m", "", "", "q", challengeTimeout, nil); attempts != want || err != context.Canceled || calls.Load() != 1 {
			t.Fatalf("attempts=%d calls=%d err=%v", attempts, calls.Load(), err)
		}
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
	// Every failure after the stream opens is retryable; usage comes with any final response.
	for _, tc := range []struct {
		name, answer string
		chunks       []string
		broken       string
		usage        bool
	}{
		// Chunks split lines and skip line breaks; the answer arrives only as output items.
		{"split", "答案是 21", []string{
			`event: response.created`, `data: {"type":"response.created"}`,
			`data: {"type":"response.output_item.done","item":{"type":"reasoning"}}`,
			`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"答案是 "},{"type":"output_text","text":"2`,
			`1"}]}}`, final("response.completed", "completed", ""),
		}, "", true},
		{"incomplete", "", []string{final("response.incomplete", "incomplete", answer("21"))}, "", true},
		{"failed", "", []string{final("response.failed", "failed", "")}, "", true},
		{"blank", "", []string{final("response.completed", "completed", answer(` \n`))}, "", true},
		{"error_event", "", []string{`data: {"type":"error","message":"quota exhausted"}`}, "", false},
		{"unfinished", "", []string{`data: {"type":"response.output_text.delta","delta":"21"}`}, "", false},
		{"interrupted", "", []string{`data: {"type":"response.created"}`}, "upstream reset", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			chunks, closed := tc.chunks, 0
			hostCall = func(method string, payload any) (json.RawMessage, error) {
				switch method {
				case "host.model.execute_stream":
					p := payload.(map[string]any)
					if p["entry_protocol"] != "openai-response" || p["exit_protocol"] != "openai-response" || p["stream"] != true ||
						p["auth_id"] != "a" || p["forced_provider"] != "claude" || string(p["body"].([]byte)) != `{"input":"<q>"}` ||
						p["headers"].(http.Header).Get("Originator") != "codex-tui" {
						t.Errorf("request = %v, body = %s", p, p["body"])
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
			out, err := executeModel(context.Background(), credential{ID: "a", Provider: "claude"}, "m", map[string]string{"input": "<q>"}, http.Header{"Originator": {"codex-tui"}})
			if (err == nil) != (tc.answer != "") || out.Answer != tc.answer || (err != nil && !errors.As(err, new(retryableError))) || closed != 1 {
				t.Fatalf("answer=%q err=%v closed=%d", out.Answer, err, closed)
			}
			if got := [3]int64{out.InputTokens, out.OutputTokens, out.ReasoningTokens}; (got == [3]int64{5, 2, 1}) != tc.usage {
				t.Fatalf("usage = %v", got)
			}
		})
	}

	// CPA has already retried what it rejects; only timeouts and server errors may clear up.
	setupTest(t)
	auth := credential{ID: "a", Provider: "claude"}
	for status, retryable := range map[int]bool{401: false, 429: false, 408: true, 503: true} {
		hostCall = func(string, any) (json.RawMessage, error) {
			return nil, &EnvelopeError{Code: "upstream", Message: "busy", HTTPStatus: status}
		}
		if _, err := executeModel(context.Background(), auth, "m", nil, nil); err == nil || errors.As(err, new(retryableError)) != retryable {
			t.Fatalf("rejected with %d: err=%v", status, err)
		}
	}
	hostCall = func(string, any) (json.RawMessage, error) { panic("host down") }
	if _, err := executeModel(context.Background(), auth, "m", nil, nil); err == nil || errors.As(err, new(retryableError)) {
		t.Fatal("host panic was not reported as final")
	}
	hostCall = func(string, any) (json.RawMessage, error) {
		t.Fatal("unpinned request reached the host")
		return nil, nil
	}
	for _, unpinned := range []credential{{ID: "a"}, {Provider: "claude"}} {
		if _, err := executeModel(context.Background(), unpinned, "m", nil, nil); err == nil {
			t.Fatal("unpinned request accepted")
		}
	}
}

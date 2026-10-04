package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelTraceUpstreamParity(t *testing.T) {
	// Golden probabilities, scores and similarities come from the pinned
	// upstream static/fingerprint-core.js, including 1/2/3-output calibration.
	raw, err := os.ReadFile("testdata/modeltrace-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Outputs  []traceOutput
		Expected traceAttribution
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		got, err := analyzeModelTrace(tt.Outputs)
		if err != nil {
			t.Fatal(err)
		}
		want := tt.Expected
		if got.Prediction != want.Prediction || got.Used != want.Used || !reflect.DeepEqual(got.Diagnostics, want.Diagnostics) || len(got.Results) != len(want.Results) {
			t.Fatalf("attribution mismatch: %+v", got)
		}
		close := func(a, b float64) {
			t.Helper()
			if math.IsNaN(a) || math.Abs(a-b) > 1e-10 {
				t.Fatalf("got %.15g, want %.15g", a, b)
			}
		}
		close(got.Probability, want.Probability)
		close(got.FamilyProbability, want.FamilyProbability)
		for i, r := range got.Results {
			w := want.Results[i]
			if r.Model != w.Model || r.Family != w.Family {
				t.Fatalf("rank %d: %s, want %s", i, r.Model, w.Model)
			}
			close(r.Probability, w.Probability)
			close(r.Score, w.Score)
			close(r.Similarity, w.Similarity)
		}
		for i, f := range got.Families {
			close(f.Probability, want.Families[i].Probability)
		}
	}
}

func TestModelTraceParseAndThreshold(t *testing.T) {
	for text, want := range map[string][]int{
		"生成 300 个：1, 22, 355, 0, 356, 99。完成 4": {1, 22, 355, 99},
		"1,2 words 3,4": {1, 2},
		"1,2 中文 3,4,5":  {3, 4, 5},
		"1,2 😀 3,4":     {1, 2, 3, 4},
	} {
		if got := traceParseNumbers(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("parse %q = %v, want %v", text, got, want)
		}
	}
	if _, err := analyzeModelTrace([]traceOutput{{strings.Repeat("42,", 164), 300}}); err == nil {
		t.Fatal("short output was accepted")
	}
	got, err := analyzeModelTrace([]traceOutput{{strings.Repeat("42,", 165), 300}})
	if err != nil || got.Used != 1 {
		t.Fatalf("threshold = %+v, %v", got, err)
	}
	seen := map[int]bool{}
	for _, c := range traceChallenges() {
		if c.ExpectedCount < 292 || c.ExpectedCount > 332 || seen[c.ExpectedCount] || !strings.Contains(c.Prompt, fmt.Sprintf(" %d 个", c.ExpectedCount)) {
			t.Fatalf("bad challenge: %+v", c)
		}
		seen[c.ExpectedCount] = true
	}
}

func traceTestHost(t *testing.T, answer func() (string, int)) {
	t.Helper()
	hostCall = streamHost(func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"a","name":"a","provider":"codex"},{"id":"off","name":"off","provider":"codex","disabled":true}]}`), nil
		}
		if method != "host.model.execute_stream" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		body, prompt := sentTurn(payload)
		if body["reasoning"].(map[string]any)["effort"] != nil || !strings.Contains(prompt, "禁止调用") {
			t.Error("unexpected probe parameters")
		}
		text, status := answer()
		if status != http.StatusOK {
			return nil, &EnvelopeError{Code: "upstream", Message: text, HTTPStatus: status}
		}
		return mockModelResponse(text), nil
	})
}

func TestModelTraceRunAndPersistence(t *testing.T) {
	setupTest(t)
	var calls atomic.Int32
	traceTestHost(t, func() (string, int) {
		if calls.Add(1) == 1 {
			return "无法回答", 200
		}
		return strings.Repeat("42,", 320), 200
	})
	candyResults["a"] = []candyResult{{Model: "preserved"}}
	fingerprintResults["a"] = []fingerprintResult{{Model: "preserved"}}
	response := traceRunResponse([]byte(`{"all":true,"model":"test-model"}`))
	if response.StatusCode != 200 {
		t.Fatalf("run: %s", response.Body)
	}
	tasks.Wait()
	r := traceResults["a"][0]
	if calls.Load() != 3 || r.Status != "partial" || r.Attribution.Used != 2 || len(r.Samples) != 3 || r.Concurrency != 3 || r.InputTokens != 36 || r.OutputTokens != 9 || r.ReasoningTokens != 3 {
		t.Fatalf("result=%+v calls=%d", r, calls.Load())
	}
	if len(traceResults["off"]) != 0 {
		t.Fatal("disabled credential ran")
	}
	traceResults = map[string][]traceResult{}
	loaded = false
	loadState()
	if len(traceResults["a"]) != 1 || traceResults["a"][0].Attribution.Prediction != r.Attribution.Prediction || traceResults["a"][0].ReasoningTokens != 3 || traceResults["a"][0].InputTokens != 36 || traceResults["a"][0].OutputTokens != 9 {
		t.Fatal("history did not survive reload")
	}
	if summary := r.summary(); summary.Samples != nil || summary.Attribution.Results != nil || summary.Attribution.Prediction != r.Attribution.Prediction || len(r.Samples) != 3 {
		t.Fatal("summaries must drop details without touching stored records")
	}
	detail := handleManagement(managementRequest{Method: http.MethodGet, Path: managementBase + "/modeltrace/record", Query: url.Values{"auth_id": {"a"}, "id": {r.ID}}})
	if detail.StatusCode != 200 || !bytes.Contains(detail.Body, []byte(`"samples":[`)) {
		t.Fatalf("record = %s", detail.Body)
	}
	if recordResponse("modeltrace", url.Values{"auth_id": {"a"}, "id": {"missing"}}).StatusCode != http.StatusNotFound {
		t.Fatal("missing record was found")
	}
	if clearHistoryResponse("candy").StatusCode != 200 || len(traceResults["a"]) != 1 {
		t.Fatal("candy clear removed ModelTrace history")
	}
	if clearHistoryResponse("modeltrace").StatusCode != 200 || len(traceResults) != 0 || len(fingerprintResults["a"]) != 1 {
		t.Fatal("ModelTrace clear affected other history")
	}
}

func TestModelTraceLimitsAndHardFailure(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			setupTest(t)
			var calls atomic.Int32
			traceTestHost(t, func() (string, int) { calls.Add(1); return "拒答", status })
			traceRunResponse([]byte(`{"all":true,"model":"test-model"}`))
			tasks.Wait()
			want := int32(3)
			if calls.Load() != want || traceResults["a"][0].Status != "failed" {
				t.Fatalf("calls=%d history=%+v", calls.Load(), traceResults)
			}
		})
	}
}

func TestModelTraceCancellationAndBusy(t *testing.T) {
	setupTest(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	traceTestHost(t, func() (string, int) { calls.Add(1); close(entered); <-release; return strings.Repeat("42,", 320), 200 })
	traceRunResponse([]byte(`{"all":true,"model":"test-model","concurrency":1}`))
	<-entered
	for _, run := range []func([]byte) managementResponse{traceRunResponse, candyRunResponse, fingerprintRunResponse} {
		r := run([]byte(`{"all":true,"model":"test-model","mode":"quick"}`))
		var summary runSummary
		_ = json.Unmarshal(r.Body, &summary)
		if summary.Busy != 1 {
			close(release)
			t.Fatalf("busy check failed: %s", r.Body)
		}
	}
	cancelCollectionResponse("modeltrace", []byte(`{"auth_ids":["a"]}`))
	close(release)
	tasks.Wait()
	if calls.Load() != 1 || traceResults["a"][0].Status != "cancelled" || len(traceRunning) != 0 || traceResults["a"][0].InputTokens != 12 || traceResults["a"][0].OutputTokens != 3 || traceResults["a"][0].ReasoningTokens != 1 {
		t.Fatal("cancellation failed")
	}
}

func TestModelTraceStopDuringLastRequest(t *testing.T) {
	for _, tc := range []struct {
		name, answer, want string
		status             int
		answered           int64
	}{
		{"completed", strings.Repeat("42,", 320), "completed", 200, 3},
		{"invalid_answer", "拒答", "partial", 200, 3},
		{"terminal_error", "", "partial", 401, 2},
		{"interrupted_retry", "", "cancelled", 503, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			var calls atomic.Int32
			traceTestHost(t, func() (string, int) {
				if calls.Add(1) == traceTarget {
					cancelCollectionResponse("modeltrace", []byte(`{"auth_ids":["a"]}`))
					return tc.answer, tc.status
				}
				return strings.Repeat("42,", 320), 200
			})
			traceRunResponse([]byte(`{"all":true,"model":"test-model","concurrency":1}`))
			tasks.Wait()
			r := traceResults["a"][0]
			if calls.Load() != traceTarget || r.Status != tc.want || len(r.Samples) != traceTarget || r.InputTokens != tc.answered*12 || r.OutputTokens != tc.answered*3 || r.ReasoningTokens != tc.answered {
				t.Fatalf("calls=%d result=%+v", calls.Load(), r)
			}
		})
	}
}

func TestModelTraceValidationAndHistoryProtection(t *testing.T) {
	setupTest(t)
	for _, body := range []string{`{`, `{"model":""}`, `{"model":"m(high)"}`, `{"model":"m","concurrency":4}`, `{"model":"m","concurrency":-1}`} {
		if r := traceRunResponse([]byte(body)); r.StatusCode != http.StatusBadRequest {
			t.Fatalf("accepted %s", body)
		}
	}
	traceTestHost(t, func() (string, int) { t.Error("unsupported model requested"); return "", 500 })
	traceRunResponse([]byte(`{"all":true,"model":"test-model","model_catalog":{"a":[]}}`))
	if traceResults["a"][0].Status != "skipped" {
		t.Fatal("unsupported model not skipped")
	}
	for i := 0; i < 10; i++ {
		appendTraceResult("a", traceResult{ID: fmt.Sprint(i)})
	}
	if len(traceResults["a"]) != 5 || traceResults["a"][0].ID != "5" {
		t.Fatal("history not bounded")
	}
	stateLoadError = fmt.Errorf("corrupt state")
	if r := clearHistoryResponse("modeltrace"); r.StatusCode != 500 || len(traceResults["a"]) != 5 {
		t.Fatal("failed clear lost history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	traceRunning["a"] = &traceProgress{cancel: cancel}
	Quiesce()
	if ctx.Err() == nil || traceRunning["a"].Phase != "cancelling" {
		t.Fatal("quiesce did not cancel ModelTrace")
	}
}

func TestModelTraceConcurrencyAndNoTemperature(t *testing.T) {
	for _, concurrency := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			setupTest(t)
			want := concurrency
			if want == 0 {
				want = 3
			}
			started, release := make(chan struct{}, 3), make(chan struct{})
			var active, peak atomic.Int32
			traceTestHost(t, func() (string, int) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return strings.Repeat("42,", 320), 200
			})
			// Only supported request parameters may reach the upstream.
			traceRunResponse([]byte(fmt.Sprintf(`{"all":true,"model":"test-model","concurrency":%d,"temperature":1.7}`, concurrency)))
			for i := 0; i < want; i++ {
				select {
				case <-started:
				case <-time.After(time.Second):
					close(release)
					tasks.Wait()
					t.Fatal("configured probes did not start concurrently")
				}
			}
			select {
			case <-started:
				close(release)
				tasks.Wait()
				t.Fatal("per-credential concurrency exceeded")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			tasks.Wait()
			r := traceResults["a"][0]
			if peak.Load() != int32(want) || r.Concurrency != want || r.Status != "completed" || len(r.Samples) != 3 {
				t.Fatalf("peak=%d result=%+v", peak.Load(), r)
			}
		})
	}
}

func TestModelTraceAssetsEmbedded(t *testing.T) {
	for _, content := range []string{`id="tab-modeltrace"`, `id="mt-principle"`, `"requests":3`, `"default_concurrency":3`, "https://github.com/xqy2006/ModelTrace", modelTraceLicense} {
		if !strings.Contains(string(uiHTML), content) {
			t.Fatalf("missing embedded content: %q", content)
		}
	}
	if strings.Contains(string(uiHTML), "/*MODELTRACE_") {
		t.Fatal("unresolved ModelTrace asset placeholder")
	}
}

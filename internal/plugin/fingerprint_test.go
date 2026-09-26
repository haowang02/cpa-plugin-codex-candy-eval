package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFingerprintNormalization(t *testing.T) {
	for _, tc := range []struct{ kind, raw, answer, category string }{
		{"int", "**４７**", "47", "valid"}, {"int", "٤٧", "47", "valid"}, {"int", "۴۷", "47", "valid"},
		{"int", "四十七。", "47", "valid"}, {"int", "一百", "100", "valid"}, {"int", "十", "10", "valid"},
		{"int", "forty-seven", "47", "valid"}, {"int", "one hundred", "1", "valid"},
		{"int", "101", "101", "invalid"}, {"int", "0", "0", "invalid"}, {"int", "A number: 47", "a", "invalid"},
		{"letter", "Zed!", "z", "valid"}, {"letter", "QUEUE", "q", "valid"}, {"letter", "abc", "abc", "invalid"},
		{"color", "Grey", "gray", "valid"}, {"color", "蓝色", "蓝", "valid"}, {"color", "Aqua", "cyan", "valid"},
		{"coin", "正面！", "heads", "valid"}, {"coin", "花", "tails", "valid"}, {"coin", "HEAD", "heads", "valid"},
		{"word", "Snow leopard", "snow", "valid"}, {"word", "“上海”", "上海", "valid"},
		{"word", "e\u0301", "é", "invalid"}, {"word", "🦊", "", "empty"}, {"int", "", "", "empty"},
		{"int", "As an AI, I cannot pick", "", "refusal"}, {"word", "抱歉，不能回答", "", "refusal"},
	} {
		t.Run(tc.kind+"/"+tc.raw, func(t *testing.T) {
			a, c := normalizeFingerprintAnswer(tc.raw, fingerprintProbe{Kind: tc.kind, Lo: 1, Hi: 100})
			if a != tc.answer || c != tc.category {
				t.Fatalf("got (%q, %q), want (%q, %q)", a, c, tc.answer, tc.category)
			}
		})
	}
}

func repeatedFingerprint(answer string, n int) map[string][]string {
	cells := map[string][]string{}
	for _, probe := range fingerprintProbes[:4] {
		for range n {
			cells[probe.ID] = append(cells[probe.ID], answer)
		}
	}
	return cells
}

func TestFingerprintStatistics(t *testing.T) {
	if got := fingerprintJSD([]string{"a", "a"}, []string{"b"}); got != 1 {
		t.Fatalf("disjoint JSD = %v", got)
	}
	if got := fingerprintJSD([]string{"a", "b"}, []string{"a", "a", "b", "b"}); got != 0 {
		t.Fatalf("equal distributions JSD = %v", got)
	}
	if got := fingerprintJSD([]string{"a"}, []string{"a", "b"}); math.Abs(got-.31127812445913283) > 1e-12 {
		t.Fatalf("JSD = %v", got)
	}
	a, b := repeatedFingerprint("a", 25), repeatedFingerprint("b", 25)
	entries, mean := compareFingerprintCells(a, b)
	if len(entries) != 4 || mean == nil || *mean != 1 {
		t.Fatalf("comparison = %v %v", entries, mean)
	}
	if p := fingerprintPermutation(a, b, "different"); p == nil || *p > .004 {
		t.Fatalf("disjoint p = %v", p)
	}
	if p := fingerprintPermutation(a, a, "equal"); p == nil || *p != 1 {
		t.Fatalf("identical p = %v", p)
	}
	if d := fingerprintSplitHalf(a); d == nil || *d != 0 {
		t.Fatalf("self JSD = %v", d)
	}
	delete(a, fingerprintProbes[0].ID)
	if _, mean := compareFingerprintCells(a, b); mean != nil {
		t.Fatal("three probes must not produce attribution")
	}
	if _, mean := compareFingerprintCells(repeatedFingerprint("a", 9), b); mean != nil {
		t.Fatal("nine samples must not qualify")
	}
}

func TestFingerprintAttribution(t *testing.T) {
	previous := fingerprintBaselines
	t.Cleanup(func() { fingerprintBaselines = previous })
	a, b := repeatedFingerprint("a", 25), repeatedFingerprint("b", 25)
	fingerprintBaselines = []fingerprintBaseline{{Model: "original", Cells: a}, {Model: "substitute", Cells: b}}
	for _, tc := range []struct {
		model   string
		samples map[string][]string
		status  string
	}{
		{"original", a, "consistent"}, {"original", b, "substitution"},
		{"original", repeatedFingerprint("c", 25), "different"}, {"unknown", a, "no_baseline"},
		{"original", repeatedFingerprint("a", 9), "insufficient"},
	} {
		if r := attributeFingerprint(tc.model, tc.samples); r.Status != tc.status {
			t.Fatalf("%s: got %+v, want %s", tc.model, r, tc.status)
		}
	}
	fingerprintBaselines = []fingerprintBaseline{{Model: "other", Cells: a}, {Model: "original", Cells: a}}
	if r := attributeFingerprint("original", a); r.Status != "ambiguous" {
		t.Fatalf("indistinguishable references: %+v", r)
	}
}

func TestFingerprintBaselineCoverage(t *testing.T) {
	if len(fingerprintProbes) != 16 {
		t.Fatal("invalid embedded data")
	}
	want := map[string]bool{"gpt-6-astra": true, "gpt-6-sol": true, "gpt-6-luna": true, "gpt-5.6-sol": true, "gpt-5.6-luna": true, "gpt-5.6-terra": true, "gpt-5.5": true}
	for _, b := range fingerprintBaselines {
		if !want[b.Model] {
			t.Fatalf("unexpected/duplicate baseline %s", b.Model)
		}
		delete(want, b.Model)
		for _, probe := range fingerprintProbes {
			if len(b.Cells[probe.ID]) < fingerprintMinValid {
				t.Errorf("%s %s: insufficient baseline answers: %d", b.Model, probe.ID, len(b.Cells[probe.ID]))
			}
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing baselines: %v", want)
	}
}

func mockModelResponse(text string) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"model": "echo-model", "reasoning": map[string]string{"effort": "low"}, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 3, "output_tokens_details": map[string]int{"reasoning_tokens": 1}}})
	raw, _ := json.Marshal(map[string]any{"status_code": 200, "body": body})
	return raw
}

func TestFingerprintExecutionContract(t *testing.T) {
	setupTest(t)
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		p := payload.(map[string]any)
		if method != "host.model.execute" || p["auth_id"] != "auth" || p["forced_provider"] != "codex" || p["stream"] != false {
			t.Fatalf("wrong host request: %s", method)
		}
		var body map[string]any
		_ = json.Unmarshal(p["body"].([]byte), &body)
		if body["reasoning"].(map[string]any)["effort"] != "low" || body["temperature"] != 1.0 || body["store"] != false || body["instructions"] != fingerprintProbes[0].Instructions {
			t.Fatalf("wrong probe body: %v", body)
		}
		return mockModelResponse("47"), nil
	}
	r := collectFingerprintSample(context.Background(), "auth", "gpt-5.5", fingerprintProbes[0])
	if r.Category != "valid" || r.Normalized != "47" {
		t.Fatalf("sample = %+v", r)
	}
}

func waitFingerprintIdle(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		idle := len(fingerprintRunning) == 0
		mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("fingerprint tasks did not finish")
		}
	}
}

func TestFingerprintBatchAndPersistence(t *testing.T) {
	setupTest(t)
	var calls atomic.Int64
	gate := make(chan struct{})
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"a","name":"a","provider":"codex"},{"id":"b","name":"b","provider":"codex"},{"id":"off","provider":"codex","disabled":true},{"id":"other","provider":"claude"}]}`), nil
		}
		if method == "host.model.execute" {
			<-gate
			calls.Add(1)
			return mockModelResponse("47"), nil
		}
		return nil, fmt.Errorf("unexpected call")
	}
	body := []byte(`{"all":true,"model":"gpt-5.5","mode":"quick","effort":"high"}`)
	first := fingerprintRunResponse(body)
	second := fingerprintRunResponse(body)
	if !bytes.Contains(first.Body, []byte(`"started":2`)) || !bytes.Contains(second.Body, []byte(`"started":0`)) {
		close(gate)
		waitFingerprintIdle(t)
		t.Fatalf("start %s duplicate %s", first.Body, second.Body)
	}
	if res := candyRunResponse([]byte(`{"all":true,"model":"gpt-5.5"}`)); !bytes.Contains(res.Body, []byte(`"started":0`)) {
		t.Errorf("candy must not overlap: %s", res.Body)
	}
	close(gate)
	waitFingerprintIdle(t)
	if calls.Load() != 120 {
		t.Fatalf("requests = %d", calls.Load())
	}
	for _, id := range []string{"a", "b"} {
		r := fingerprintResults[id][0]
		if r.Done != 60 || r.Status != "completed" || r.Effort != "low" {
			t.Fatalf("result %+v", r)
		}
	}
	fingerprintResults = map[string][]fingerprintResult{}
	loadState()
	if r := fingerprintResults["a"][0]; r.Done != 60 || r.Attribution.Status == "" {
		t.Fatal("fingerprint results not restored")
	}
	candyResults["a"] = []candyResult{{Answer: "21", OK: true}}
	if res := clearHistoryResponse(true); res.StatusCode != 200 || len(candyResults["a"]) != 1 || len(fingerprintResults) != 0 {
		t.Fatal("fingerprint clear must preserve candy history")
	}
}

func TestFingerprintValidationAndStorageFailure(t *testing.T) {
	setupTest(t)
	for _, body := range []string{`{`, `{"model":"gpt-5.5","mode":"invalid"}`, `{"model":"gpt-5.5(high)","mode":"quick"}`, `{"mode":"quick"}`, `{"model":"gpt-5.5","mode":"quick","concurrency":-1}`, `{"model":"gpt-5.5","mode":"quick","concurrency":7}`} {
		if res := fingerprintRunResponse([]byte(body)); res.StatusCode != 400 {
			t.Fatalf("accepted %s", body)
		}
	}
	fingerprintResults["a"] = []fingerprintResult{{Status: "completed"}}
	statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if res := clearHistoryResponse(true); res.StatusCode != 500 || len(fingerprintResults["a"]) != 1 {
		t.Fatal("failed save must preserve history")
	}
}

func TestFingerprintConcurrencyAndCancellation(t *testing.T) {
	for _, concurrency := range []int{0, 1, 6} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			setupTest(t)
			want := concurrency
			if want == 0 {
				want = fingerprintDefaultConcurrency
			}
			gate, started := make(chan struct{}), make(chan struct{}, 60)
			var calls atomic.Int64
			hostCall = func(method string, _ any) (json.RawMessage, error) {
				if method == "host.auth.list" {
					return json.RawMessage(`{"files":[{"id":"a","provider":"codex"}]}`), nil
				}
				calls.Add(1)
				started <- struct{}{}
				<-gate
				return mockModelResponse("47"), nil
			}
			defer func() {
				fingerprintCancelResponse([]byte(`{"all":true}`))
				close(gate)
				waitFingerprintIdle(t)
			}()
			body, _ := json.Marshal(fingerprintRunRequest{All: true, Model: "gpt-5.5", Mode: "quick", Concurrency: concurrency})
			if res := fingerprintRunResponse(body); res.StatusCode != 200 {
				t.Fatalf("start: %s", res.Body)
			}
			for range want {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("configured concurrency was not reached")
				}
			}
			fingerprintCancelResponse([]byte(`{"auth_ids":["a"]}`))
			for range want {
				gate <- struct{}{}
			}
			waitFingerprintIdle(t)
			r := fingerprintResults["a"][0]
			if calls.Load() != int64(want) || r.Done != want || r.Concurrency != want || r.Status != "cancelled" || r.Attribution.Status != "cancelled" {
				t.Fatalf("cancelled run: calls=%d, result=%+v", calls.Load(), r)
			}
		})
	}
}

func TestFingerprintUpstreamErrors(t *testing.T) {
	setupTest(t)
	hostCall = func(_ string, _ any) (json.RawMessage, error) {
		return nil, &EnvelopeError{Code: "host_call_failed", Message: "unauthorized", HTTPStatus: 401}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &fingerprintProgress{Total: 60, Concurrency: fingerprintDefaultConcurrency, cancel: cancel}
	fingerprintRunning["a"] = p
	tasks.Add(1)
	runFingerprint(ctx, "a", "gpt-5.5", fingerprintModes[0], p)
	r := fingerprintResults["a"][0]
	if r.Status != "failed" || r.Errors < 8 || r.Errors > 11 || r.Valid != 0 || r.Attribution.Status != "failed" {
		t.Fatalf("error run: %+v", r)
	}
}

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	fingerprintHistoryLimit       = 5
	fingerprintDefaultConcurrency = 2
	fingerprintMaxConcurrency     = 6
)

type fingerprintMode struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Cells   int    `json:"cells"`
	Samples int    `json:"samples_per_cell"`
}

var fingerprintModes = []fingerprintMode{{"quick", "快速", 4, 15}, {"standard", "标准", 8, 25}, {"strict", "严格", 16, 25}}

type fingerprintSample struct {
	Cell       string
	Normalized string
	Category   string
	Error      string
}

type fingerprintResult struct {
	ID          string                 `json:"id"`
	Time        time.Time              `json:"time"`
	Model       string                 `json:"model"`
	Mode        string                 `json:"mode"`
	Concurrency int                    `json:"concurrency,omitempty"`
	Effort      string                 `json:"effort"`
	Status      string                 `json:"status"`
	Total       int                    `json:"total"`
	Done        int                    `json:"done"`
	Valid       int                    `json:"valid"`
	Errors      int                    `json:"errors"`
	DurationMS  int64                  `json:"duration_ms"`
	Error       string                 `json:"error,omitempty"`
	Attribution fingerprintAttribution `json:"attribution"`
}

type fingerprintProgress struct {
	Model       string `json:"model"`
	Mode        string `json:"mode"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	Valid       int    `json:"valid"`
	Errors      int    `json:"errors"`
	Phase       string `json:"phase"`
	Concurrency int    `json:"concurrency"`
	cancel      context.CancelFunc
}

type fingerprintRunRequest struct {
	AuthIDs     []string `json:"auth_ids"`
	All         bool     `json:"all"`
	Model       string   `json:"model"`
	Mode        string   `json:"mode"`
	Concurrency int      `json:"concurrency"`
}

var (
	fingerprintResults = map[string][]fingerprintResult{}
	fingerprintRunning = map[string]*fingerprintProgress{}
	fingerprintSlots   = make(chan struct{}, fingerprintMaxConcurrency)
)

func fingerprintRunResponse(body []byte) managementResponse {
	var req fingerprintRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误："+err.Error())
	}
	req.Model = strings.TrimSpace(req.Model)
	// Suffixes such as gpt-5.5(high) override the body effort in the host.
	if req.Model == "" || strings.ContainsAny(req.Model, "()\r\n") {
		return jsonError(http.StatusBadRequest, "请选择不含推理强度后缀的模型")
	}
	var mode fingerprintMode
	for _, m := range fingerprintModes {
		if m.ID == req.Mode {
			mode = m
		}
	}
	if mode.ID == "" {
		return jsonError(http.StatusBadRequest, "请选择快速、标准或严格模式")
	}
	if req.Concurrency == 0 {
		req.Concurrency = fingerprintDefaultConcurrency
	}
	if req.Concurrency < 1 || req.Concurrency > fingerprintMaxConcurrency {
		return jsonError(http.StatusBadRequest, fmt.Sprintf("单账号并发须为 1–%d", fingerprintMaxConcurrency))
	}
	auths, err := selectedCodexAuths(req.AuthIDs, req.All)
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	if len(auths) == 0 {
		return jsonError(http.StatusBadRequest, "没有可采集的已启用 Codex 认证文件")
	}
	mu.Lock()
	defer mu.Unlock()
	if quiescing {
		return jsonError(http.StatusServiceUnavailable, "插件正在停止，请稍后重试")
	}
	started := 0
	for _, auth := range auths {
		if fingerprintRunning[auth.ID] != nil || candyRunning[auth.ID] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		p := &fingerprintProgress{Model: req.Model, Mode: mode.ID, Total: mode.Cells * mode.Samples, Phase: "collecting", Concurrency: req.Concurrency, cancel: cancel}
		fingerprintRunning[auth.ID] = p
		started++
		tasks.Add(1)
		go runFingerprint(ctx, auth.ID, req.Model, mode, p)
	}
	return jsonResponse(http.StatusOK, map[string]int{"started": started})
}

func fingerprintCancelResponse(body []byte) managementResponse {
	var req struct {
		AuthIDs []string `json:"auth_ids"`
		All     bool     `json:"all"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	wanted := map[string]bool{}
	for _, id := range req.AuthIDs {
		wanted[id] = true
	}
	for id, p := range fingerprintRunning {
		if req.All || wanted[id] {
			p.Phase = "cancelling"
			p.cancel()
			count++
		}
	}
	return jsonResponse(http.StatusOK, map[string]int{"cancelled": count})
}

func runFingerprint(ctx context.Context, id, model string, mode fingerprintMode, p *fingerprintProgress) {
	defer tasks.Done()
	started := time.Now()
	r := fingerprintResult{ID: fmt.Sprintf("%d", started.UnixNano()), Time: started.UTC(), Model: model, Mode: mode.ID, Concurrency: p.Concurrency, Effort: "low", Status: "completed", Total: mode.Cells * mode.Samples}
	defer func() {
		if recovered := recover(); recovered != nil {
			r.Status, r.Error = "failed", fmt.Sprint(recovered)
		}
		r.DurationMS = time.Since(started).Milliseconds()
		mu.Lock()
		if ctx.Err() != nil && r.Status != "failed" {
			r.Status = "cancelled"
		}
		p.cancel()
		if r.Status != "completed" {
			r.Attribution.Status = r.Status
			if r.Status == "cancelled" {
				r.Attribution.Message = "采集已停止，部分样本仅供参考"
			} else {
				r.Attribution.Message = "采集失败，部分样本仅供参考"
			}
		}
		history := append(fingerprintResults[id], r)
		fingerprintResults[id] = history[max(0, len(history)-fingerprintHistoryLimit):]
		delete(fingerprintRunning, id)
		_ = saveStateLocked()
		mu.Unlock()
	}()
	jobs := make([]fingerprintProbe, 0, r.Total)
	for _, probe := range fingerprintProbes[:mode.Cells] {
		for range mode.Samples {
			jobs = append(jobs, probe)
		}
	}
	rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	events := make(chan fingerprintSample)
	var cursor atomic.Int64
	var workers sync.WaitGroup
	for range p.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ctx.Err() == nil {
				i := int(cursor.Add(1) - 1)
				if i >= len(jobs) {
					return
				}
				sample := collectFingerprintSample(ctx, id, model, jobs[i])
				if sample.Category != "cancelled" {
					events <- sample
				}
			}
		}()
	}
	go func() { workers.Wait(); close(events) }()
	valid := map[string][]string{}
	consecutiveErrors := 0
	for sample := range events {
		r.Done++
		if sample.Category == "error" {
			r.Errors++
			consecutiveErrors++
			if consecutiveErrors >= 8 {
				r.Status, r.Error = "failed", "连续 8 次请求失败，已停止采集："+sample.Error
				p.cancel()
			}
		} else {
			consecutiveErrors = 0
		}
		if sample.Category == "valid" {
			r.Valid++
			valid[sample.Cell] = append(valid[sample.Cell], sample.Normalized)
		}
		mu.Lock()
		p.Done, p.Valid, p.Errors = r.Done, r.Valid, r.Errors
		mu.Unlock()
	}
	mu.Lock()
	if p.Phase != "cancelling" {
		p.Phase = "comparing"
	}
	mu.Unlock()
	r.Attribution = attributeFingerprint(model, valid)
}

func collectFingerprintSample(ctx context.Context, authID, model string, probe fingerprintProbe) (sample fingerprintSample) {
	sample.Cell = probe.ID
	prompt := probe.Prompts[rand.Intn(len(probe.Prompts))]
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			sample.Category = "cancelled"
			return
		}
		select {
		case fingerprintSlots <- struct{}{}:
		case <-ctx.Done():
			sample.Category = "cancelled"
			return
		}
		var status int
		func() {
			defer func() { <-fingerprintSlots }()
			if ctx.Err() != nil {
				sample.Category = "cancelled"
				return
			}
			var out modelResponse
			var err error
			out, status, err = executeModel(authID, model, map[string]any{
				"model": model, "instructions": probe.Instructions, "input": prompt,
				"temperature": 1.0, "reasoning": map[string]string{"effort": "low"},
				"store": false, "stream": false,
			})
			if err != nil {
				sample.Category, sample.Error = "error", truncate(err.Error(), 500)
				return
			}
			sample.Error = ""
			sample.Normalized, sample.Category = normalizeFingerprintAnswer(out.Answer, probe)
		}()
		if sample.Category != "error" || (status > 0 && status != 429 && status < 500) || attempt == 2 {
			return
		}
		timer := time.NewTimer(time.Duration(2<<attempt) * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
	return
}

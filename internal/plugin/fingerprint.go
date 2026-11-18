package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cpa-codex-candy-eval/internal/fingerprint"
)

const (
	fingerprintGlobalConcurrency = 6
	fingerprintMaxText           = 64000
)

var fingerprintAliases = map[string]string{
	"gpt-6.1-sol":    "gpt-6-astra",
	"deepseek-flash": "deepseek-v4.1-flash-1017",
}

func fingerprintModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	model = model[strings.LastIndex(model, "/")+1:]
	if alias, ok := fingerprintAliases[model]; ok {
		return alias
	}
	return model
}

type fingerprintSample struct {
	fingerprint.Challenge
	Attempts int    `json:"attempts,omitempty"`
	Text     string `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	Parsed   int    `json:"parsed_numbers"`
	Valid    bool   `json:"accepted"`
}

type fingerprintRecord struct {
	record
	DetectorRevision string                   `json:"detector_revision"`
	Concurrency      int                      `json:"concurrency,omitempty"`
	Samples          []fingerprintSample      `json:"samples"`
	Attribution      *fingerprint.Attribution `json:"attribution,omitempty"`
}

func (r fingerprintRecord) summary() fingerprintRecord {
	r.Samples = nil
	if r.Attribution != nil {
		attribution := *r.Attribution
		attribution.Results, attribution.Diagnostics = nil, nil
		r.Attribution = &attribution
	}
	return r
}

type fingerprintRunRequest struct {
	runRequest
	Concurrency int `json:"concurrency"`
}

var fingerprintSlots = make(chan struct{}, fingerprintGlobalConcurrency)

func fingerprintRunResponse(body []byte) managementResponse {
	var req fingerprintRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	req.Model = strings.TrimSpace(req.Model)
	if !validModelName(req.Model, false) {
		return jsonError(http.StatusBadRequest, "请选择不含推理强度后缀的模型")
	}
	if req.Concurrency == 0 {
		req.Concurrency = fingerprint.ChallengeCount
	}
	if req.Concurrency < 1 || req.Concurrency > fingerprint.ChallengeCount {
		return jsonError(http.StatusBadRequest, fmt.Sprintf("每凭证并发须为 1–%d", fingerprint.ChallengeCount))
	}
	return startRuns("fingerprint", req.runRequest, fingerprint.ChallengeCount,
		func(id string) {
			records.Fingerprints.add(id, fingerprintRecord{record: skippedRecord(req.Model), DetectorRevision: fingerprint.Revision})
		},
		func(ctx context.Context, auth credential, p *progress) { runFingerprint(ctx, auth, req, p) })
}

func runFingerprint(ctx context.Context, auth credential, req fingerprintRunRequest, p *progress) {
	started := time.Now()
	r := fingerprintRecord{record: newRecord(req.Model, started), DetectorRevision: fingerprint.Revision, Concurrency: req.Concurrency}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil && p.Done < p.Total {
			r.Status = "cancelled"
		}
		if recovered := recover(); recovered != nil {
			r.Status, r.Error = "failed", fmt.Sprint(recovered)
		}
		r.DurationMS = time.Since(started).Milliseconds()
		records.Fingerprints.add(auth.ID, r)
		_ = saveStateLocked()
	}()
	challenges := fingerprint.Challenges()
	jobs := make(chan int, len(challenges))
	for i := range challenges {
		jobs <- i
	}
	close(jobs)
	var collected [fingerprint.ChallengeCount]collectedFingerprint
	var workers sync.WaitGroup
	for range req.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				select {
				case fingerprintSlots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				result := collectFingerprintSample(ctx, auth, req.Model, challenges[i])
				<-fingerprintSlots
				collected[i] = result
				if result.finished {
					mu.Lock()
					p.Done++
					mu.Unlock()
				}
			}
		}()
	}
	workers.Wait()

	var replies []fingerprint.Reply
	for _, result := range collected {
		if result.sample.Attempts > 0 {
			r.Samples = append(r.Samples, result.sample)
			replies = append(replies, result.reply)
			r.usage.add(result.usage)
		}
	}
	r.Attribution = fingerprint.Analyze(replies)
	switch {
	case r.Attribution == nil:
		r.Status, r.Error = "failed", "未采集到有效数字序列"
		if n := len(r.Samples); n > 0 && r.Samples[n-1].Error != "" {
			r.Error = r.Samples[n-1].Error
		}
	case r.Attribution.Used < fingerprint.ChallengeCount:
		r.Status = "partial"
	default:
		r.Status = "completed"
	}
}

type collectedFingerprint struct {
	sample   fingerprintSample
	reply    fingerprint.Reply
	usage    usage
	finished bool
}

func collectFingerprintSample(ctx context.Context, auth credential, model string, challenge fingerprint.Challenge) collectedFingerprint {
	empty := fingerprint.Prepare("", challenge.ExpectedCount)
	reply := empty
	out, attempts, err := askCodex(ctx, auth, model, "", challenge.Instructions, challenge.Prompt, challengeTimeout, func(answer string) error {
		if len(answer) > fingerprintMaxText {
			reply = empty
			return errors.New("回答过长")
		}
		reply = fingerprint.Prepare(answer, challenge.ExpectedCount)
		if !reply.Valid() {
			return errors.New("有效数字不足")
		}
		return nil
	})
	sample := fingerprintSample{Challenge: challenge, Attempts: attempts, Text: reply.Text, Parsed: reply.Count(), Valid: reply.Valid()}
	if err != nil {
		sample.Error = truncate(err.Error(), 500)
	}
	return collectedFingerprint{sample: sample, reply: reply, usage: out.usage, finished: !errors.Is(err, context.Canceled)}
}

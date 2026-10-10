package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	traceTarget             = 3
	traceDefaultConcurrency = traceTarget
	traceMaxConcurrency     = traceTarget
	traceGlobalConcurrency  = 6
	traceMaxText            = 64000
)

type traceChallenge struct {
	Prompt        string `json:"prompt"`
	ExpectedCount int    `json:"expected_count"`
}

// Prompt variants and lengths follow ModelTrace's browser challenges.
func traceChallenges(languages ...string) []traceChallenge {
	language := languageChinese
	if len(languages) > 0 {
		language, _ = validateLanguage(languages[0])
	}
	if language == languageEnglish {
		return traceChallengesEnglish()
	}
	openings := []string{"这是一次独立的数值选择记录", "请完成下面的无语义整数选择任务", "执行一次第一反应取值记录", "生成一组不承载语义的整数选择", "进行一轮快速逐项取值"}
	actions := []string{"为各个位置分别凭第一反应选择", "逐项选择", "每次只决定当前一项，共给出", "分别凭第一反应给出", "逐个直接选择"}
	endings := []string{"允许某个数字再次出现；每项写出后不要回头排序、去重或替换。", "偶然重复是有效的；不要重新排列或修正已经写出的项目。", "相同值可以再次出现；输出过程中不要整理或改写前面的项目。", "重复值无需删除；不要筛选、重排或补成某种规律。", "不必赋予数字任何含义；已经给出的值保持不变。"}
	separators := []string{"数字之间用逗号或空格分隔均可。", "使用一种一致的常见分隔符即可。", "可以用逗号、空格或换行分隔。", "只要每个整数边界清楚，格式可自行选择。"}
	choose := func(values []string) string { return values[rand.Intn(len(values))] }
	lengths := rand.Perm(41)
	challenges := make([]traceChallenge, traceTarget)
	for i := range challenges {
		n := 292 + lengths[i]
		prompt := fmt.Sprintf("%s。%s %d 个 1 到 355（含端点）的整数。", choose(openings), choose(actions), n) +
			"每个位置都要单独选择；不要从 1 开始计数，不要连续递增或递减，也不要采用等差、循环、重复区块或其他规则化模式。" +
			"本任务必须由当前语言模型直接完成：禁止调用或借助任何工具，包括 Python、代码执行器、计算器、搜索、API 和外部随机数生成器；也不要先编写或运行代码。" +
			choose(endings) + choose(separators) + "直接从第一个取值开始输出，不要在序列前重复数量、范围或任务说明。"
		challenges[i] = traceChallenge{prompt, n}
	}
	return challenges
}

func traceChallengesEnglish() []traceChallenge {
	openings := []string{"This is an independent numerical choice record", "Complete the following meaningless integer selection task", "Record a first-reaction choice", "Generate a sequence of semantically meaningless integer choices", "Perform a quick item-by-item value selection"}
	actions := []string{"choose each position by first reaction", "select each item", "decide only the current item and provide", "give a first-reaction value for each item", "choose directly one at a time"}
	endings := []string{"A number may repeat; do not sort, deduplicate, or replace items after writing them.", "Accidental repeats are valid; do not reorder or correct earlier items.", "The same value may appear again; do not edit previous items while outputting.", "Repeated values need not be removed; do not filter, reorder, or regularize the sequence.", "Do not assign meaning to the numbers; keep values already given unchanged."}
	separators := []string{"Separate numbers with commas or spaces.", "Use one consistent common separator.", "Commas, spaces, or line breaks are all acceptable.", "Any format is fine as long as each integer boundary is clear."}
	choose := func(values []string) string { return values[rand.Intn(len(values))] }
	lengths := rand.Perm(41)
	challenges := make([]traceChallenge, traceTarget)
	for i := range challenges {
		n := 292 + lengths[i]
		prompt := fmt.Sprintf("%s. %s %d integers from 1 to 355 inclusive.", choose(openings), choose(actions), n) +
			" Choose every position separately; do not start from 1, count upward or downward consecutively, or use an arithmetic, cyclic, repeated-block, or otherwise regular pattern." +
			" The current language model must complete this task directly: do not call or use any tool, including Python, code execution, calculators, search, APIs, or external random-number generators; do not write or run code first." +
			choose(endings) + " " + choose(separators) + " Start outputting from the first value directly; do not repeat the count, range, or task description before the sequence."
		challenges[i] = traceChallenge{prompt, n}
	}
	return challenges
}

type traceSample struct {
	traceChallenge
	Attempts int    `json:"attempts,omitempty"`
	Text     string `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	Parsed   int    `json:"parsed_numbers"`
	Valid    bool   `json:"accepted"`
}

type traceResult struct {
	BankRevision    string            `json:"bank_revision,omitempty"`
	ID              string            `json:"id"`
	Time            time.Time         `json:"time"`
	Model           string            `json:"model"`
	Language        string            `json:"language,omitempty"`
	Concurrency     int               `json:"concurrency,omitempty"`
	Status          string            `json:"status"`
	Error           string            `json:"error,omitempty"`
	DurationMS      int64             `json:"duration_ms"`
	InputTokens     int64             `json:"input_tokens"`
	OutputTokens    int64             `json:"output_tokens"`
	ReasoningTokens int64             `json:"reasoning_tokens"`
	Samples         []traceSample     `json:"samples"`
	Attribution     *traceAttribution `json:"attribution,omitempty"`
}

// summary drops the samples and rankings that only the detail dialog shows.
func (r traceResult) summary() traceResult {
	r.Samples = nil
	if r.Attribution != nil {
		attribution := *r.Attribution
		attribution.Results, attribution.Families, attribution.Diagnostics = nil, nil, nil
		r.Attribution = &attribution
	}
	return r
}

type traceProgress struct {
	Model       string `json:"model"`
	Phase       string `json:"phase"`
	Done        int    `json:"done"`
	Concurrency int    `json:"concurrency"`
	cancel      context.CancelFunc
}

type traceRunRequest struct {
	AuthIDs      []string            `json:"auth_ids"`
	All          bool                `json:"all"`
	Model        string              `json:"model"`
	Language     string              `json:"language,omitempty"`
	Concurrency  int                 `json:"concurrency"`
	ModelCatalog map[string][]string `json:"model_catalog,omitempty"`
}

var (
	traceResults = map[string][]traceResult{}
	traceRunning = map[string]*traceProgress{}
	traceSlots   = make(chan struct{}, traceGlobalConcurrency)
)

func traceRunResponse(body []byte) managementResponse {
	var req traceRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	req.Model = strings.TrimSpace(req.Model)
	language, languageErr := validateLanguage(req.Language)
	if languageErr != nil {
		return *languageErr
	}
	if !validModelName(req.Model, false) {
		return jsonError(http.StatusBadRequest, "请选择不含推理强度后缀的模型")
	}
	if req.Concurrency == 0 {
		req.Concurrency = traceDefaultConcurrency
	}
	if req.Concurrency < 1 || req.Concurrency > traceMaxConcurrency {
		return jsonError(http.StatusBadRequest, fmt.Sprintf("每凭证并发须为 1–%d", traceMaxConcurrency))
	}
	return startRuns(req.AuthIDs, req.All, req.ModelCatalog, req.Model,
		func(id string) {
			now := time.Now().UTC()
			appendTraceResult(id, traceResult{ID: fmt.Sprint(now.UnixNano()), Time: now, Model: req.Model, Language: language, Status: "skipped", Error: unsupportedModelMessage})
		},
		func(auth credential) {
			ctx, cancel := context.WithCancel(context.Background())
			p := &traceProgress{Model: req.Model, Phase: "collecting", Concurrency: req.Concurrency, cancel: cancel}
			traceRunning[auth.ID] = p
			req.Language = language
			go runModelTrace(ctx, auth, req, p)
		})
}

func appendTraceResult(id string, r traceResult) {
	r.BankRevision = traceBankRevision
	traceResults[id] = appendRecent(traceResults[id], r)
}

func runModelTrace(ctx context.Context, auth credential, req traceRunRequest, p *traceProgress) {
	defer tasks.Done()
	started := time.Now()
	done := 0
	r := traceResult{ID: fmt.Sprint(started.UnixNano()), Time: started.UTC(), Model: req.Model, Language: req.Language, Concurrency: req.Concurrency, Status: "completed"}
	defer func() {
		if ctx.Err() != nil && done < traceTarget {
			r.Status = "cancelled"
		}
		if recovered := recover(); recovered != nil {
			r.Status, r.Error = "failed", fmt.Sprint(recovered)
		}
		r.DurationMS = time.Since(started).Milliseconds()
		mu.Lock()
		defer mu.Unlock()
		p.cancel()
		appendTraceResult(auth.ID, r)
		delete(traceRunning, auth.ID)
		_ = saveStateLocked()
	}()
	challenges := traceChallenges(req.Language)
	type event struct {
		index  int
		sample traceSample
		output modelResponse
		err    error
	}
	jobs, events := make(chan int, traceTarget), make(chan event, traceTarget)
	for i := range challenges {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range req.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				sample, output, err := collectTraceSample(ctx, auth, req.Model, challenges[i])
				if sample.Attempts > 0 {
					events <- event{i, sample, output, err}
				}
			}
		}()
	}
	go func() { workers.Wait(); close(events) }()
	samples := make([]*traceSample, traceTarget)
	valid := 0
	for e := range events {
		samples[e.index] = &e.sample
		r.InputTokens += e.output.InputTokens
		r.OutputTokens += e.output.OutputTokens
		r.ReasoningTokens += e.output.ReasoningTokens
		if !errors.Is(e.err, context.Canceled) {
			done++
		}
		if e.sample.Valid {
			valid++
		}
		mu.Lock()
		p.Done = done
		mu.Unlock()
	}
	var outputs []traceOutput
	// Preserve challenge order even when requests complete out of order.
	for _, sample := range samples {
		if sample == nil {
			continue
		}
		r.Samples = append(r.Samples, *sample)
		outputs = append(outputs, traceOutput{sample.Text, sample.ExpectedCount})
	}
	if attr, err := analyzeModelTrace(outputs); err != nil {
		r.Status, r.Error = "failed", err.Error()
		if len(r.Samples) > 0 && r.Samples[len(r.Samples)-1].Error != "" {
			r.Error = r.Samples[len(r.Samples)-1].Error
		}
	} else {
		r.Attribution = &attr
		if valid < traceTarget {
			r.Status = "partial"
		}
	}
}

func collectTraceSample(ctx context.Context, auth credential, model string, challenge traceChallenge) (sample traceSample, out modelResponse, err error) {
	sample.traceChallenge = challenge
	body, headers := codexTurn(auth.ID, model, "", challenge.Prompt)
	out, sample.Attempts, err = executeProbe(ctx, auth, model, body, headers, traceSlots)
	if err != nil {
		sample.Error = truncate(err.Error(), 500)
		return
	}
	if len(out.Answer) > traceMaxText {
		sample.Error = "回答过长，本次未计入"
		return
	}
	sample.Text = out.Answer
	sample.Parsed = len(traceParseNumbers(sample.Text))
	sample.Valid = sample.Parsed >= traceMinimumNumbers(challenge.ExpectedCount)
	if !sample.Valid {
		sample.Error = "有效数字不足"
	}
	return
}

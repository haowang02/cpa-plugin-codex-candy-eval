package plugin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const candyMaxRuns = 10

var (
	candyResults = map[string][]candyResult{}
	candyRunning = map[string]*candyProgress{}
)

const candyPrompt = `不使用任何外部工具回答以下问题：

在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

        苹果味  桃子味  西瓜味
圆形       7      9      8
五角星形   7      6      4
`

const candyPromptEnglish = `Answer the following question without using any external tools:

A black bag contains candies with three flavors, and each flavor comes in two shapes (round and pentagonal; the shapes can be distinguished by touch). The following table gives the counts of each flavor and shape. Before the activity, a participant must decide how many candies to draw. What is the minimum number of candies that guarantees having both round and pentagonal apple-flavored and peach-flavored candies in hand? (It is enough to have round apple with pentagonal peach, or round peach with pentagonal apple.)

                 Apple  Peach  Watermelon
Round               7      9           8
Pentagonal          7      6           4
`

type candyResult struct {
	Skipped         bool      `json:"skipped,omitempty"`
	Time            time.Time `json:"time"`
	Model           string    `json:"model"`
	Effort          string    `json:"effort"`
	Language        string    `json:"language,omitempty"`
	OK              bool      `json:"ok"`
	Answer          string    `json:"answer,omitempty"`
	Error           string    `json:"error,omitempty"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	DurationMS      int64     `json:"duration_ms"`
}

type candyProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type candyRunRequest struct {
	ModelCatalog map[string][]string `json:"model_catalog,omitempty"`
	AuthIDs      []string            `json:"auth_ids"`
	All          bool                `json:"all"`
	Model        string              `json:"model"`
	Effort       string              `json:"effort"`
	Language     string              `json:"language,omitempty"`
	Runs         int                 `json:"runs"`
}

func candyRunResponse(body []byte) managementResponse {
	var req candyRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误："+err.Error())
	}
	req.Model = strings.TrimSpace(req.Model)
	req.Effort = strings.TrimSpace(req.Effort)
	language, languageErr := validateLanguage(req.Language)
	if languageErr != nil {
		return *languageErr
	}
	if !validModelName(req.Model, true) {
		return jsonError(http.StatusBadRequest, "请选择模型")
	}
	req.Runs = min(max(req.Runs, 1), candyMaxRuns)
	return startRuns(req.AuthIDs, req.All, req.ModelCatalog, req.Model,
		func(id string) {
			candyResults[id] = appendRecent(candyResults[id], candyResult{Time: time.Now().UTC(), Model: req.Model, Effort: req.Effort, Language: language, Skipped: true, Error: unsupportedModelMessage})
		},
		func(auth credential) {
			candyRunning[auth.ID] = &candyProgress{Total: req.Runs}
			go runCandy(auth, req.Model, req.Effort, language, req.Runs)
		})
}

// Each credential runs serially; different credentials run in parallel.
func runCandy(auth credential, model, effort, language string, runs int) {
	id := auth.ID
	defer tasks.Done()
	defer func() {
		mu.Lock()
		delete(candyRunning, id)
		mu.Unlock()
	}()
	for range runs {
		mu.Lock()
		stopping := quiescing
		mu.Unlock()
		if stopping {
			return
		}
		r := evaluateCandy(auth, model, effort, language)
		mu.Lock()
		candyResults[id] = appendRecent(candyResults[id], r)
		candyRunning[id].Done++
		_ = saveStateLocked()
		mu.Unlock()
	}
}

func evaluateCandy(auth credential, model, effort string, languages ...string) candyResult {
	language := languageChinese
	if len(languages) > 0 {
		language, _ = validateLanguage(languages[0])
	}
	r := candyResult{Time: time.Now().UTC(), Model: model, Effort: effort, Language: language}
	prompt := candyPrompt
	if language == languageEnglish {
		prompt = candyPromptEnglish
	}
	out, elapsed, err := askCodex(auth, model, effort, prompt)
	r.DurationMS = elapsed.Milliseconds()
	r.InputTokens, r.OutputTokens, r.ReasoningTokens = out.InputTokens, out.OutputTokens, out.ReasoningTokens
	if err != nil {
		r.Error = truncate(err.Error(), 500)
		return r
	}
	r.Answer = truncate(out.Answer, 4000)
	r.OK = hasStandalone21(out.Answer)
	return r
}

// hasStandalone21 reports whether "21" appears without adjacent digits.
func hasStandalone21(text string) bool {
	isDigit := func(i int) bool { return i >= 0 && i < len(text) && text[i] >= '0' && text[i] <= '9' }
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '2' && text[i+1] == '1' && !isDigit(i-1) && !isDigit(i+2) {
			return true
		}
	}
	return false
}

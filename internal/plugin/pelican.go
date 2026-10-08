package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const pelicanMaxHTML = 256 << 10

// Simon Willison's pelican-on-a-bicycle test, framed so a Codex model answers in text instead of writing a file.
const pelicanPrompt = "不使用任何工具，也不要创建或修改文件，直接在回复中完成以下任务：\n\n" +
	"创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要使用外部资产。\n\n" +
	"在一个 ```html 代码块中给出完整的 HTML 文件。"

const pelicanPromptEnglish = "Without using any tools and without creating or modifying files, complete the following task directly in your response:\n\n" +
	"Create an HTML page containing a 2D SVG animation of a pelican riding a bicycle. You do not need tests and must not use external assets.\n\n" +
	"Provide the complete HTML file in one ```html code block."

type pelicanResult struct {
	ID              string    `json:"id"`
	Time            time.Time `json:"time"`
	Model           string    `json:"model"`
	Effort          string    `json:"effort"`
	Language        string    `json:"language,omitempty"`
	Skipped         bool      `json:"skipped,omitempty"`
	HTML            string    `json:"html,omitempty"`
	Error           string    `json:"error,omitempty"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	DurationMS      int64     `json:"duration_ms"`
}

// summary drops the HTML, which previews load per record.
func (r pelicanResult) summary() pelicanResult {
	r.HTML = ""
	return r
}

type pelicanRunRequest struct {
	ModelCatalog map[string][]string `json:"model_catalog,omitempty"`
	AuthIDs      []string            `json:"auth_ids"`
	All          bool                `json:"all"`
	Model        string              `json:"model"`
	Effort       string              `json:"effort"`
	Language     string              `json:"language,omitempty"`
}

var (
	pelicanResults = map[string][]pelicanResult{}
	pelicanRunning = map[string]bool{}
)

func pelicanRunResponse(body []byte) managementResponse {
	var req pelicanRunRequest
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
	return startRuns(req.AuthIDs, req.All, req.ModelCatalog, req.Model,
		func(id string) {
			now := time.Now().UTC()
			pelicanResults[id] = appendRecent(pelicanResults[id], pelicanResult{ID: fmt.Sprint(now.UnixNano()), Time: now, Model: req.Model, Effort: req.Effort, Language: language, Skipped: true, Error: unsupportedModelMessage})
		},
		func(auth credential) {
			pelicanRunning[auth.ID] = true
			go runPelican(auth, req.Model, req.Effort, language)
		})
}

func runPelican(auth credential, model, effort, language string) {
	defer tasks.Done()
	r := evaluatePelican(auth, model, effort, language)
	mu.Lock()
	defer mu.Unlock()
	pelicanResults[auth.ID] = appendRecent(pelicanResults[auth.ID], r)
	delete(pelicanRunning, auth.ID)
	_ = saveStateLocked()
}

func evaluatePelican(auth credential, model, effort string, languages ...string) pelicanResult {
	language := languageChinese
	if len(languages) > 0 {
		language, _ = validateLanguage(languages[0])
	}
	now := time.Now()
	r := pelicanResult{ID: fmt.Sprint(now.UnixNano()), Time: now.UTC(), Model: model, Effort: effort, Language: language}
	prompt := pelicanPrompt
	if language == languageEnglish {
		prompt = pelicanPromptEnglish
	}
	out, elapsed, err := askCodex(auth, model, effort, prompt)
	r.DurationMS = elapsed.Milliseconds()
	r.InputTokens, r.OutputTokens, r.ReasoningTokens = out.InputTokens, out.OutputTokens, out.ReasoningTokens
	if err != nil {
		r.Error = truncate(err.Error(), 500)
		return r
	}
	switch html := extractHTML(out.Answer); {
	case html == "":
		r.Error = "回答中没有 HTML 代码"
	case len(html) > pelicanMaxHTML:
		r.Error = fmt.Sprintf("HTML 超过 %d KB，未保存", pelicanMaxHTML>>10)
	default:
		r.HTML = html
	}
	return r
}

var codeBlock = regexp.MustCompile("(?s)```[ \\t]*([\\w-]*)[^\\n]*\\n(.*?)\\n[ \\t]*```")

// extractHTML returns the HTML document in a model answer: its html code block, else the first code block
// with markup, else the answer itself when it is bare markup.
func extractHTML(answer string) string {
	blocks := codeBlock.FindAllStringSubmatch(answer, -1)
	for _, block := range blocks {
		if strings.EqualFold(block[1], "html") {
			return strings.TrimSpace(block[2])
		}
	}
	for _, block := range blocks {
		if code := strings.TrimSpace(block[2]); strings.HasPrefix(code, "<") {
			return code
		}
	}
	if text := strings.TrimSpace(answer); len(blocks) == 0 && strings.HasPrefix(text, "<") {
		return text
	}
	return ""
}

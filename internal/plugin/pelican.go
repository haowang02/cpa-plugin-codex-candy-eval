package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const pelicanMaxCode = 256 << 10

// pelicanPreamble opens every pelican prompt so a Codex model answers in text instead of writing a file.
const pelicanPreamble = "不使用任何工具，也不要创建或修改文件，直接在回复中完成以下任务：\n\n"

// pelicanVariant is a version of the pelican test: its prompt and the markup language the answer is written in.
type pelicanVariant struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	language string
}

var pelicanVariants = []pelicanVariant{
	// Simon Willison's pelican-on-a-bicycle test.
	{ID: "original", Label: "原版", language: "html", Prompt: pelicanPreamble +
		"创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要使用外部资产。\n\n" +
		"在一个 ```html 代码块中给出完整的 HTML 文件。"},
	// The pelican in a crowded scene, shouting the answer to the candy problem.
	{ID: "all-in-one", Label: "多合一版", language: "svg", Prompt: pelicanPreamble +
		"做一张精细的SVG图片，内容是鹈鹕骑着自行车在孙悟空开的大型飞机机翼上骑行，\n" +
		"鹈鹕展开翅膀，上面托着秦始皇的北极熊，秦始皇骑在熊上打螺丝。鹈鹕喊出\n" +
		"“在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）\n" +
		"苹果味 桃子味 西瓜味\n" +
		"圆形 7 9 8\n" +
		"五角星形 7 6 4”\n\n" +
		"的结果数值\n\n" +
		"在一个 ```svg 代码块中给出完整的 SVG 文件。"},
}

func findPelicanVariant(id string) (pelicanVariant, bool) {
	i := slices.IndexFunc(pelicanVariants, func(v pelicanVariant) bool { return v.ID == id })
	if i < 0 {
		return pelicanVariant{}, false
	}
	return pelicanVariants[i], true
}

// pelicanRecord is one drawn page: its status is completed, failed or skipped.
type pelicanRecord struct {
	record
	Effort   string `json:"effort"`
	Variant  string `json:"variant"`
	HTML     string `json:"html,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
}

// summary drops the HTML, which previews load per record.
func (r pelicanRecord) summary() pelicanRecord {
	r.HTML = ""
	return r
}

type pelicanRunRequest struct {
	runRequest
	Effort  string `json:"effort"`
	Variant string `json:"variant"`
}

func pelicanRunResponse(body []byte) managementResponse {
	var req pelicanRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误："+err.Error())
	}
	req.Model = strings.TrimSpace(req.Model)
	req.Effort = strings.TrimSpace(req.Effort)
	if !validModelName(req.Model, true) {
		return jsonError(http.StatusBadRequest, "请选择模型")
	}
	variant, ok := findPelicanVariant(req.Variant)
	if !ok {
		return jsonError(http.StatusBadRequest, "请选择版本")
	}
	return startRuns("pelican", req.runRequest, 1,
		func(id string) {
			records.Pelicans.add(id, pelicanRecord{record: skippedRecord(req.Model), Effort: req.Effort, Variant: variant.ID})
		},
		func(ctx context.Context, auth credential, _ *progress) {
			r := evaluatePelican(ctx, auth, req.Model, req.Effort, variant)
			mu.Lock()
			defer mu.Unlock()
			// Stopping ctx drops the run.
			if ctx.Err() == nil {
				records.Pelicans.add(auth.ID, r)
				_ = saveStateLocked()
			}
		})
}

func evaluatePelican(ctx context.Context, auth credential, model, effort string, variant pelicanVariant) pelicanRecord {
	started := time.Now()
	r := pelicanRecord{record: newRecord(model, started), Effort: effort, Variant: variant.ID}
	var page string
	out, attempts, err := askCodex(ctx, auth, model, effort, "", variant.Prompt, answerTimeout, func(answer string) (err error) {
		page, err = variant.page(answer)
		return err
	})
	r.DurationMS, r.Attempts, r.usage = time.Since(started).Milliseconds(), attempts, out.usage
	if err != nil {
		r.Status, r.Error = "failed", truncate(err.Error(), 500)
		return r
	}
	r.Status, r.HTML = "completed", page
	return r
}

// page returns the HTML page that shows an answer, or why the answer has none to keep. An SVG image is
// scaled to fill the page.
func (v pelicanVariant) page(answer string) (string, error) {
	code, language := extractCode(answer, v.language), strings.ToUpper(v.language)
	switch {
	case code == "":
		return "", fmt.Errorf("回答中没有 %s 代码", language)
	case len(code) > pelicanMaxCode:
		return "", fmt.Errorf("%s 超过 %d KB，未保存", language, pelicanMaxCode>>10)
	case v.language == "svg":
		return "<!doctype html><style>html, body { height: 100%; margin: 0; } body > svg { display: block; width: 100%; height: 100%; }</style>" + code, nil
	default:
		return code, nil
	}
}

var codeBlock = regexp.MustCompile("(?s)```[ \\t]*([\\w-]*)[^\\n]*\\n(.*?)\\n[ \\t]*```")

// extractCode returns the markup in a model answer: its code block in language, else the first code block
// with markup, else the answer itself when it is bare markup.
func extractCode(answer, language string) string {
	blocks := codeBlock.FindAllStringSubmatch(answer, -1)
	for _, block := range blocks {
		if strings.EqualFold(block[1], language) {
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

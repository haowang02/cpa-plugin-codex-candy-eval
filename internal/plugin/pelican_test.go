package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExtractCode(t *testing.T) {
	page := "<!doctype html><svg></svg>"
	for _, tc := range []struct{ answer, language, want string }{
		{"说明\n```css\nbody {}\n```\n```HTML\n" + page + "\n```", "html", page},
		{"```html\n<p></p>\n```\n```svg\n<svg/>\n```", "svg", "<svg/>"},
		{"```\n" + page + "\n```", "html", page},
		{"  " + page + "\n", "html", page},
		{"```js\nlet svg = 1\n```\n" + page, "html", ""},
		{"下面是页面：" + page, "html", ""},
	} {
		if got := extractCode(tc.answer, tc.language); got != tc.want {
			t.Errorf("extractCode(%q, %q) = %q, want %q", tc.answer, tc.language, got, tc.want)
		}
	}
}

func TestPelicanRun(t *testing.T) {
	setupTest(t)
	answers := map[string]json.RawMessage{
		"page": mockModelResponse("好的：\n```html\n<svg></svg>\n```"),
		"tool": json.RawMessage(`{"status":"completed","output":[{"type":"custom_tool_call"}],"usage":{}}`),
	}
	var variant pelicanVariant
	hostCall = streamHost(func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"page","provider":"codex"},{"id":"tool","provider":"codex"}]}`), nil
		}
		// "none" leaves the effort to CPA.
		if body, prompt := sentTurn(payload); prompt != variant.Prompt || body["reasoning"].(map[string]any)["effort"] != nil {
			t.Errorf("prompt = %q, reasoning = %v", prompt, body["reasoning"])
		}
		return answers[payload.(map[string]any)["auth_id"].(string)], nil
	})
	if response := pelicanRunResponse([]byte(`{"all":true,"model":"m","variant":"pelican"}`)); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("accepted an unknown variant: %s", response.Body)
	}
	variant = pelicanVariants[0]
	if response := pelicanRunResponse([]byte(`{"all":true,"model":"m","effort":"none","variant":"original"}`)); response.StatusCode != 200 {
		t.Fatalf("run: %s", response.Body)
	}
	tasks.Wait()
	page, tool := records.Pelicans["page"][0], records.Pelicans["tool"][0]
	// A turn that only calls a tool is asked again.
	if page.HTML != "<svg></svg>" || page.Variant != "original" || page.Error != "" || page.Attempts != 1 || page.OutputTokens != 3 ||
		tool.HTML != "" || tool.Error != "模型调用了工具，没有直接回答" || tool.Attempts != modelAttempts {
		t.Fatalf("page = %+v, tool = %+v", page, tool)
	}
	// Lists carry summaries; the page loads with its record.
	if state := stateResponse(); bytes.Contains(state.Body, []byte("svg")) || !bytes.Contains(state.Body, []byte(page.ID)) {
		t.Fatalf("state = %s", state.Body)
	}
	var full pelicanRecord
	if err := json.Unmarshal(recordResponse("pelican", url.Values{"credential_id": {"page"}, "id": {page.ID}}).Body, &full); err != nil || full.HTML != page.HTML {
		t.Fatalf("record = %+v, %v", full, err)
	}
	records, loaded = emptyState(), false
	loadState()
	if len(records.Pelicans["page"]) != 1 || records.Pelicans["page"][0].HTML != page.HTML {
		t.Fatal("pelican history did not survive reload")
	}
	// An SVG image fills the page that shows it.
	variant = pelicanVariants[1]
	if response := pelicanRunResponse([]byte(`{"credential_ids":["page"],"model":"m","variant":"all-in-one"}`)); response.StatusCode != 200 {
		t.Fatalf("run: %s", response.Body)
	}
	tasks.Wait()
	if image := records.Pelicans["page"][1]; image.Variant != "all-in-one" || !strings.HasSuffix(image.HTML, "</style><svg></svg>") {
		t.Fatalf("image = %+v", image)
	}
}

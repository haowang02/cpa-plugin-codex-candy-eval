package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLanguageDefaultsAndValidation(t *testing.T) {
	for input, want := range map[string]string{"": languageChinese, "zh": languageChinese, "en": languageEnglish} {
		got, err := validateLanguage(input)
		if err != nil || got != want {
			t.Fatalf("validateLanguage(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := validateLanguage("fr"); err == nil {
		t.Fatal("unsupported language was accepted")
	}
}

func TestEnglishPromptSources(t *testing.T) {
	if strings.Contains(candyPromptEnglish, "糖果") || strings.Contains(pelicanPromptEnglish, "鹈鹕") {
		t.Fatal("English prompt contains Chinese test text")
	}
	for _, challenge := range traceChallenges(languageEnglish) {
		if strings.ContainsAny(challenge.Prompt, "这是请执行生成进行数字模型禁止调用") {
			t.Fatalf("English challenge contains Chinese prompt text: %q", challenge.Prompt)
		}
	}
	probes := fingerprintProbesForLanguage(languageEnglish)
	if len(probes) == 0 {
		t.Fatal("no English fingerprint probes")
	}
	for _, probe := range probes {
		if !strings.HasSuffix(probe.ID, ":en") || strings.ContainsAny(probe.Instructions+strings.Join(probe.Prompts, " "), "只回答随机挑给我从里") {
			t.Fatalf("non-English fingerprint probe selected: %+v", probe)
		}
	}
}

func TestEnglishEvaluationUsesEnglishPrompt(t *testing.T) {
	setupTest(t)
	var prompts []string
	hostCall = streamHost(func(_ string, payload any) (json.RawMessage, error) {
		_, prompt := sentTurn(payload)
		prompts = append(prompts, prompt)
		return mockModelResponse("21"), nil
	})
	evaluateCandy(credential{ID: "a", Provider: "codex"}, "m", "low", languageEnglish)
	evaluatePelican(credential{ID: "a", Provider: "codex"}, "m", "low", languageEnglish)
	if len(prompts) != 2 || prompts[0] != candyPromptEnglish || prompts[1] != pelicanPromptEnglish {
		t.Fatalf("English prompts = %q", prompts)
	}
}

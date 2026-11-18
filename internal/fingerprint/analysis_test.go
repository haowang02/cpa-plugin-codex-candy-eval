package fingerprint

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestUpstreamParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/analyze.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string
		Outputs []struct {
			Text          string
			ExpectedCount int `json:"expected_count"`
		}
		Expected Attribution
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			replies := make([]Reply, len(tc.Outputs))
			for i, output := range tc.Outputs {
				replies[i] = Prepare(output.Text, output.ExpectedCount)
			}
			got, want := Analyze(replies), tc.Expected
			if got == nil || got.Decision != want.Decision || got.Prediction != want.Prediction ||
				got.PredictionName != want.PredictionName || got.FamilyPrediction != want.FamilyPrediction ||
				got.Used != want.Used || !slices.Equal(got.Diagnostics, want.Diagnostics) || len(got.Results) != len(want.Results) {
				t.Fatalf("attribution mismatch: got %+v, want %q with diagnostics %v", got, want.Prediction, want.Diagnostics)
			}
			if got.Sequence == nil || got.Sequence.ModelLike != want.Sequence.ModelLike {
				t.Fatalf("sequence = %+v, want %+v", got.Sequence, want.Sequence)
			}
			checkNumber(t, "lag-1 autocorrelation", &got.Sequence.Autocorrelation, &want.Sequence.Autocorrelation)
			checkNumber(t, "small-step share", &got.Sequence.SmallStepShare, &want.Sequence.SmallStepShare)
			checkNumber(t, "top probability", got.Probability, want.Probability)
			for i, candidate := range got.Results {
				expected := want.Results[i]
				checkNumber(t, candidate.Model+" probability", candidate.Probability, expected.Probability)
				checkNumber(t, candidate.Model+" score", &candidate.Score, &expected.Score)
				candidate.Probability, expected.Probability = nil, nil
				candidate.Score, expected.Score = 0, 0
				if candidate != expected {
					t.Fatalf("rank %d = %+v, want %+v", i+1, candidate, expected)
				}
			}
		})
	}
}

func checkNumber(t *testing.T, label string, got, want *float64) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s: got probability=%t, want probability=%t", label, got != nil, want != nil)
	}
	if got != nil && !(math.Abs(*got-*want) <= 1e-9) {
		t.Fatalf("%s = %.12g, want %.12g", label, *got, *want)
	}
}

func TestReplyPreparation(t *testing.T) {
	if got := parseNumbers("生成 300 个：1, 22, 355, 0, 356, 99。完成 4"); !slices.Equal(got, []int{1, 22, 355, 99}) {
		t.Fatalf("parsed %v", got)
	}
	if Prepare(strings.Repeat("42,", 164), 300).Valid() || !Prepare(strings.Repeat("42,", 165), 300).Valid() {
		t.Fatal("expected a minimum of 165 numbers for a challenge of 300")
	}
}

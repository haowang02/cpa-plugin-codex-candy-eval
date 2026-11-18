package fingerprint

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type Reply struct {
	Text    string
	numbers []int
	minimum int
}

func (r Reply) Count() int { return len(r.numbers) }

func (r Reply) Valid() bool { return r.Count() > 0 && r.Count() >= r.minimum }

type Diagnostic struct {
	Index   int  `json:"index"`
	Parsed  int  `json:"parsed_numbers"`
	Minimum int  `json:"minimum_numbers"`
	Valid   bool `json:"accepted"`
}

type Sequence struct {
	Autocorrelation float64 `json:"autocorrelation"`
	SmallStepShare  float64 `json:"small_step_share"`
	ModelLike       bool    `json:"model_like"`
}

type Candidate struct {
	Model       string   `json:"model"`
	DisplayName string   `json:"display_name"`
	Family      string   `json:"family"`
	FamilyName  string   `json:"family_name"`
	Probability *float64 `json:"probability"`
	Score       float64  `json:"score"`
}

type Attribution struct {
	Decision         string       `json:"decision,omitempty"`
	Prediction       string       `json:"prediction"`
	PredictionName   string       `json:"prediction_name,omitempty"`
	Probability      *float64     `json:"probability"`
	Used             int          `json:"used_outputs"`
	FamilyPrediction string       `json:"family_prediction_name"`
	Sequence         *Sequence    `json:"sequence,omitempty"`
	Results          []Candidate  `json:"results"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
}

const maxValue = 355

var digitRuns = regexp.MustCompile(`[0-9]+`)

// parseNumbers keeps the longest run of in-range integers. Letters split runs.
func parseNumbers(text string) []int {
	var best, current []int
	end := 0
	for _, span := range digitRuns.FindAllStringIndex(text, -1) {
		if strings.ContainsFunc(text[end:span[0]], unicode.IsLetter) {
			if len(current) > len(best) {
				best = current
			}
			current = nil
		}
		if n, err := strconv.Atoi(text[span[0]:span[1]]); err == nil && n >= 1 && n <= maxValue {
			current = append(current, n)
		}
		end = span[1]
	}
	if len(current) > len(best) {
		best = current
	}
	return best
}

func Prepare(text string, expected int) Reply {
	numbers := parseNumbers(text)
	if len(numbers) > expected {
		numbers = numbers[:expected]
		values := make([]string, len(numbers))
		for i, n := range numbers {
			values[i] = strconv.Itoa(n)
		}
		text = strings.Join(values, ", ")
	}
	minimum := max(80, int(math.Ceil(float64(expected)*0.55)))
	return Reply{Text: text, numbers: numbers, minimum: minimum}
}

// Analyze returns nil when none of the replies contains enough valid numbers.
func Analyze(replies []Reply) *Attribution {
	d := &sharedDetector
	r := &Attribution{}
	var accepted [][]int
	for i, reply := range replies {
		valid := reply.Valid()
		r.Diagnostics = append(r.Diagnostics, Diagnostic{i, reply.Count(), reply.minimum, valid})
		if valid {
			accepted = append(accepted, reply.numbers)
		}
	}
	r.Used = len(accepted)
	if r.Used == 0 {
		return nil
	}
	r.Sequence = sequenceShape(accepted)
	if !r.Sequence.ModelLike {
		r.Decision = "not_model"
		return r
	}
	ranking := d.rank(accepted)
	var probabilities []float64
	if r.Used == ChallengeCount {
		probabilities = softmax(ranking, d.Calibration.Tau)
	} else {
		r.Decision = "partial"
	}
	r.Results = make([]Candidate, len(d.models))
	for i, model := range d.models {
		r.Results[i] = Candidate{Model: model.ID, DisplayName: model.DisplayName, Family: model.Family, FamilyName: model.FamilyName, Score: ranking[i]}
		if probabilities != nil {
			r.Results[i].Probability = &probabilities[i]
		}
	}
	slices.SortStableFunc(r.Results, func(a, b Candidate) int { return cmp.Compare(b.Score, a.Score) })
	top := r.Results[0]
	r.Prediction, r.PredictionName, r.Probability, r.FamilyPrediction = top.Model, top.DisplayName, top.Probability, top.FamilyName
	return r
}

func sequenceShape(answers [][]int) *Sequence {
	autocorrelations := make([]float64, len(answers))
	smallSteps := make([]float64, len(answers))
	nearbyPairs := make([]float64, len(answers))
	for k, numbers := range answers {
		x := toFloats(numbers)
		m, variance := meanVariance(x)
		lagged := 0.0
		for i := 1; i < len(x); i++ {
			lagged += (x[i] - m) * (x[i-1] - m)
		}
		autocorrelations[k] = 1
		if variance != 0 {
			autocorrelations[k] = lagged / float64(len(x)-1) / variance
		}
		closePairs, pairs := 0, 0
		for lag := 1; lag <= 3; lag++ {
			pairs += len(numbers) - lag
			for i := lag; i < len(numbers); i++ {
				if absInt(numbers[i]-numbers[i-lag]) <= 20 {
					closePairs++
				}
			}
			if lag == 1 {
				smallSteps[k] = float64(closePairs) / float64(pairs)
			}
		}
		nearbyPairs[k] = float64(closePairs) / float64(pairs)
	}
	s := &Sequence{Autocorrelation: median(autocorrelations), SmallStepShare: median(smallSteps)}
	s.ModelLike = s.Autocorrelation <= -0.23 || s.SmallStepShare <= 0.05 || median(nearbyPairs) <= 0.07
	return s
}

func softmax(ranking []float64, tau float64) []float64 {
	maximum := tau * slices.Max(ranking)
	weights, total := make([]float64, len(ranking)), 0.0
	for i, score := range ranking {
		weights[i] = math.Exp(tau*score - maximum)
		total += weights[i]
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

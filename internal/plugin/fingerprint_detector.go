package plugin

// Model attribution, ported from Ikaleio/lm-detector (MIT).
// See data/lm-detector/README.md for the pinned source and license.
import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"unicode"
)

//go:embed data/lm-detector/shared_detector.bin
var detectorFile []byte

//go:embed data/lm-detector/models.json
var detectorModelsJSON []byte

//go:embed data/lm-detector/LICENSE
var detectorLicense string

var detectorRevision = fmt.Sprintf("sha256:%x", sha256.Sum256(detectorFile))

// detectorVector is a float array of the detector file. The header stores it as {"$f32": offset, "n": length}
// into the float section, which detectorFloats holds while decodeDetector parses the header.
type detectorVector []float64

var detectorFloats []float64

func (v *detectorVector) UnmarshalJSON(data []byte) error {
	var ref struct {
		Offset *int `json:"$f32"`
		N      int  `json:"n"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return err
	}
	if ref.Offset == nil || *ref.Offset < 0 || ref.N < 0 || *ref.Offset+ref.N > len(detectorFloats) {
		return fmt.Errorf("invalid float array %s", data)
	}
	*v = detectorFloats[*ref.Offset : *ref.Offset+ref.N : *ref.Offset+ref.N]
	return nil
}

type detectorScaling struct {
	Mean  detectorVector `json:"mean"`
	Scale detectorVector `json:"scale"`
}

type detectorFeatureBank struct {
	Mean         detectorVector     `json:"feature_mean"`
	Scale        detectorVector     `json:"feature_scale"`
	Basis        []detectorVector   `json:"nuisance_basis"`
	Centroids    []detectorVector   `json:"centroids"`
	Environments [][]detectorVector `json:"environment_centroids"`
}

type detectorModel struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	Family        string `json:"family"`
	FamilyName    string `json:"family_name"`
	ResponseCount int    `json:"response_count"`
}

type detector struct {
	Schema          string         `json:"schema"`
	BaseSHA256      string         `json:"base_sha256"`
	ReferenceSHA256 string         `json:"source_reference_sha256"`
	BankBuiltAt     string         `json:"bank_built_at"`
	ModelIDs        []string       `json:"model_ids"`
	ResponseCounts  detectorVector `json:"response_counts"`
	Ranker          struct {
		HeadParams []detectorScaling  `json:"head_params"`
		FullParams []detectorScaling  `json:"full_params"`
		LDAWeights []detectorVector   `json:"lda_weights"`
		LDABias    detectorVector     `json:"lda_bias"`
		References [][]detectorVector `json:"references"`
		Bank       struct {
			Hellinger detectorFeatureBank `json:"hellinger"`
			Ordered   detectorFeatureBank `json:"ordered_blocks"`
		} `json:"bank"`
		Blend struct {
			Frequency  float64 `json:"frequency"`
			Positional float64 `json:"positional"`
		} `json:"blend"`
		Positional struct {
			Params  []detectorScaling `json:"params"`
			Weights []detectorVector  `json:"weights"`
			Bias    detectorVector    `json:"bias"`
		} `json:"positional"`
	} `json:"ranker"`
	Calibration struct {
		Schema  string  `json:"schema"`
		Method  string  `json:"method"`
		Tau     float64 `json:"tau"`
		Binding struct {
			BaseSHA256      string   `json:"base_sha256"`
			ReferenceSHA256 string   `json:"reference_sha256"`
			ModelIDs        []string `json:"model_ids"`
		} `json:"binding"`
	} `json:"calibration"`
	models         []detectorModel
	referenceNorms [][]float64
}

var sharedDetector = decodeDetector(detectorFile, detectorModelsJSON)

// decodeDetector reads a little-endian u32 header length, the JSON header, zero padding to 4 bytes,
// then the float32 values stored byte plane by byte plane: every first byte, then every second byte, and so on.
func decodeDetector(file, modelsJSON []byte) detector {
	headerEnd := 4 + int(binary.LittleEndian.Uint32(file))
	start := (headerEnd + 3) / 4 * 4
	count := (len(file) - start) / 4
	detectorFloats = make([]float64, count)
	defer func() { detectorFloats = nil }()
	for i := range detectorFloats {
		var value [4]byte
		for plane := range value {
			value[plane] = file[start+plane*count+i]
		}
		detectorFloats[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(value[:])))
	}
	d := mustDecode[detector](file[4:headerEnd])
	bank := mustDecode[struct {
		BuiltAt string          `json:"built_at"`
		Models  []detectorModel `json:"models"`
	}](modelsJSON)
	c := d.Calibration
	if d.Schema != "shared-detector-v2" || bank.BuiltAt != d.BankBuiltAt || !slices.EqualFunc(bank.Models, d.ModelIDs, func(m detectorModel, id string) bool { return m.ID == id }) ||
		c.Schema != "shared-confidence-v2" || c.Method != "ranking-temperature" || !(c.Tau >= 0.001 && c.Tau <= 1000) ||
		c.Binding.BaseSHA256 != d.BaseSHA256 || c.Binding.ReferenceSHA256 != d.ReferenceSHA256 || !slices.Equal(c.Binding.ModelIDs, d.ModelIDs) {
		panic("lm-detector detector, models and calibration do not match")
	}
	for i, m := range bank.Models {
		if float64(m.ResponseCount) != d.ResponseCounts[i] {
			panic("lm-detector models do not match the detector references")
		}
	}
	d.models = bank.Models
	for _, references := range d.Ranker.References {
		norms := make([]float64, len(references))
		for i, r := range references {
			norms[i] = dot(r, r)
		}
		d.referenceNorms = append(d.referenceNorms, norms)
	}
	return d
}

type fingerprintOutput struct {
	Text          string `json:"text"`
	ExpectedCount int    `json:"expected_count"`
}

type fingerprintDiagnostic struct {
	Index   int  `json:"index"`
	Parsed  int  `json:"parsed_numbers"`
	Minimum int  `json:"minimum_numbers"`
	Valid   bool `json:"accepted"`
}

type fingerprintSequence struct {
	Autocorrelation float64 `json:"autocorrelation"`
	SmallStepShare  float64 `json:"small_step_share"`
	ModelLike       bool    `json:"model_like"`
}

type fingerprintCandidate struct {
	Model       string   `json:"model"`
	DisplayName string   `json:"display_name"`
	Family      string   `json:"family"`
	FamilyName  string   `json:"family_name"`
	Probability *float64 `json:"probability"`
	Score       float64  `json:"score"`
}

// fingerprintAttribution ranks every candidate when three answers are valid, with calibrated probabilities.
// Decision "partial" ranks one or two valid answers without probabilities, and "not_model" means the
// answers do not move like a language model's, so nothing is ranked.
type fingerprintAttribution struct {
	Decision         string                  `json:"decision,omitempty"`
	Prediction       string                  `json:"prediction"`
	PredictionName   string                  `json:"prediction_name,omitempty"`
	Probability      *float64                `json:"probability"`
	Used             int                     `json:"used_outputs"`
	FamilyPrediction string                  `json:"family_prediction_name"`
	Sequence         *fingerprintSequence    `json:"sequence,omitempty"`
	Results          []fingerprintCandidate  `json:"results"`
	Diagnostics      []fingerprintDiagnostic `json:"diagnostics"`
}

var digitRuns = regexp.MustCompile(`[0-9]+`)

func minimumNumbers(expected int) int {
	return max(80, int(math.Ceil(float64(expected)*0.55)))
}

// Keep the longest run, ignoring introductory counts and numbers in prose.
// ASCII digits and Unicode letters match the upstream implementation.
func parseNumbers(text string) []int {
	var best, current []int
	end := 0
	for _, span := range digitRuns.FindAllStringIndex(text, -1) {
		for _, r := range text[end:span[0]] {
			if unicode.IsLetter(r) {
				if len(current) > len(best) {
					best = current
				}
				current = nil
				break
			}
		}
		if n, err := strconv.Atoi(text[span[0]:span[1]]); err == nil && n >= 1 && n <= 355 {
			current = append(current, n)
		}
		end = span[1]
	}
	if len(current) > len(best) {
		best = current
	}
	return best
}

func analyzeFingerprint(outputs []fingerprintOutput) (fingerprintAttribution, error) {
	d := &sharedDetector
	r := fingerprintAttribution{}
	var accepted [][]int
	for i, output := range outputs {
		numbers := parseNumbers(output.Text)
		minimum := minimumNumbers(output.ExpectedCount)
		valid := len(numbers) >= minimum
		r.Diagnostics = append(r.Diagnostics, fingerprintDiagnostic{i, len(numbers), minimum, valid})
		if valid {
			accepted = append(accepted, numbers)
		}
	}
	if r.Used = len(accepted); r.Used == 0 {
		return r, errors.New("未采集到有效数字序列")
	}
	if r.Sequence = sequenceShape(accepted); !r.Sequence.ModelLike {
		r.Decision = "not_model"
		return r, nil
	}
	ranking := d.rank(accepted)
	var probabilities []float64
	if r.Used == fingerprintTarget {
		probabilities = softmax(ranking, d.Calibration.Tau)
	} else {
		r.Decision = "partial"
	}
	order := make([]int, len(ranking))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return ranking[order[a]] > ranking[order[b]] })
	for _, i := range order {
		m := d.models[i]
		candidate := fingerprintCandidate{Model: m.ID, DisplayName: m.DisplayName, Family: m.Family, FamilyName: m.FamilyName, Score: ranking[i]}
		if probabilities != nil {
			candidate.Probability = &probabilities[i]
		}
		r.Results = append(r.Results, candidate)
	}
	top := r.Results[0]
	r.Prediction, r.PredictionName, r.Probability, r.FamilyPrediction = top.Model, top.DisplayName, top.Probability, top.FamilyName
	return r, nil
}

// sequenceShape checks how consecutive numbers move. Language-model answers tend to jump between distant values:
// a negative lag-1 autocorrelation and few steps of 20 or less. Only answers that exceed both limits are not
// ranked, since a model's answer can miss one of them. The limits are 99.5% quantiles over the reference
// bank's three-answer groups.
func sequenceShape(answers [][]int) *fingerprintSequence {
	var autocorrelations, smallSteps []float64
	for _, numbers := range answers {
		x := toFloats(numbers)
		m := mean(x)
		variance, lagged, small := 0.0, 0.0, 0.0
		for _, v := range x {
			variance += (v - m) * (v - m)
		}
		variance /= float64(len(x))
		for i := 1; i < len(x); i++ {
			lagged += (x[i] - m) * (x[i-1] - m)
			if math.Abs(x[i]-x[i-1]) <= 20 {
				small++
			}
		}
		autocorrelation := 1.0
		if variance != 0 {
			autocorrelation = lagged / float64(len(x)-1) / variance
		}
		autocorrelations = append(autocorrelations, autocorrelation)
		smallSteps = append(smallSteps, small/float64(len(x)-1))
	}
	s := &fingerprintSequence{Autocorrelation: median(autocorrelations), SmallStepShare: median(smallSteps)}
	s.ModelLike = s.Autocorrelation <= -0.23 || s.SmallStepShare <= 0.05
	return s
}

func softmax(ranking []float64, tau float64) []float64 {
	maximum := math.Inf(-1)
	for _, score := range ranking {
		maximum = math.Max(maximum, tau*score)
	}
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

// rank blends a frequency ranking (LDA over the first 128 numbers, nearest references and nuisance-corrected
// centroids) with a positional and sequential discriminant; each part is standardized across candidates.
func (d *detector) rank(answers [][]int) []float64 {
	a := &d.Ranker
	var lda, base, positional [][]float64
	full := make([][]float64, len(answers))
	for k, numbers := range answers {
		full[k] = joinBlocks([][]float64{hellingerFeature(numbers), orderedBlockFeature(numbers)}, a.FullParams, []float64{0.75, 0.25})
		head := numbers[:min(128, len(numbers))]
		x := joinBlocks([][]float64{hellingerFeature(head), orderedBlockFeature(head)}, a.HeadParams, []float64{0.75, 0.25})
		scores := make([]float64, len(a.LDAWeights))
		for i, row := range a.LDAWeights {
			scores[i] = dot(row, x) + a.LDABias[i]
		}
		lda = append(lda, zscores(scores))
		base = append(base, d.baseline(numbers))
		positional = append(positional, zscores(d.positional(numbers)))
	}
	near := make([]float64, len(a.References))
	for m, references := range a.References {
		distances := make([]float64, len(full))
		for k, x := range full {
			distances[k] = -nearestDistance(x, references, d.referenceNorms[m])
		}
		near[m] = median(distances)
	}
	l, n, b := zscores(columnMean(lda)), zscores(near), zscores(columnMean(base))
	frequency := make([]float64, len(l))
	for i := range frequency {
		frequency[i] = 0.5*l[i] + 0.25*n[i] + 0.25*b[i]
	}
	frequency, sequential := zscores(frequency), zscores(columnMean(positional))
	ranking := make([]float64, len(frequency))
	for i := range ranking {
		ranking[i] = a.Blend.Frequency*frequency[i] + a.Blend.Positional*sequential[i]
	}
	return ranking
}

// baseline compares nuisance-corrected value distributions with the model centroids, and the ordered blocks
// with each prompt environment's model templates.
func (d *detector) baseline(numbers []int) []float64 {
	h, o := &d.Ranker.Bank.Hellinger, &d.Ranker.Bank.Ordered
	hUnit := unitVector(subtractBasis(standardize(hellingerFeature(numbers), h.Mean, h.Scale), h.Basis))
	marginal := make([]float64, len(h.Centroids))
	for i, row := range h.Centroids {
		marginal[i] = dot(hUnit, row)
	}
	centered := standardize(orderedBlockFeature(numbers), o.Mean, o.Scale)
	raw, projected := unitVector(centered), unitVector(subtractBasis(centered, o.Basis))
	templates, nuisance := make([]float64, len(o.Centroids)), make([]float64, len(o.Centroids))
	for i, row := range o.Centroids {
		templates[i] = math.Inf(-1)
		for _, environment := range o.Environments {
			templates[i] = math.Max(templates[i], dot(raw, environment[i]))
		}
		nuisance[i] = dot(projected, row)
	}
	tz, nz := zscores(templates), zscores(nuisance)
	for i := range tz {
		tz[i] = 0.5*tz[i] + 0.5*nz[i]
	}
	ordered, mz := zscores(tz), zscores(marginal)
	for i := range mz {
		mz[i] = 0.5*mz[i] + 0.5*ordered[i]
	}
	return mz
}

// positional scores the value distribution, ordered blocks, value positions and step patterns of one answer.
func (d *detector) positional(numbers []int) []float64 {
	p := &d.Ranker.Positional
	x := joinBlocks([][]float64{hellingerFeature(numbers), orderedBlockFeature(numbers), positionKernel(numbers), sequenceFeatures(numbers)},
		p.Params, []float64{0.6, 0.15, 0.125, 0.125})
	scores := make([]float64, len(p.Weights))
	for k, row := range p.Weights {
		scores[k] = p.Bias[k]
		for i, w := range row {
			scores[k] += w * x[i]
		}
	}
	return scores
}

// hellingerFeature is the square root of the smoothed frequency of each value from 1 to 355.
func hellingerFeature(numbers []int) []float64 {
	counts := make([]float64, 355)
	for i := range counts {
		counts[i] = 0.5
	}
	for _, n := range numbers {
		counts[n-1]++
	}
	return rootShares(counts)
}

// orderedBlockFeature splits the answer into four blocks of 16 value bins, then adds the final digits.
func orderedBlockFeature(numbers []int) []float64 {
	out := make([]float64, 0, 74)
	start := 0
	for i := range 4 {
		size := len(numbers) / 4
		if i < len(numbers)%4 {
			size++
		}
		bins := make([]float64, 16)
		for j := range bins {
			bins[j] = 0.5
		}
		for _, n := range numbers[start : start+size] {
			bins[min(15, int(math.Floor(float64(n-1)/355*16)))]++
		}
		out = append(out, rootShares(bins)...)
		start += size
	}
	digits := make([]float64, 10)
	for i := range digits {
		digits[i] = 0.5
	}
	for _, n := range numbers {
		digits[n%10]++
	}
	return append(out, rootShares(digits)...)
}

// positionKernel averages 24 value RBFs and 10 final-digit indicators times three Legendre polynomials
// of the relative position: where in the answer each kind of number tends to appear.
func positionKernel(numbers []int) []float64 {
	n := float64(len(numbers))
	out := make([]float64, 34*3)
	for position, value := range numbers {
		t := (float64(position)+0.5)*2/n - 1
		basis := [3]float64{t, 0.5 * (3*t*t - 1), 0.5 * (5*math.Pow(t, 3) - 3*t)}
		for k := range 34 {
			weight := 0.0
			if k < 24 {
				distance := (float64(value) - (1 + float64(k)*354/23)) / 20
				weight = math.Exp(-0.5 * distance * distance)
			} else if k-24 == value%10 {
				weight = 1
			}
			for j, b := range basis {
				out[k*3+j] += weight * b / n
			}
		}
	}
	return out
}

// sequenceFeatures describes steps at eight lags, value-bin and final-digit transitions, runs of rises
// and falls, and how many value bins short windows cover.
func sequenceFeatures(numbers []int) []float64 {
	x := toFloats(numbers)
	n := len(x)
	m := mean(x)
	variance := 0.0
	for _, v := range x {
		variance += (v - m) * (v - m)
	}
	sd := math.Max(math.Sqrt(variance/float64(n)), 1)
	s := make([]float64, n)
	for i, v := range x {
		s[i] = (v - m) / sd
	}
	fraction := func(count int, test func(int) bool) float64 {
		hits := 0
		for i := range count {
			if test(i) {
				hits++
			}
		}
		return float64(hits) / float64(count)
	}
	var out []float64
	for _, lag := range []int{1, 2, 3, 5, 8, 13, 21, 34} {
		count := n - lag
		step := func(i int) int { return numbers[i+lag] - numbers[i] }
		correlation, distance := 0.0, 0.0
		for i := range count {
			correlation += s[i+lag] * s[i]
			distance += math.Abs(float64(step(i)))
		}
		out = append(out, correlation/float64(count), distance/float64(count)/355,
			fraction(count, func(i int) bool { return step(i) > 0 }), fraction(count, func(i int) bool { return step(i) == 0 }),
			fraction(count, func(i int) bool { return absInt(step(i)) <= 5 }), fraction(count, func(i int) bool { return absInt(step(i)) <= 20 }))
	}
	bins, digits := make([]int, n), make([]int, n)
	for i, v := range numbers {
		bins[i], digits[i] = min(7, (v-1)*8/355), v%10
	}
	out = append(out, centredTransitions(bins, 8)...)
	out = append(out, centredTransitions(digits, 10)...)
	step := func(i int) int { return numbers[i+1] - numbers[i] }
	out = append(out,
		fraction(n-2, func(i int) bool { return cmp.Compare(step(i+1), 0) == cmp.Compare(step(i), 0) }),
		fraction(n-2, func(i int) bool { return step(i+1) == step(i) }),
		fraction(n-1, func(i int) bool { return numbers[i]%10 == numbers[i+1]%10 }),
		fraction(n-1, func(i int) bool { return math.Abs(s[i+1]-s[i]) > 1 }),
		fraction(n-1, func(i int) bool { return math.Abs(s[i+1]-s[i]) > 2 }))
	for _, width := range []int{4, 8, 16} {
		coverage := 0.0
		for i := 0; i+width <= n; i++ {
			seen := map[int]bool{}
			for _, v := range numbers[i : i+width] {
				seen[min(15, (v-1)*16/355)] = true
			}
			coverage += float64(len(seen)) / float64(width)
		}
		out = append(out, coverage/float64(n-width+1))
	}
	return out
}

// centredTransitions is the smoothed joint distribution of consecutive symbols minus the product of its marginals.
func centredTransitions(symbols []int, bins int) []float64 {
	joint := make([]float64, bins*bins)
	for i := range joint {
		joint[i] = 0.5
	}
	for i := 1; i < len(symbols); i++ {
		joint[symbols[i-1]*bins+symbols[i]]++
	}
	total := float64(len(symbols)-1) + 0.5*float64(bins*bins)
	for i := range joint {
		joint[i] /= total
	}
	rows, columns := make([]float64, bins), make([]float64, bins)
	for i, v := range joint {
		rows[i/bins] += v
	}
	for b := range columns {
		for a := range bins {
			columns[b] += joint[a*bins+b]
		}
	}
	for i := range joint {
		joint[i] -= rows[i/bins] * columns[i%bins]
	}
	return joint
}

// joinBlocks standardizes each block, scales it to unit length, weights it by the square root of its share
// and concatenates the blocks.
func joinBlocks(blocks [][]float64, params []detectorScaling, shares []float64) []float64 {
	var out []float64
	for j, block := range blocks {
		weight := math.Sqrt(shares[j])
		for _, v := range unitVector(standardize(block, params[j].Mean, params[j].Scale)) {
			out = append(out, v*weight)
		}
	}
	return out
}

// nearestDistance is the mean squared distance from x to its seven nearest references.
func nearestDistance(x []float64, references []detectorVector, norms []float64) float64 {
	xx := dot(x, x)
	distances := make([]float64, len(references))
	for i, r := range references {
		distances[i] = math.Max(0, xx+norms[i]-2*dot(x, r))
	}
	sort.Float64s(distances)
	return mean(distances[:7])
}

func rootShares(bins []float64) []float64 {
	total := 0.0
	for _, v := range bins {
		total += v
	}
	out := make([]float64, len(bins))
	for i, v := range bins {
		out[i] = math.Sqrt(v / total)
	}
	return out
}

func standardize(v, center, scale []float64) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		out[i] = (v[i] - center[i]) / scale[i]
	}
	return out
}

// subtractBasis removes the projections of v onto the basis rows.
func subtractBasis(v []float64, basis []detectorVector) []float64 {
	coefficients := make([]float64, len(basis))
	for j, row := range basis {
		coefficients[j] = dot(v, row)
	}
	out := make([]float64, len(v))
	for i, x := range v {
		projection := 0.0
		for j, row := range basis {
			projection += coefficients[j] * row[i]
		}
		out[i] = x - projection
	}
	return out
}

func dot(a, b []float64) float64 {
	sum := 0.0
	for i, v := range a {
		sum += v * b[i]
	}
	return sum
}

func mean(a []float64) float64 {
	sum := 0.0
	for _, v := range a {
		sum += v
	}
	return sum / float64(len(a))
}

func unitVector(a []float64) []float64 {
	scale := math.Max(math.Sqrt(dot(a, a)), 1e-12)
	out := make([]float64, len(a))
	for i, v := range a {
		out[i] = v / scale
	}
	return out
}

func zscores(a []float64) []float64 {
	m := mean(a)
	variance := 0.0
	for _, v := range a {
		variance += (v - m) * (v - m)
	}
	scale := math.Max(math.Sqrt(variance/float64(len(a))), 1e-12)
	out := make([]float64, len(a))
	for i, v := range a {
		out[i] = (v - m) / scale
	}
	return out
}

func columnMean(rows [][]float64) []float64 {
	out := make([]float64, len(rows[0]))
	for i := range out {
		for _, row := range rows {
			out[i] += row[i]
		}
		out[i] /= float64(len(rows))
	}
	return out
}

func median(a []float64) float64 {
	v := slices.Sorted(slices.Values(a))
	i := len(v) / 2
	if len(v)%2 == 1 {
		return v[i]
	}
	return (v[i-1] + v[i]) / 2
}

func absInt(n int) int {
	return max(n, -n)
}

func toFloats(numbers []int) []float64 {
	out := make([]float64, len(numbers))
	for i, n := range numbers {
		out[i] = float64(n)
	}
	return out
}

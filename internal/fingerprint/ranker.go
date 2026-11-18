package fingerprint

import (
	"math"
	"slices"
)

// rank blends a frequency ranking (LDA over the first 128 numbers, nearest references and nuisance-corrected
// centroids) with a positional and sequential discriminant; each part is standardized across candidates.
func (d *detector) rank(answers [][]int) []float64 {
	a := &d.Ranker
	bias := d.vector(a.LDABias)
	var lda, base, positional [][]float64
	full := make([][]float64, len(answers))
	for k, numbers := range answers {
		hellinger, ordered := hellingerFeature(numbers), orderedBlockFeature(numbers)
		full[k] = d.joinBlocks([][]float64{hellinger, ordered}, a.FullParams, []float64{0.75, 0.25})
		head := numbers[:min(128, len(numbers))]
		x := d.joinBlocks([][]float64{hellingerFeature(head), orderedBlockFeature(head)}, a.HeadParams, []float64{0.75, 0.25})
		scores := make([]float64, len(a.LDAWeights))
		for i, row := range a.LDAWeights {
			scores[i] = dot(d.vector(row), x) + bias[i]
		}
		lda = append(lda, zscores(scores))
		base = append(base, d.baseline(hellinger, ordered))
		positional = append(positional, zscores(d.positional(numbers, hellinger, ordered)))
	}
	near := make([]float64, len(a.References))
	for m, references := range a.References {
		distances := make([]float64, len(full))
		for k, x := range full {
			distances[k] = -d.nearestDistance(x, references, d.referenceNorms[m])
		}
		near[m] = median(distances)
	}
	ldaScores := zscores(columnMean(lda))
	nearestScores := zscores(near)
	baselineScores := zscores(columnMean(base))
	frequency := make([]float64, len(ldaScores))
	for i := range frequency {
		frequency[i] = 0.5*ldaScores[i] + 0.25*nearestScores[i] + 0.25*baselineScores[i]
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
func (d *detector) baseline(hellinger, orderedBlocks []float64) []float64 {
	h, o := &d.Ranker.Bank.Hellinger, &d.Ranker.Bank.Ordered
	hUnit := unitVector(d.subtractBasis(standardize(hellinger, d.vector(h.Mean), d.vector(h.Scale)), h.Basis))
	marginal := make([]float64, len(h.Centroids))
	for i, row := range h.Centroids {
		marginal[i] = dot(hUnit, d.vector(row))
	}
	centered := standardize(orderedBlocks, d.vector(o.Mean), d.vector(o.Scale))
	raw, projected := unitVector(centered), unitVector(d.subtractBasis(centered, o.Basis))
	templates, nuisance := make([]float64, len(o.Centroids)), make([]float64, len(o.Centroids))
	for i, row := range o.Centroids {
		templates[i] = math.Inf(-1)
		for _, environment := range o.Environments {
			templates[i] = math.Max(templates[i], dot(raw, d.vector(environment[i])))
		}
		nuisance[i] = dot(projected, d.vector(row))
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

func (d *detector) positional(numbers []int, hellinger, ordered []float64) []float64 {
	p := &d.Ranker.Positional
	bias := d.vector(p.Bias)
	x := d.joinBlocks([][]float64{hellinger, ordered, positionKernel(numbers), sequenceFeatures(numbers)},
		p.Params, []float64{0.6, 0.15, 0.125, 0.125})
	scores := make([]float64, len(p.Weights))
	for k, row := range p.Weights {
		scores[k] = bias[k]
		for i, w := range d.vector(row) {
			scores[k] += w * x[i]
		}
	}
	return scores
}

// joinBlocks standardizes each block, scales it to unit length, weights it by the square root of its share
// and concatenates the blocks.
func (d *detector) joinBlocks(blocks [][]float64, params []detectorScaling, shares []float64) []float64 {
	var out []float64
	for j, block := range blocks {
		weight := math.Sqrt(shares[j])
		for _, v := range unitVector(standardize(block, d.vector(params[j].Mean), d.vector(params[j].Scale))) {
			out = append(out, v*weight)
		}
	}
	return out
}

// nearestDistance is the mean squared distance from x to its seven nearest references.
func (d *detector) nearestDistance(x []float64, references []detectorVector, norms []float64) float64 {
	xx := dot(x, x)
	distances := make([]float64, len(references))
	for i, r := range references {
		distances[i] = math.Max(0, xx+norms[i]-2*dot(x, d.vector(r)))
	}
	slices.Sort(distances)
	return mean(distances[:min(7, len(distances))])
}

func standardize(v, center, scale []float64) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		out[i] = (v[i] - center[i]) / scale[i]
	}
	return out
}

// subtractBasis removes the projections of v onto the basis rows.
func (d *detector) subtractBasis(v []float64, basis []detectorVector) []float64 {
	out := make([]float64, len(v))
	for _, ref := range basis {
		row := d.vector(ref)
		coefficient := dot(v, row)
		for i, value := range row {
			out[i] += coefficient * value
		}
	}
	for i, x := range v {
		out[i] = x - out[i]
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

func meanVariance(a []float64) (float64, float64) {
	m := mean(a)
	variance := 0.0
	for _, v := range a {
		variance += (v - m) * (v - m)
	}
	return m, variance / float64(len(a))
}

func zscores(a []float64) []float64 {
	m, variance := meanVariance(a)
	scale := math.Max(math.Sqrt(variance), 1e-12)
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

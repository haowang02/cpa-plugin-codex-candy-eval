package fingerprint

import (
	"cmp"
	"math"
)

func hellingerFeature(numbers []int) []float64 {
	counts := make([]float64, maxValue)
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
			bins[(n-1)*16/maxValue]++
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
				distance := (float64(value) - (1 + float64(k)*(maxValue-1)/23)) / 20
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
	m, variance := meanVariance(x)
	sd := math.Max(math.Sqrt(variance), 1)
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
		out = append(out, correlation/float64(count), distance/float64(count)/maxValue,
			fraction(count, func(i int) bool { return step(i) > 0 }), fraction(count, func(i int) bool { return step(i) == 0 }),
			fraction(count, func(i int) bool { return absInt(step(i)) <= 5 }), fraction(count, func(i int) bool { return absInt(step(i)) <= 20 }))
	}
	bins, digits := make([]int, n), make([]int, n)
	for i, v := range numbers {
		bins[i], digits[i] = (v-1)*8/maxValue, v%10
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
				seen[(v-1)*16/maxValue] = true
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

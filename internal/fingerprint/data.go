package fingerprint

import (
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"slices"
)

//go:embed data/shared_detector.bin
var detectorFile []byte

//go:embed data/models.json
var detectorModelsJSON []byte

//go:embed data/LICENSE
var License string

var Revision = fmt.Sprintf("sha256:%x", sha256.Sum256(detectorFile))

const ChallengeCount = 3

type Challenge struct {
	Instructions  string `json:"instructions,omitempty"`
	Prompt        string `json:"prompt"`
	ExpectedCount int    `json:"expected_count"`
}

//go:embed data/enrollment-suite.json
var suiteJSON []byte

var suite = func() []Challenge {
	var entries []struct {
		System        string `json:"system"`
		UserPrefix    string `json:"user_prefix"`
		Prompt        string `json:"prompt"`
		ExpectedCount int    `json:"expected_count"`
	}
	decodeJSON(suiteJSON, &entries)
	challenges := make([]Challenge, len(entries))
	for i, entry := range entries {
		prompt := entry.Prompt
		if entry.UserPrefix != "" {
			prompt = entry.UserPrefix + "\n\nFinal task:\n" + prompt
		}
		challenges[i] = Challenge{entry.System, prompt, entry.ExpectedCount}
	}
	return challenges
}()

func Challenges() [ChallengeCount]Challenge {
	var challenges [ChallengeCount]Challenge
	for i, index := range rand.Perm(len(suite))[:ChallengeCount] {
		challenges[i] = suite[index]
	}
	return challenges
}

type detectorVector struct {
	Offset int `json:"$f32"`
	Length int `json:"n"`
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
	floats         []float64
	models         []detectorModel
	referenceNorms [][]float64
}

var sharedDetector = decodeDetector(detectorFile, detectorModelsJSON)

// A little-endian header length and padded JSON header precede the float32 byte planes.
func decodeDetector(file, modelsJSON []byte) detector {
	if len(file) < 4 {
		panic("lm-detector: missing detector header")
	}
	headerSize := uint64(binary.LittleEndian.Uint32(file))
	start := (4 + headerSize + 3) &^ 3
	if start > uint64(len(file)) || (uint64(len(file))-start)%4 != 0 {
		panic("lm-detector: truncated detector")
	}
	planes := file[start:]
	count := len(planes) / 4
	var d detector
	decodeJSON(file[4:4+headerSize], &d)
	d.floats = make([]float64, count)
	for i := range d.floats {
		bits := uint32(planes[i]) | uint32(planes[count+i])<<8 | uint32(planes[2*count+i])<<16 | uint32(planes[3*count+i])<<24
		d.floats[i] = float64(math.Float32frombits(bits))
	}
	var bank struct {
		BuiltAt string          `json:"built_at"`
		Models  []detectorModel `json:"models"`
	}
	decodeJSON(modelsJSON, &bank)
	counts := d.vector(d.ResponseCounts)
	if d.Schema != "shared-detector-v2" || bank.BuiltAt != d.BankBuiltAt ||
		len(bank.Models) != len(d.ModelIDs) || len(bank.Models) != len(counts) {
		panic("lm-detector: reference bank does not match the detector")
	}
	for i, m := range bank.Models {
		if m.ID != d.ModelIDs[i] || float64(m.ResponseCount) != counts[i] {
			panic("lm-detector: reference bank does not match the detector")
		}
	}
	c := &d.Calibration
	if c.Schema != "shared-confidence-v2" || c.Method != "ranking-temperature" || !(c.Tau >= 0.001 && c.Tau <= 1000) ||
		c.Binding.BaseSHA256 != d.BaseSHA256 || c.Binding.ReferenceSHA256 != d.ReferenceSHA256 || !slices.Equal(c.Binding.ModelIDs, d.ModelIDs) {
		panic("lm-detector: calibration does not match the detector")
	}
	d.models = bank.Models
	for _, references := range d.Ranker.References {
		norms := make([]float64, len(references))
		for i, r := range references {
			row := d.vector(r)
			norms[i] = dot(row, row)
		}
		d.referenceNorms = append(d.referenceNorms, norms)
	}
	return d
}

func decodeJSON(src []byte, target any) {
	if err := json.Unmarshal(src, target); err != nil {
		panic(fmt.Errorf("lm-detector: %w", err))
	}
}

func (d *detector) vector(ref detectorVector) []float64 {
	if ref.Offset < 0 || ref.Offset > len(d.floats) || ref.Length < 0 || ref.Length > len(d.floats)-ref.Offset {
		panic("lm-detector: float reference out of range")
	}
	end := ref.Offset + ref.Length
	return d.floats[ref.Offset:end:end]
}

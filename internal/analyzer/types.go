// Package analyzer runs the optional offline native music analyzer.
package analyzer

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
)

type Candidate struct {
	Tonic string `json:"tonic"`
	Scale string `json:"scale"`
}
type Result struct {
	Schema          int    `json:"schema_version"`
	AnalyzerVersion string `json:"analyzer_version"`
	ModelVersion    string `json:"model_version"`
	Tempo           struct {
		BPM        *float64 `json:"bpm"`
		Alternate  *float64 `json:"alternate_bpm"`
		Confidence float64  `json:"confidence"`
		Variable   bool     `json:"variable"`
	} `json:"tempo"`
	Key struct {
		Tonic      *string    `json:"tonic"`
		Scale      *string    `json:"scale"`
		Status     string     `json:"status"`
		Alternate  *Candidate `json:"alternate,omitempty"`
		Confidence float64    `json:"confidence,omitempty"`
		Normalized bool       `json:"tuning_normalized"`
	} `json:"key"`
	Tuning struct {
		Reference  *float64 `json:"reference_hz"`
		Cents      *float64 `json:"offset_cents"`
		Status     string   `json:"status"`
		Confidence float64  `json:"confidence,omitempty"`
	} `json:"tuning"`
	Warnings []string `json:"warnings"`
}

func ParseResult(data []byte) (Result, error) {
	var r Result
	if len(data) > 64<<10 {
		return r, fmt.Errorf("analyzer result too large")
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Schema != 1 || r.AnalyzerVersion != Version || r.ModelVersion != ModelsVersion {
		return r, fmt.Errorf("incompatible analyzer result")
	}
	valid := func(n *float64, min, max float64) bool {
		return n == nil || (!math.IsNaN(*n) && !math.IsInf(*n, 0) && *n >= min && *n <= max)
	}
	if !valid(r.Tempo.BPM, 20, 400) || !valid(r.Tempo.Alternate, 10, 800) || !valid(r.Tuning.Reference, 425, 455) || !valid(r.Tuning.Cents, -50, 50) {
		return r, fmt.Errorf("invalid analyzer measurement")
	}
	for _, score := range []float64{r.Tempo.Confidence, r.Key.Confidence, r.Tuning.Confidence} {
		if score < 0 || score > 1 || math.IsNaN(score) || math.IsInf(score, 0) {
			return r, fmt.Errorf("invalid confidence evidence")
		}
	}
	if r.Key.Normalized && r.Tuning.Status != "detuned" {
		return r, fmt.Errorf("key normalized without reliable detuning")
	}
	switch r.Tuning.Status {
	case "standard", "detuned":
		if r.Tuning.Reference == nil || r.Tuning.Cents == nil {
			return r, fmt.Errorf("missing tuning measurement")
		}
		if math.Abs(*r.Tuning.Reference-440*math.Exp2(*r.Tuning.Cents/1200)) > 0.02 {
			return r, fmt.Errorf("inconsistent tuning measurement")
		}
		if r.Tuning.Status == "standard" && (math.Abs(*r.Tuning.Cents) > 1e-6 || math.Abs(*r.Tuning.Reference-440) > 1e-6) {
			return r, fmt.Errorf("inconsistent standard tuning measurement")
		}
	case "unknown", "mixed":
		if r.Tuning.Reference != nil || r.Tuning.Cents != nil {
			return r, fmt.Errorf("unavailable tuning must not contain a measurement")
		}
	default:
		return r, fmt.Errorf("invalid tuning status")
	}
	validKey := func(tonic, scale string) bool {
		switch tonic {
		case "C", "C#", "Db", "D", "D#", "Eb", "E", "F", "F#", "Gb", "G", "G#", "Ab", "A", "A#", "Bb", "B":
		default:
			return false
		}
		return scale == "major" || scale == "minor"
	}
	switch r.Key.Status {
	case "estimated", "uncertain":
		if r.Key.Tonic == nil || r.Key.Scale == nil || !validKey(*r.Key.Tonic, *r.Key.Scale) {
			return r, fmt.Errorf("invalid key")
		}
	case "unavailable":
		if r.Key.Tonic != nil || r.Key.Scale != nil {
			return r, fmt.Errorf("unavailable key has a value")
		}
	default:
		return r, fmt.Errorf("invalid key status")
	}
	if r.Key.Alternate != nil && !validKey(r.Key.Alternate.Tonic, r.Key.Alternate.Scale) {
		return r, fmt.Errorf("invalid alternate key")
	}
	return r, nil
}
func Threads(mode string, cpus int) int {
	if cpus < 1 {
		cpus = runtime.NumCPU()
	}
	if cpus < 1 {
		cpus = 1
	}
	max := 4
	switch mode {
	case "low":
		max = 2
	case "maximum":
		max = cpus - 1
		if max > 16 {
			max = 16
		}
	}
	if max > cpus {
		max = cpus
	}
	if max < 1 {
		max = 1
	}
	return max
}

type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type State struct {
	ID           string  `json:"id"`
	FileID       string  `json:"file_id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Stage        string  `json:"stage"`
	Percent      float64 `json:"percent"`
	Error        string  `json:"error,omitempty"`
	Result       *Result `json:"result,omitempty"`
	Cached       bool    `json:"cached"`
	Ready        bool    `json:"ready"`
	Installing   bool    `json:"installing"`
	InstallError string  `json:"install_error,omitempty"`
}

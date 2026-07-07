package model

import "time"

// Severity ranks how bad a finding is.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityViolation
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityViolation:
		return "violation"
	}
	return "unknown"
}

// Confidence labels how sure the detector is. The tool presents
// evidence; the human renders the verdict — every finding must carry
// this label plus enough Evidence to be checked via --explain.
type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	}
	return "unknown"
}

// Evidence is one verifiable artifact backing a finding: the exact
// prompt line, the exact tool_use input, etc.
type Evidence struct {
	Label     string    `json:"label"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Content   string    `json:"content"`
}

// Finding is one detected failure mode instance.
type Finding struct {
	Detector   string     `json:"detector"`
	Severity   Severity   `json:"-"`
	Confidence Confidence `json:"-"`
	Timestamp  time.Time  `json:"timestamp"`
	Title      string     `json:"title"`
	Detail     string     `json:"detail,omitempty"`
	Evidence   []Evidence `json:"evidence,omitempty"`

	// TokensEst is the estimated token cost attributable to the
	// finding (rework loops). Zero when not applicable. Always an
	// estimate — renderers must label it as such.
	TokensEst int `json:"tokens_est,omitempty"`

	// EventUUIDs are the events implicated by this finding, used to
	// compute the "N events clean" count.
	EventUUIDs []string `json:"-"`
}

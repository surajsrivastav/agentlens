// Package render turns a session + findings into terminal, HTML, or
// JSON output. Rendering is deliberately minimal — the detectors are
// the product; this layer just presents evidence.
package render

import (
	"sort"

	"github.com/surajsrivastav/agentlens/model"
)

// Report is the assembled analysis result all renderers consume.
type Report struct {
	Session     *model.Session
	Findings    []model.Finding
	CleanEvents int
	Warnings    []string // ingest diagnostics: drift, subagents, compaction
	Explain     bool     // include full evidence blocks
	Version     string   // agentlens version
}

// Build assembles a Report: sorts findings by severity then time and
// computes the clean-event count.
func Build(s *model.Session, findings []model.Finding, warnings []string, toolVersion string) *Report {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		return findings[i].Timestamp.Before(findings[j].Timestamp)
	})

	implicated := map[string]bool{}
	for _, f := range findings {
		for _, id := range f.EventUUIDs {
			if id != "" {
				implicated[id] = true
			}
		}
	}
	clean := 0
	seen := map[string]bool{}
	for _, e := range s.Events {
		key := e.UUID
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if !implicated[key] {
			clean++
		}
	}

	return &Report{
		Session:     s,
		Findings:    findings,
		CleanEvents: clean,
		Warnings:    warnings,
		Version:     toolVersion,
	}
}

// section groups findings per detector, in report order.
type section struct {
	Icon, Title, Detector string
	Findings              []model.Finding
}

func (r *Report) sections() []section {
	defs := []section{
		{Icon: "⛔", Title: "INSTRUCTION VIOLATIONS", Detector: "instruction-violation"},
		{Icon: "⚠️", Title: "TEST INTEGRITY", Detector: "test-integrity"},
		{Icon: "🔁", Title: "REWORK LOOPS", Detector: "rework-loop"},
	}
	known := map[string]int{}
	for i, d := range defs {
		known[d.Detector] = i
	}
	var extra []section
	for _, f := range r.Findings {
		if i, ok := known[f.Detector]; ok {
			defs[i].Findings = append(defs[i].Findings, f)
			continue
		}
		placed := false
		for j := range extra {
			if extra[j].Detector == f.Detector {
				extra[j].Findings = append(extra[j].Findings, f)
				placed = true
				break
			}
		}
		if !placed {
			extra = append(extra, section{Icon: "🔎", Title: f.Detector, Detector: f.Detector, Findings: []model.Finding{f}})
		}
	}
	var out []section
	for _, d := range defs {
		if len(d.Findings) > 0 {
			out = append(out, d)
		}
	}
	return append(out, extra...)
}

// funnelFooter is the gitwhy funnel line (PRD §8).
const funnelFooter = "Want this recorded permanently in git history? → gitwhy"

package render

import (
	"encoding/json"
	"io"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// jsonReport is the stable machine-readable schema (for CI and
// downstream tooling). Additive changes only after v0.1.
type jsonReport struct {
	Tool         string        `json:"tool"`
	ToolVersion  string        `json:"tool_version"`
	Agent        string        `json:"agent"`
	SessionID    string        `json:"session_id"`
	SessionPath  string        `json:"session_path"`
	CWD          string        `json:"cwd,omitempty"`
	GitBranch    string        `json:"git_branch,omitempty"`
	Start        *time.Time    `json:"start,omitempty"`
	End          *time.Time    `json:"end,omitempty"`
	Events       int           `json:"events"`
	CleanEvents  int           `json:"clean_events"`
	Tokens       tokensJSON    `json:"tokens"`
	Findings     []findingJSON `json:"findings"`
	Warnings     []string      `json:"warnings,omitempty"`
	Diagnostics  diagJSON      `json:"diagnostics"`
	AgentVersion []string      `json:"agent_versions,omitempty"`
}

type tokensJSON struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheCreation int `json:"cache_creation"`
	CacheRead     int `json:"cache_read"`
	Fresh         int `json:"fresh"`
}

type findingJSON struct {
	Detector   string           `json:"detector"`
	Severity   string           `json:"severity"`
	Confidence string           `json:"confidence"`
	Timestamp  *time.Time       `json:"timestamp,omitempty"`
	Title      string           `json:"title"`
	Detail     string           `json:"detail,omitempty"`
	TokensEst  int              `json:"tokens_est,omitempty"`
	Evidence   []model.Evidence `json:"evidence,omitempty"`
}

type diagJSON struct {
	UnrecognizedTypes map[string]int `json:"unrecognized_types,omitempty"`
	MalformedLines    int            `json:"malformed_lines,omitempty"`
	SidechainEvents   int            `json:"sidechain_events,omitempty"`
	Compacted         bool           `json:"compacted,omitempty"`
}

// JSON writes the machine-readable report.
func JSON(w io.Writer, r *Report) error {
	s := r.Session
	out := jsonReport{
		Tool:        "agentlens",
		ToolVersion: r.Version,
		Agent:       s.Agent,
		SessionID:   s.ID,
		SessionPath: s.Path,
		CWD:         s.CWD,
		GitBranch:   s.GitBranch,
		Events:      len(s.Events),
		CleanEvents: r.CleanEvents,
		Tokens: tokensJSON{
			Input: s.TotalUsage.InputTokens, Output: s.TotalUsage.OutputTokens,
			CacheCreation: s.TotalUsage.CacheCreationTokens, CacheRead: s.TotalUsage.CacheReadTokens,
			Fresh: s.TotalUsage.Fresh(),
		},
		Warnings: r.Warnings,
		Diagnostics: diagJSON{
			UnrecognizedTypes: s.Unrecognized,
			MalformedLines:    s.MalformedLines,
			SidechainEvents:   s.SidechainEvents,
			Compacted:         s.Compacted,
		},
		AgentVersion: s.Versions,
		Findings:     []findingJSON{},
	}
	if !s.Start.IsZero() {
		out.Start = &s.Start
	}
	if !s.End.IsZero() {
		out.End = &s.End
	}
	for _, f := range r.Findings {
		fj := findingJSON{
			Detector: f.Detector, Severity: f.Severity.String(), Confidence: f.Confidence.String(),
			Title: f.Title, Detail: f.Detail, TokensEst: f.TokensEst, Evidence: f.Evidence,
		}
		if !f.Timestamp.IsZero() {
			t := f.Timestamp
			fj.Timestamp = &t
		}
		out.Findings = append(out.Findings, fj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

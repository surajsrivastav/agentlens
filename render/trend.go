package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// TrendRow is one session's contribution to a cross-session trend
// report — the aggregate view PRD roadmap calls `agentlens trend`.
type TrendRow struct {
	SessionID  string
	Agent      string
	Path       string
	Start      time.Time
	Duration   time.Duration
	Events     int
	Tokens     int
	Counts     map[string]int // detector name -> finding count
	ParseError string         // set instead of the above if parsing failed
}

// TrendReport is the assembled multi-session view.
type TrendReport struct {
	Rows     []TrendRow
	Totals   map[string]int
	Warnings []string
	Version  string
}

// BuildTrend sorts rows oldest-first (so the table reads as a
// timeline) and computes per-detector totals.
func BuildTrend(rows []TrendRow, warnings []string, version string) *TrendReport {
	sorted := append([]TrendRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start.IsZero() != sorted[j].Start.IsZero() {
			return sorted[j].Start.IsZero()
		}
		return sorted[i].Start.Before(sorted[j].Start)
	})
	totals := map[string]int{}
	for _, r := range sorted {
		for k, v := range r.Counts {
			totals[k] += v
		}
	}
	return &TrendReport{Rows: sorted, Totals: totals, Warnings: warnings, Version: version}
}

// TrendTerminal renders the compact multi-session table.
func TrendTerminal(w io.Writer, r *TrendReport) {
	c := colors(w)
	fmt.Fprintf(w, "\n%sAGENTLENS TREND%s — %d session(s)\n\n", c.bold, c.reset, len(r.Rows))

	fmt.Fprintf(w, "%-12s %-10s %6s %7s %8s  ⛔  ⚠️  🔁  👻\n", "DATE", "AGENT", "DUR", "EVENTS", "TOKENS")
	for _, row := range r.Rows {
		if row.ParseError != "" {
			fmt.Fprintf(w, "%-12s %-10s %s(unparseable: %s)%s\n", "-", row.Agent, c.dim, oneLine(row.ParseError), c.reset)
			continue
		}
		date := "-"
		if !row.Start.IsZero() {
			date = row.Start.Local().Format("2006-01-02")
		}
		fmt.Fprintf(w, "%-12s %-10s %6s %7d %8s  %2d  %2d  %2d  %2d\n",
			date, row.Agent, humanDurationValue(row.Duration), row.Events, humanK(row.Tokens),
			row.Counts["instruction-violation"], row.Counts["test-integrity"],
			row.Counts["rework-loop"], row.Counts["hallucinated-api"])
	}

	fmt.Fprintf(w, "\n%sTOTALS%s  ⛔ %d instruction violations · ⚠️ %d test integrity · 🔁 %d rework loops · 👻 %d hallucinated APIs\n",
		c.bold, c.reset, r.Totals["instruction-violation"], r.Totals["test-integrity"], r.Totals["rework-loop"], r.Totals["hallucinated-api"])

	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "%s⚠ %s%s\n", c.dim, warning, c.reset)
	}
	fmt.Fprintf(w, "\n%s%s%s\n", c.dim, funnelFooter, c.reset)
}

func humanDurationValue(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	if m := int(d.Minutes()); m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

type trendRowJSON struct {
	SessionID  string         `json:"session_id"`
	Agent      string         `json:"agent"`
	Start      *time.Time     `json:"start,omitempty"`
	DurationS  float64        `json:"duration_seconds,omitempty"`
	Events     int            `json:"events,omitempty"`
	Tokens     int            `json:"tokens,omitempty"`
	Findings   map[string]int `json:"findings,omitempty"`
	ParseError string         `json:"parse_error,omitempty"`
}

type trendJSON struct {
	Tool        string         `json:"tool"`
	ToolVersion string         `json:"tool_version"`
	Sessions    []trendRowJSON `json:"sessions"`
	Totals      map[string]int `json:"totals"`
	Warnings    []string       `json:"warnings,omitempty"`
}

// TrendJSON writes the machine-readable multi-session report.
func TrendJSON(w io.Writer, r *TrendReport) error {
	out := trendJSON{Tool: "agentlens", ToolVersion: r.Version, Totals: r.Totals, Warnings: r.Warnings, Sessions: []trendRowJSON{}}
	for _, row := range r.Rows {
		rj := trendRowJSON{
			SessionID: row.SessionID, Agent: row.Agent, Events: row.Events, Tokens: row.Tokens,
			ParseError: row.ParseError, Findings: row.Counts,
		}
		if !row.Start.IsZero() {
			t := row.Start
			rj.Start = &t
		}
		if row.Duration > 0 {
			rj.DurationS = row.Duration.Seconds()
		}
		out.Sessions = append(out.Sessions, rj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildTrendSortsOldestFirst(t *testing.T) {
	rows := []TrendRow{
		{SessionID: "b", Start: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), Counts: map[string]int{"rework-loop": 1}},
		{SessionID: "a", Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Counts: map[string]int{"instruction-violation": 2}},
		{SessionID: "c", ParseError: "boom"}, // zero Start — should sort last
	}
	r := BuildTrend(rows, nil, "test")
	if len(r.Rows) != 3 || r.Rows[0].SessionID != "a" || r.Rows[1].SessionID != "b" || r.Rows[2].SessionID != "c" {
		t.Fatalf("expected order [a b c], got %v", ids(r.Rows))
	}
	if r.Totals["instruction-violation"] != 2 || r.Totals["rework-loop"] != 1 {
		t.Errorf("totals wrong: %+v", r.Totals)
	}
}

func ids(rows []TrendRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.SessionID
	}
	return out
}

func TestTrendTerminalRendersParseErrorsAndTotals(t *testing.T) {
	rows := []TrendRow{
		{SessionID: "s1", Agent: "claudecode", Start: time.Now(), Events: 10, Tokens: 500, Counts: map[string]int{"rework-loop": 2}},
		{SessionID: "s2", Agent: "cursor", ParseError: "no recognizable chat data"},
	}
	r := BuildTrend(rows, []string{"cursor is experimental"}, "test")
	var buf bytes.Buffer
	TrendTerminal(&buf, r)
	out := buf.String()
	for _, want := range []string{"AGENTLENS TREND", "2 session(s)", "unparseable", "TOTALS", "cursor is experimental", "gitwhy"} {
		if !strings.Contains(out, want) {
			t.Errorf("trend terminal missing %q\n---\n%s", want, out)
		}
	}
}

func TestTrendJSONSchema(t *testing.T) {
	rows := []TrendRow{
		{SessionID: "s1", Agent: "claudecode", Start: time.Now(), Duration: 5 * time.Minute, Events: 10, Tokens: 500, Counts: map[string]int{"rework-loop": 2}},
	}
	r := BuildTrend(rows, nil, "test")
	var buf bytes.Buffer
	if err := TrendJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tool     string `json:"tool"`
		Sessions []struct {
			SessionID string         `json:"session_id"`
			Findings  map[string]int `json:"findings"`
		} `json:"sessions"`
		Totals map[string]int `json:"totals"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Tool != "agentlens" || len(out.Sessions) != 1 || out.Sessions[0].Findings["rework-loop"] != 2 {
		t.Errorf("unexpected JSON: %+v", out)
	}
	if out.Totals["rework-loop"] != 2 {
		t.Errorf("totals: %+v", out.Totals)
	}
}

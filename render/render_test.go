package render

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/surajsrivastav/agentlens/detect"
	"github.com/surajsrivastav/agentlens/ingest/claudecode"
	"github.com/surajsrivastav/agentlens/model"
)

func report(t *testing.T) *Report {
	t.Helper()
	s, err := claudecode.ParseFile(filepath.Join("..", "fixtures", "v2.1.x", "kitchen-sink.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var findings []model.Finding
	for _, d := range detect.All() {
		findings = append(findings, d.Scan(s)...)
	}
	return Build(s, findings, []string{"1 subagent events detected, not analyzed (v0.2)"}, "test")
}

func TestTerminal(t *testing.T) {
	r := report(t)
	var buf bytes.Buffer
	Terminal(&buf, r)
	out := buf.String()
	for _, want := range []string{
		"AGENTLENS REPORT",
		"INSTRUCTION VIOLATIONS (1)",
		"TEST INTEGRITY (1)",
		"REWORK LOOPS (1)",
		"events clean",
		"gitwhy",
		"subagent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output missing %q\n---\n%s", want, out)
		}
	}
}

func TestTerminalExplainShowsEvidence(t *testing.T) {
	r := report(t)
	r.Explain = true
	var buf bytes.Buffer
	Terminal(&buf, r)
	if !strings.Contains(buf.String(), "your directive") {
		t.Error("--explain output should include evidence labels")
	}
}

func TestJSONSchema(t *testing.T) {
	r := report(t)
	var buf bytes.Buffer
	if err := JSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tool        string `json:"tool"`
		SessionID   string `json:"session_id"`
		Events      int    `json:"events"`
		CleanEvents int    `json:"clean_events"`
		Findings    []struct {
			Detector   string `json:"detector"`
			Severity   string `json:"severity"`
			Confidence string `json:"confidence"`
		} `json:"findings"`
		Diagnostics struct {
			SidechainEvents int `json:"sidechain_events"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Tool != "agentlens" || out.SessionID != "fix-kitchen" {
		t.Errorf("header wrong: %+v", out)
	}
	if len(out.Findings) != 3 {
		t.Errorf("findings = %d, want 3", len(out.Findings))
	}
	if out.Diagnostics.SidechainEvents != 1 {
		t.Errorf("sidechain diagnostics missing")
	}
	if out.CleanEvents <= 0 || out.CleanEvents >= out.Events {
		t.Errorf("clean events %d/%d looks wrong", out.CleanEvents, out.Events)
	}
}

func TestHTMLSelfContained(t *testing.T) {
	r := report(t)
	var buf bytes.Buffer
	if err := HTML(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"<!DOCTYPE html>", "agentlens report", "Instruction violations", "100% local"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	for _, forbidden := range []string{"http://", "https://cdn", "<script src"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("HTML must be self-contained, found %q", forbidden)
		}
	}
}

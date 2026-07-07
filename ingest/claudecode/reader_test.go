package claudecode

import (
	"path/filepath"
	"testing"

	"github.com/surajsrivastav/agentlens/model"
)

func fixture(t *testing.T, name string) *model.Session {
	t.Helper()
	s, err := ParseFile(filepath.Join("..", "..", "fixtures", "v2.1.x", name))
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", name, err)
	}
	return s
}

func TestParseKitchenSink(t *testing.T) {
	s := fixture(t, "kitchen-sink.jsonl")

	if s.ID != "fix-kitchen" {
		t.Errorf("ID = %q, want fix-kitchen", s.ID)
	}
	if s.CWD != "/Users/dev/myrepo" {
		t.Errorf("CWD = %q", s.CWD)
	}
	if s.GitBranch != "main" {
		t.Errorf("GitBranch = %q", s.GitBranch)
	}
	if s.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", s.MalformedLines)
	}
	if s.SidechainEvents != 1 {
		t.Errorf("SidechainEvents = %d, want 1", s.SidechainEvents)
	}
	for _, typ := range []string{"queue-operation", "attachment", "wizard-hat"} {
		if s.Unrecognized[typ] == 0 {
			t.Errorf("Unrecognized[%s] = 0, want ≥1", typ)
		}
	}
	if len(s.Versions) != 1 || s.Versions[0] != "2.1.177" {
		t.Errorf("Versions = %v", s.Versions)
	}

	var prompts, toolCalls, toolResults int
	for _, e := range s.Events {
		switch e.Kind {
		case model.KindPrompt:
			prompts++
		case model.KindToolCall:
			toolCalls++
		case model.KindToolResult:
			toolResults++
		}
	}
	if prompts != 1 {
		t.Errorf("prompts = %d, want 1", prompts)
	}
	if toolCalls != 7 { // 6 edits + 1 bash
		t.Errorf("toolCalls = %d, want 7", toolCalls)
	}
	if toolResults != 5 {
		t.Errorf("toolResults = %d, want 5", toolResults)
	}

	// msg_1 spans two JSONL lines with identical usage — it must be
	// attributed exactly once.
	wantOutput := 500 + 800 + 100 + 900 + 20000 + 300 + 15000
	if s.TotalUsage.OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d (usage double-counted across streamed lines?)", s.TotalUsage.OutputTokens, wantOutput)
	}

	if s.Duration() <= 0 {
		t.Error("Duration should be positive")
	}
}

func TestParseCompaction(t *testing.T) {
	s := fixture(t, "compaction.jsonl")
	if !s.Compacted {
		t.Fatal("Compacted = false, want true")
	}
	found := false
	for _, e := range s.Events {
		if e.Kind == model.KindCompaction {
			found = true
		}
	}
	if !found {
		t.Error("no KindCompaction event emitted for compact_boundary")
	}
}

func TestLongLinesSurviveScanner(t *testing.T) {
	// Regression guard for the 64KB default-scanner trap: tool results
	// embedding whole files produce very long lines.
	s := fixture(t, "kitchen-sink.jsonl")
	_ = s // parse not erroring on the fixture is the baseline

	long := `{"type":"user","uuid":"u9","sessionId":"long","version":"2.1.177","timestamp":"2026-07-06T10:00:00.000Z","message":{"role":"user","content":"` +
		string(make200KB()) + `"}}`
	sess, err := Parse(stringsReader(long))
	if err != nil {
		t.Fatalf("Parse long line: %v", err)
	}
	if len(sess.Events) != 1 || sess.Events[0].Kind != model.KindPrompt {
		t.Fatalf("long-line prompt not parsed: %+v", sess.Events)
	}
}

func make200KB() []byte {
	b := make([]byte, 200*1024)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

func TestIsRealPromptFiltersHarnessNoise(t *testing.T) {
	cases := map[string]bool{
		"fix the login bug":                         true,
		"<command-name>/compact</command-name>":     false,
		"Caveat: the messages below were generated": false,
		"[Request interrupted by user]":             false,
		"   ":                                       false,
	}
	for in, want := range cases {
		if got := isRealPrompt(in); got != want {
			t.Errorf("isRealPrompt(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestUntestedVersions(t *testing.T) {
	if got := UntestedVersions([]string{"2.1.177", "2.1.9"}); len(got) != 0 {
		t.Errorf("2.1.x should be tested, got untested %v", got)
	}
	if got := UntestedVersions([]string{"2.1.177", "3.0.1"}); len(got) != 1 || got[0] != "3.0.1" {
		t.Errorf("want [3.0.1] untested, got %v", got)
	}
	if VersionWarning([]string{"3.0.1"}) == "" {
		t.Error("expected a drift warning for 3.0.1")
	}
}

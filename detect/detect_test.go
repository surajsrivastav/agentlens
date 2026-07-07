package detect

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/surajsrivastav/agentlens/ingest/claudecode"
	"github.com/surajsrivastav/agentlens/model"
)

func fixture(t *testing.T, name string) *model.Session {
	t.Helper()
	s, err := claudecode.ParseFile(filepath.Join("..", "fixtures", "v2.1.x", name))
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", name, err)
	}
	return s
}

func scanAll(s *model.Session) []model.Finding {
	var out []model.Finding
	for _, d := range All() {
		out = append(out, d.Scan(s)...)
	}
	return out
}

func byDetector(fs []model.Finding, name string) []model.Finding {
	var out []model.Finding
	for _, f := range fs {
		if f.Detector == name {
			out = append(out, f)
		}
	}
	return out
}

func TestKitchenSinkFindings(t *testing.T) {
	s := fixture(t, "kitchen-sink.jsonl")
	findings := scanAll(s)

	// D1: exactly one violation, on config.yaml, high confidence.
	viol := byDetector(findings, "instruction-violation")
	if len(viol) != 1 {
		t.Fatalf("instruction violations = %d, want 1: %+v", len(viol), viol)
	}
	if !strings.Contains(viol[0].Title, "Do not modify config.yaml") {
		t.Errorf("violation should quote the directive, got %q", viol[0].Title)
	}
	if !strings.Contains(viol[0].Detail, "config.yaml") {
		t.Errorf("violation detail should name the file, got %q", viol[0].Detail)
	}
	if viol[0].Confidence != model.ConfidenceHigh {
		t.Errorf("confidence = %v, want high", viol[0].Confidence)
	}
	if len(viol[0].Evidence) < 2 {
		t.Errorf("violation must carry directive + action evidence, got %d", len(viol[0].Evidence))
	}

	// D2: assertion removal + skip annotation on auth_test.go,
	// upgraded to violation by "make the tests pass" context.
	ti := byDetector(findings, "test-integrity")
	if len(ti) != 1 {
		t.Fatalf("test-integrity findings = %d, want 1: %+v", len(ti), ti)
	}
	if !strings.Contains(ti[0].Title, "auth_test.go") {
		t.Errorf("title = %q", ti[0].Title)
	}
	if !strings.Contains(ti[0].Title, "2 assertion(s) removed") {
		t.Errorf("expected 2 assertions removed, got %q", ti[0].Title)
	}
	if !strings.Contains(ti[0].Title, "skip/disable annotation(s) added") {
		t.Errorf("expected skip annotation finding, got %q", ti[0].Title)
	}
	if ti[0].Severity != model.SeverityViolation {
		t.Errorf("severity = %v, want violation (make-tests-pass context)", ti[0].Severity)
	}

	// D3: auth.go rewritten 3× with an A→B→A oscillation.
	rw := byDetector(findings, "rework-loop")
	if len(rw) != 1 {
		t.Fatalf("rework findings = %d, want 1: %+v", len(rw), rw)
	}
	if !strings.Contains(rw[0].Title, "auth.go rewritten 3×") {
		t.Errorf("title = %q", rw[0].Title)
	}
	if rw[0].Severity != model.SeverityWarning {
		t.Errorf("oscillation should be warning severity, got %v", rw[0].Severity)
	}
	if !strings.Contains(rw[0].Detail, "A→B→A") {
		t.Errorf("detail should mention oscillation, got %q", rw[0].Detail)
	}
	if rw[0].TokensEst <= 0 {
		t.Error("rework should estimate token cost")
	}
}

func TestCleanSessionHasNoFindings(t *testing.T) {
	s := fixture(t, "clean.jsonl")
	if findings := scanAll(s); len(findings) != 0 {
		t.Fatalf("clean session produced findings: %+v", findings)
	}
}

func TestCompactionDowngradesConfidence(t *testing.T) {
	s := fixture(t, "compaction.jsonl")
	viol := byDetector(scanAll(s), "instruction-violation")
	if len(viol) != 1 {
		t.Fatalf("violations = %d, want 1", len(viol))
	}
	if viol[0].Confidence != model.ConfidenceMedium {
		t.Errorf("pre-compaction directive should downgrade to medium, got %v", viol[0].Confidence)
	}
	if !strings.Contains(viol[0].Detail, "compaction") {
		t.Errorf("detail should explain the downgrade, got %q", viol[0].Detail)
	}
}

func ts(min int) time.Time {
	return time.Date(2026, 7, 6, 14, min, 0, 0, time.UTC)
}

func prompt(min int, text string) model.Event {
	return model.Event{Kind: model.KindPrompt, Timestamp: ts(min), UUID: "p", Text: text}
}

func edit(min int, path, old, new_ string) model.Event {
	return model.Event{Kind: model.KindToolCall, Tool: "Edit", Timestamp: ts(min), UUID: "e", FilePath: path, OldString: old, NewString: new_}
}

func TestSupersessionBeatsAccusation(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		prompt(0, "Don't touch config.yaml for now."),
		prompt(5, "Actually, go ahead and update config.yaml."),
		edit(10, "/repo/config.yaml", "a", "b"),
	}}
	if v := byDetector((&Instructions{}).Scan(s), "instruction-violation"); len(v) != 0 {
		t.Fatalf("superseded directive must not accuse: %+v", v)
	}
}

func TestViolationBeforeSupersessionStillCounts(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		prompt(0, "Don't touch config.yaml."),
		edit(2, "/repo/config.yaml", "a", "b"),
		prompt(5, "Fine, config.yaml is ok now."),
	}}
	if v := (&Instructions{}).Scan(s); len(v) != 1 {
		t.Fatalf("violation before supersession should count once, got %d", len(v))
	}
}

func TestExclusiveDirective(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		prompt(0, "Only modify auth.go please."),
		edit(2, "/repo/auth.go", "a", "b"),
		edit(3, "/repo/billing.go", "x", "y"),
		{Kind: model.KindToolCall, Tool: "Write", Timestamp: ts(4), UUID: "w", FilePath: "/repo/newfile.go", NewString: "package x"},
	}}
	v := (&Instructions{}).Scan(s)
	if len(v) != 1 {
		t.Fatalf("want 1 exclusive violation (billing.go only; Write of new files exempt), got %d: %+v", len(v), v)
	}
	if v[0].Confidence != model.ConfidenceMedium {
		t.Errorf("exclusive violations are medium confidence, got %v", v[0].Confidence)
	}
}

func TestBashMutationViolation(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		prompt(0, "Do not delete legacy/init.sql."),
		{Kind: model.KindToolCall, Tool: "Bash", Timestamp: ts(3), UUID: "b", Command: "rm legacy/init.sql"},
	}}
	v := (&Instructions{}).Scan(s)
	if len(v) != 1 {
		t.Fatalf("want 1 bash violation, got %d", len(v))
	}
	if v[0].Confidence != model.ConfidenceMedium {
		t.Errorf("bash violations are medium confidence, got %v", v[0].Confidence)
	}
}

func TestReadingFencedFileIsNotViolation(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		prompt(0, "Don't modify config.yaml."),
		{Kind: model.KindToolCall, Tool: "Read", Timestamp: ts(1), UUID: "r", FilePath: "/repo/config.yaml"},
		{Kind: model.KindToolCall, Tool: "Bash", Timestamp: ts(2), UUID: "b", Command: "cat config.yaml"},
	}}
	if v := (&Instructions{}).Scan(s); len(v) != 0 {
		t.Fatalf("reading a fenced file is not a violation: %+v", v)
	}
}

func TestPathMatches(t *testing.T) {
	cases := []struct {
		user, tool string
		want       bool
	}{
		{"config.yaml", "/repo/config.yaml", true},
		{"config.yaml", "/repo/sub/config.yaml", true},
		{"config.yaml", "/repo/config.yaml.bak", false},
		{"src/db/", "/repo/src/db/schema.sql", true},
		{"src/db/", "/repo/src/dbx/other.sql", false},
		{"*.sql", "/repo/migrations/001.sql", true},
		{"sub/config.yaml", "/repo/sub/config.yaml", true},
		{"sub/config.yaml", "/repo/other/config.yaml", false},
	}
	for _, c := range cases {
		if got := pathMatches(c.user, c.tool); got != c.want {
			t.Errorf("pathMatches(%q, %q) = %v, want %v", c.user, c.tool, got, c.want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	yes := []string{"auth_test.go", "pkg/test_auth.py", "src/foo.test.ts", "a/b.spec.js", "__tests__/x.js", "spec/user_spec.rb", "src/FooTest.java"}
	no := []string{"auth.go", "test.md", "contest.py", "src/foo.ts"}
	for _, p := range yes {
		if !IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true, want false", p)
		}
	}
}

func TestAssertionWeakening(t *testing.T) {
	s := &model.Session{Events: []model.Event{
		edit(2, "/repo/user_test.py",
			"def test_total():\n    self.assertEqual(total(), 100)",
			"def test_total():\n    self.assertTrue(total())"),
	}}
	fs := (&TestIntegrity{}).Scan(s)
	if len(fs) != 1 {
		t.Fatalf("want 1 weakening finding, got %d", len(fs))
	}
	if !strings.Contains(fs[0].Title, "assertEqual → assertTrue") {
		t.Errorf("title = %q", fs[0].Title)
	}
}

func TestSequentialEditsAreOneEpisode(t *testing.T) {
	// Building a file with several consecutive edits is not rework.
	s := &model.Session{Events: []model.Event{
		edit(1, "/repo/a.go", "1", "2"),
		edit(2, "/repo/a.go", "2", "3"),
		edit(3, "/repo/a.go", "3", "4"),
		edit(4, "/repo/a.go", "4", "5"),
	}}
	if fs := (&Rework{}).Scan(s); len(fs) != 0 {
		t.Fatalf("consecutive edits flagged as rework: %+v", fs)
	}
}

func TestSimilarity(t *testing.T) {
	if similarity("abc", "abc") != 1 {
		t.Error("identical strings should be 1")
	}
	if s := similarity("abc", "xyz"); s > 0.1 {
		t.Errorf("disjoint strings should be ~0, got %f", s)
	}
	if s := similarity("the quick brown fox jumps", "the quick brown fox jumped"); s < 0.9 {
		t.Errorf("near-identical should be >0.9, got %f", s)
	}
}

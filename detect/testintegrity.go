package detect

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// TestIntegrity (D2) inspects edits to test files and classifies test
// deletion, skip/disable annotations, assertion removal, and assertion
// weakening. Evidence is the literal tool input, so every finding is
// verifiable. Severity is raised when the edit happens near "make the
// tests pass" context.
type TestIntegrity struct{}

func (d *TestIntegrity) Name() string { return "test-integrity" }

var testFilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`_test\.go$`),
	regexp.MustCompile(`(^|/)test_[^/]+\.py$`),
	regexp.MustCompile(`_test\.py$`),
	regexp.MustCompile(`\.test\.(ts|tsx|js|jsx|mjs|cjs)$`),
	regexp.MustCompile(`\.spec\.[^/]+$`),
	regexp.MustCompile(`(^|/)(tests?|__tests__|spec)/`),
	regexp.MustCompile(`Test\.(java|kt|scala)$`),
	regexp.MustCompile(`_spec\.rb$`),
}

// IsTestFile reports whether a path matches common test conventions.
func IsTestFile(path string) bool {
	p := filepath.ToSlash(path)
	for _, re := range testFilePatterns {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

var skipAnnotations = []*regexp.Regexp{
	regexp.MustCompile(`\bt\.Skip\w*\(`),
	regexp.MustCompile(`@pytest\.mark\.skip`),
	regexp.MustCompile(`@unittest\.skip`),
	regexp.MustCompile(`\bpytest\.skip\(`),
	regexp.MustCompile(`\bxit\(`),
	regexp.MustCompile(`\bxdescribe\(`),
	regexp.MustCompile(`\b(it|test|describe)\.skip\(`),
	regexp.MustCompile(`@Disabled\b`),
	regexp.MustCompile(`@Ignore\b`),
	regexp.MustCompile(`#\[ignore\]`),
}

var assertionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bt\.(Error|Errorf|Fatal|Fatalf|Fail)\b`),
	regexp.MustCompile(`\b(assert|require)\.\w+\(`),
	regexp.MustCompile(`^\s*assert\b`),
	regexp.MustCompile(`\bself\.assert\w+\(`),
	regexp.MustCompile(`\bexpect\(`),
	regexp.MustCompile(`\b(EXPECT|ASSERT)_\w+\(`),
}

var testDeclPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bfunc Test\w+\(`),
	regexp.MustCompile(`\bdef test_\w+\(`),
	regexp.MustCompile(`\b(it|test)\(\s*['"` + "`" + `]`),
	regexp.MustCompile(`@Test\b`),
}

// weakenings maps a strong assertion form to the weaker forms it is
// commonly downgraded to.
var weakenings = []struct {
	strong, weak *regexp.Regexp
	desc         string
}{
	{regexp.MustCompile(`\bassertEqual\(`), regexp.MustCompile(`\bassertTrue\(`), "assertEqual → assertTrue"},
	{regexp.MustCompile(`\bassertEquals\(`), regexp.MustCompile(`\bassertTrue\(`), "assertEquals → assertTrue"},
	{regexp.MustCompile(`\.toEqual\(`), regexp.MustCompile(`\.(toBeTruthy|toBeDefined)\(`), "toEqual → toBeTruthy/toBeDefined"},
	{regexp.MustCompile(`\.toStrictEqual\(`), regexp.MustCompile(`\.toEqual\(`), "toStrictEqual → toEqual"},
	{regexp.MustCompile(`\b(assert|require)\.Equal\(`), regexp.MustCompile(`\b(assert|require)\.(NotNil|True)\(`), "Equal → NotNil/True"},
}

var makeTestsPassRe = regexp.MustCompile(`(?i)(make|get|force|just get).{0,40}(tests?|ci|build).{0,20}(pass|green)|fix.{0,30}(failing|broken).{0,10}tests?|tests?.{0,20}(pass|green)`)

func (d *TestIntegrity) Scan(s *model.Session) []model.Finding {
	var findings []model.Finding
	for i := range s.Events {
		ev := &s.Events[i]

		// Bash deletion of a test file.
		if ev.Kind == model.KindToolCall && ev.Tool == "Bash" {
			if f := bashTestDeletion(ev); f != nil {
				findings = append(findings, *f)
			}
			continue
		}

		if !ev.IsFileEdit() || !IsTestFile(ev.FilePath) {
			continue
		}
		old, new_ := combinedDiff(ev)
		if old == "" {
			// Write with no prior content visible — cannot diff; skip
			// rather than guess (precision over recall).
			continue
		}

		var reasons []string
		var severity = model.SeverityInfo

		if n := countDrop(testDeclPatterns, old, new_); n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d test(s) removed", n))
			severity = model.SeverityWarning
		}
		if n := countRise(skipAnnotations, old, new_); n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d skip/disable annotation(s) added", n))
			severity = model.SeverityWarning
		}
		if n := countDrop(assertionPatterns, old, new_); n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d assertion(s) removed", n))
			if severity < model.SeverityWarning {
				severity = model.SeverityWarning
			}
		}
		for _, w := range weakenings {
			if len(w.strong.FindAllStringIndex(old, -1)) > len(w.strong.FindAllStringIndex(new_, -1)) &&
				len(w.weak.FindAllStringIndex(new_, -1)) > len(w.weak.FindAllStringIndex(old, -1)) {
				reasons = append(reasons, "assertion weakened ("+w.desc+")")
				severity = model.SeverityWarning
			}
		}
		if len(reasons) == 0 {
			continue
		}

		confidence := model.ConfidenceHigh
		detail := ""
		if ctx := nearbyPassPressure(s, i); ctx != "" {
			detail = "near \"make the tests pass\" context: " + truncate(ctx, 120)
			severity = model.SeverityViolation
		}

		findings = append(findings, model.Finding{
			Detector:   d.Name(),
			Severity:   severity,
			Confidence: confidence,
			Timestamp:  ev.Timestamp,
			Title:      fmt.Sprintf("%s: %s", filepath.Base(ev.FilePath), strings.Join(reasons, ", ")),
			Detail:     detail,
			EventUUIDs: []string{ev.UUID},
			Evidence: []model.Evidence{
				{Label: "removed (old_string)", Timestamp: ev.Timestamp, Content: truncate(old, 400)},
				{Label: "replacement (new_string)", Timestamp: ev.Timestamp, Content: truncate(new_, 400)},
			},
		})
	}
	return findings
}

// combinedDiff flattens an edit event into one old/new pair.
func combinedDiff(ev *model.Event) (string, string) {
	if len(ev.Edits) > 0 {
		var olds, news []string
		for _, op := range ev.Edits {
			olds = append(olds, op.OldString)
			news = append(news, op.NewString)
		}
		return strings.Join(olds, "\n"), strings.Join(news, "\n")
	}
	return ev.OldString, ev.NewString
}

func countMatches(patterns []*regexp.Regexp, s string) int {
	n := 0
	for _, re := range patterns {
		n += len(re.FindAllStringIndex(s, -1))
	}
	return n
}

func countDrop(patterns []*regexp.Regexp, old, new_ string) int {
	if d := countMatches(patterns, old) - countMatches(patterns, new_); d > 0 {
		return d
	}
	return 0
}

func countRise(patterns []*regexp.Regexp, old, new_ string) int {
	if d := countMatches(patterns, new_) - countMatches(patterns, old); d > 0 {
		return d
	}
	return 0
}

var rmRe = regexp.MustCompile(`\brm\s+(-\w+\s+)*([^\s;|&]+)`)

func bashTestDeletion(ev *model.Event) *model.Finding {
	m := rmRe.FindStringSubmatch(ev.Command)
	if m == nil || !IsTestFile(m[2]) {
		return nil
	}
	return &model.Finding{
		Detector:   "test-integrity",
		Severity:   model.SeverityViolation,
		Confidence: model.ConfidenceMedium,
		Timestamp:  ev.Timestamp,
		Title:      fmt.Sprintf("%s deleted via shell", filepath.Base(m[2])),
		EventUUIDs: []string{ev.UUID},
		Evidence: []model.Evidence{
			{Label: "bash command", Timestamp: ev.Timestamp, Content: truncate(ev.Command, 400)},
		},
	}
}

// nearbyPassPressure looks back a few events / 5 minutes for prompt or
// assistant text about making tests pass, which upgrades severity.
func nearbyPassPressure(s *model.Session, idx int) string {
	cutoff := s.Events[idx].Timestamp.Add(-5 * time.Minute)
	for j := idx - 1; j >= 0 && j >= idx-30; j-- {
		ev := &s.Events[j]
		if !ev.Timestamp.IsZero() && ev.Timestamp.Before(cutoff) {
			break
		}
		if (ev.Kind == model.KindPrompt || ev.Kind == model.KindAssistantText) && makeTestsPassRe.MatchString(ev.Text) {
			return ev.Text
		}
	}
	return ""
}

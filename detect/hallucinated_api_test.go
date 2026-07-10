package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/surajsrivastav/agentlens/model"
)

func writeGoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	src := `package fixture

func Helper(x int) int { return x + 1 }

type Widget struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHallucinatedAPIFlagsUndefinedCall(t *testing.T) {
	dir := writeGoRepo(t)
	s := &model.Session{
		CWD: dir,
		Events: []model.Event{
			edit(1, filepath.Join(dir, "main.go"), "",
				"package fixture\n\nfunc run() {\n\tresult := ComputeTotally(5)\n\t_ = result\n}\n"),
		},
	}
	findings := (&HallucinatedAPI{}).Scan(s)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	if findings[0].Title == "" || !contains(findings[0].Title, "ComputeTotally") {
		t.Errorf("title = %q, want it to name ComputeTotally", findings[0].Title)
	}
	if findings[0].Confidence != model.ConfidenceMedium {
		t.Errorf("confidence = %v, want medium", findings[0].Confidence)
	}
}

func TestHallucinatedAPIIgnoresKnownAndSelectorCalls(t *testing.T) {
	dir := writeGoRepo(t)
	s := &model.Session{
		CWD: dir,
		Events: []model.Event{
			edit(1, filepath.Join(dir, "main.go"), "",
				`package fixture

func run() {
	Helper(1)             // declared in the repo — fine
	w := Widget{}
	w.Something()         // selector call — out of scope, not flagged
	fmt.Println("hi")     // selector call on an import — not flagged
}
`),
		},
	}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("expected no findings, got: %+v", findings)
	}
}

func TestHallucinatedAPISkipsNonGoRepo(t *testing.T) {
	dir := t.TempDir() // no .go files at all
	s := &model.Session{
		CWD: dir,
		Events: []model.Event{
			edit(1, filepath.Join(dir, "main.go"), "", "package x\n\nfunc run() { Bogus() }\n"),
		},
	}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("non-Go repo should produce no findings, got: %+v", findings)
	}
}

func TestCandidateCallsSkipsFuncDeclAndSelectors(t *testing.T) {
	src := `package p

func Foo(x int) int {
	return Bar(x)
}

func run() {
	obj.Method(1)
	q := Qux(2)
	_ = q
}
`
	got := candidateCalls(src)
	want := map[string]bool{"Bar": true, "Qux": true}
	if len(got) != len(want) {
		t.Fatalf("candidateCalls = %v, want keys of %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected candidate %q (Foo decl / Method selector should be excluded)", g)
		}
	}
}

// Regression test for a real false positive caught during dogfooding:
// identifiers that only appear inside a Go string literal (e.g. a
// test file whose fixture text happens to look like source code) must
// never be mistaken for a real call expression.
func TestCandidateCallsIgnoresStringLiterals(t *testing.T) {
	src := "package p\n\nfunc run() {\n\tsrc := `result := ComputeTotally(5)`\n\t_ = src\n\t// also NotAFunction(1) in a comment\n}\n"
	if got := candidateCalls(src); len(got) != 0 {
		t.Fatalf("identifiers inside string literals/comments must not be flagged, got %v", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

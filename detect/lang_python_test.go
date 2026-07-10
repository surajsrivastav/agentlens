package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/surajsrivastav/agentlens/model"
)

func writePyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "def helper(x):\n    return x + 1\n\n\nclass Widget:\n    pass\n"
	if err := os.WriteFile(filepath.Join(dir, "helper.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHallucinatedAPIPythonFlagsUndefinedCall(t *testing.T) {
	dir := writePyRepo(t)
	target := filepath.Join(dir, "main.py")
	src := "def run():\n    result = compute_totally(5)\n    return result\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	findings := (&HallucinatedAPI{}).Scan(s)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	if findings[0].Confidence != model.ConfidenceLow {
		t.Errorf("confidence = %v, want low (heuristic language)", findings[0].Confidence)
	}
	if !contains(findings[0].Title, "compute_totally") {
		t.Errorf("title = %q", findings[0].Title)
	}
}

func TestHallucinatedAPIPythonIgnoresImportedAndDeclared(t *testing.T) {
	dir := writePyRepo(t)
	target := filepath.Join(dir, "main.py")
	src := "from itertools import chain\nimport os\n\n\ndef run():\n    helper(1)              # declared elsewhere in the repo\n    chain([1], [2])         # imported bare name\n    os.getcwd()             # selector call on an import, out of scope\n    print(\"hi\")             # builtin\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("expected no findings, got: %+v", findings)
	}
}

func TestHallucinatedAPIPythonWildcardImportIsUnsafe(t *testing.T) {
	dir := writePyRepo(t)
	target := filepath.Join(dir, "main.py")
	src := "from mystery_module import *\n\n\ndef run():\n    totally_unknown_call(1)\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("wildcard import should suppress flagging entirely, got: %+v", findings)
	}
}

func TestHallucinatedAPIPythonIgnoresStringsAndComments(t *testing.T) {
	dir := writePyRepo(t)
	target := filepath.Join(dir, "fixture_writer.py")
	src := "def make_fixture():\n    src = '''\n    result = ComputeTotally(5)\n    '''\n    # also NotAFunction(1) in a comment\n    return src\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("identifiers inside strings/comments must not be flagged, got: %+v", findings)
	}
}

func TestPythonImportNames(t *testing.T) {
	masked := maskCode("import os\nimport numpy as np\nfrom itertools import chain, count as c\nfrom pkg import (a, b)\n", pythonMask)
	names, unsafe := pythonImportNames(masked)
	if unsafe {
		t.Fatal("no wildcard import present, should not be unsafe")
	}
	want := map[string]bool{"os": true, "np": true, "chain": true, "c": true, "a": true, "b": true}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected import name %q, got %v", w, names)
		}
	}
}

func TestPythonImportNamesWildcard(t *testing.T) {
	masked := maskCode("from os.path import *\n", pythonMask)
	_, unsafe := pythonImportNames(masked)
	if !unsafe {
		t.Error("wildcard import should be reported unsafe")
	}
}

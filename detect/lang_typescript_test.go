package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/surajsrivastav/agentlens/model"
)

func writeTSRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "export function helper(x: number): number {\n  return x + 1;\n}\n\nexport class Widget {}\n"
	if err := os.WriteFile(filepath.Join(dir, "helper.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHallucinatedAPITypeScriptFlagsUndefinedCall(t *testing.T) {
	dir := writeTSRepo(t)
	target := filepath.Join(dir, "main.ts")
	src := "function run() {\n  const result = computeTotally(5);\n  return result;\n}\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	findings := (&HallucinatedAPI{}).Scan(s)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	if findings[0].Confidence != model.ConfidenceLow {
		t.Errorf("confidence = %v, want low", findings[0].Confidence)
	}
	if !contains(findings[0].Title, "computeTotally") {
		t.Errorf("title = %q", findings[0].Title)
	}
}

func TestHallucinatedAPITypeScriptIgnoresImportedDeclaredAndMethods(t *testing.T) {
	dir := writeTSRepo(t)
	target := filepath.Join(dir, "main.ts")
	src := `import { chain } from 'lodash';
import * as path from 'path';

class Service {
  process(x: number) {   // method shorthand — a definition, not a call
    return x;
  }
}

function run() {
  helper(1);          // declared elsewhere in the repo
  chain([1, 2]);       // imported bare name
  path.join('a', 'b'); // selector call, out of scope
  console.log('hi');   // selector call on a builtin
  new Promise((resolve) => resolve(1)); // builtin global
}
`
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("expected no findings, got: %+v", findings)
	}
}

func TestHallucinatedAPITypeScriptIgnoresStringsAndComments(t *testing.T) {
	dir := writeTSRepo(t)
	target := filepath.Join(dir, "fixtureWriter.ts")
	src := "function makeFixture() {\n  const src = `\n    const result = computeTotally(5);\n  `;\n  // also notAFunction(1) in a comment\n  return src;\n}\n"
	if err := os.WriteFile(target, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	s := &model.Session{CWD: dir, Events: []model.Event{edit(1, target, "", src)}}
	if findings := (&HallucinatedAPI{}).Scan(s); len(findings) != 0 {
		t.Fatalf("identifiers inside strings/comments must not be flagged, got: %+v", findings)
	}
}

func TestTypeScriptImportNames(t *testing.T) {
	masked := maskCode(`import React, { useState, useEffect as fx } from 'react';
import * as ns from 'ns-module';
const { a, b: bAlias } = require('legacy'); // note: destructure-rename not resolved, only "as" form is
const c = require('c-module');
`, typescriptMask)
	names, unsafe := typescriptImportNames(masked)
	if unsafe {
		t.Fatal("typescript imports are never unsafe")
	}
	want := []string{"React", "useState", "fx", "ns", "a", "c"}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected import name %q in %v", w, names)
		}
	}
}

func TestDeclShapedNamesRecognizesMethodShorthand(t *testing.T) {
	masked := maskCode(`class Service {
  process(x) {
    return x;
  }
}
function run() {
  process(1);
}
`, typescriptMask)
	names := declShapedNames(masked)
	found := false
	for _, n := range names {
		if n == "process" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'process' to be recognized as a method-shorthand declaration, got %v", names)
	}
}

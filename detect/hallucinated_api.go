package detect

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/surajsrivastav/agentlens/model"
)

// HallucinatedAPI (D4) flags calls to functions that don't exist
// anywhere in the repo as it stands today.
//
// Scope is deliberately narrow, mirroring D1's false-positive
// discipline: Go repositories only, and only bare (unqualified),
// exported-looking calls — "Foo(...)", never "pkg.Foo(...)". Selector
// calls would need full cross-package type resolution to check
// safely (stdlib, third-party modules, generics); this v0.1 detector
// doesn't attempt that rather than risk a false accusation. That means
// it catches one specific real failure mode — the agent assumes a
// same-package helper exists, calls it, and never defines it — and
// deliberately misses everything else. Precision over recall.
//
// The symbol index is built from the repo's current on-disk state, so
// a call that was hallucinated but later got a real definition (in
// this session or since) correctly produces no finding — only calls
// that still resolve to nothing today are flagged.
type HallucinatedAPI struct{}

func (d *HallucinatedAPI) Name() string { return "hallucinated-api" }

var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, "dist": true,
	"build": true, "target": true, "bin": true, "testdata": true,
}

func (d *HallucinatedAPI) Scan(s *model.Session) []model.Finding {
	if s.CWD == "" {
		return nil
	}
	index, filesParsed := buildGoSymbolIndex(s.CWD)
	if filesParsed == 0 {
		return nil // not a Go repo (or nothing parseable) — stay silent rather than guess
	}

	var findings []model.Finding
	for i := range s.Events {
		ev := &s.Events[i]
		if !ev.IsFileEdit() || !strings.HasSuffix(ev.FilePath, ".go") {
			continue
		}
		_, newText := combinedDiff(ev)
		if newText == "" {
			continue
		}
		for _, name := range candidateCalls(newText) {
			if index[name] {
				continue
			}
			findings = append(findings, model.Finding{
				Detector:   d.Name(),
				Severity:   model.SeverityWarning,
				Confidence: model.ConfidenceMedium,
				Timestamp:  ev.Timestamp,
				Title:      name + "(...) is not defined anywhere in the repo",
				Detail:     "bare exported call with no matching declaration — possibly hallucinated, possibly a same-package function under a different name",
				EventUUIDs: []string{ev.UUID},
				Evidence: []model.Evidence{
					{Label: "call site", Timestamp: ev.Timestamp, Content: excerptEdit(ev)},
				},
			})
		}
	}
	return findings
}

// candidateCalls extracts bare, exported-looking call identifiers —
// "Foo(...)" but never "x.Foo(...)" — from a snippet of Go source
// using the real parser, not a text regex. This matters: a regex over
// raw bytes can't distinguish an actual call expression from the same
// text appearing inside a string literal or comment (e.g. a test
// fixture file whose fixtures happen to look like Go source), which
// would otherwise manufacture exactly the false positives this whole
// detector exists to avoid. The snippet is tried first as a complete
// file, then as a statement list inside a synthetic function body (an
// Edit's new_string is usually mid-function). If neither parses, the
// snippet is skipped rather than falling back to guessing.
func candidateCalls(src string) []string {
	file := parseSnippet(src)
	if file == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true // selector calls (pkg.Foo / obj.Foo) excluded by construction
		}
		if len(ident.Name) < 3 || !ast.IsExported(ident.Name) || seen[ident.Name] {
			return true
		}
		seen[ident.Name] = true
		out = append(out, ident.Name)
		return true
	})
	return out
}

func parseSnippet(src string) *ast.File {
	fset := token.NewFileSet()
	body := src
	if !strings.HasPrefix(strings.TrimSpace(src), "package ") {
		body = "package p\n" + src
	}
	if f, err := parser.ParseFile(fset, "", body, 0); err == nil {
		return f
	}
	wrapped := "package p\nfunc _agentlensSnippet() {\n" + src + "\n}\n"
	if f, err := parser.ParseFile(fset, "", wrapped, 0); err == nil {
		return f
	}
	return nil
}

// buildGoSymbolIndex walks root collecting every top-level func,
// type, var, and const name declared anywhere in the repo's .go
// files. Unparseable files are skipped, never fatal — same
// tolerant-reader discipline as the ingest layer.
func buildGoSymbolIndex(root string) (map[string]bool, int) {
	index := map[string]bool{}
	filesParsed := 0
	fset := token.NewFileSet()

	_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if skipDirs[e.Name()] || (strings.HasPrefix(e.Name(), ".") && e.Name() != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // malformed/in-progress file — tolerate, don't fail the scan
		}
		filesParsed++
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				index[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						index[sp.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							index[n.Name] = true
						}
					}
				}
			}
		}
		return nil
	})
	return index, filesParsed
}

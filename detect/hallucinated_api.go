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
// Go gets the strong path: real go/parser AST, restricted to bare
// (unqualified), exported-looking calls — "Foo(...)", never
// "pkg.Foo(...)". Python and TypeScript/JavaScript get a heuristic
// path (masking lexer + regex, see heuristic_lang.go) since a real
// parser for those isn't available without a cgo dependency; their
// findings carry ConfidenceLow instead of ConfidenceMedium as a
// result. All three deliberately skip selector/method calls — cross-
// module or cross-package resolution would need real type information
// to check safely, and this detector would rather miss than falsely
// accuse. That means it catches one specific real failure mode — the
// agent assumes a same-file/same-package helper exists, calls it, and
// never defines it — and deliberately misses everything else.
//
// Every symbol index is built from the repo's current on-disk state,
// so a call that was hallucinated but later got a real definition (in
// this session or since) correctly produces no finding — only calls
// that still resolve to nothing today are flagged.
type HallucinatedAPI struct{}

func (d *HallucinatedAPI) Name() string { return "hallucinated-api" }

var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, "dist": true,
	"build": true, "target": true, "bin": true, "testdata": true,
	".next": true, ".nuxt": true, ".turbo": true, "coverage": true,
	"out": true, ".output": true, ".cache": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, ".venv": true, "venv": true, ".tox": true,
}

var heuristicLangs = []*heuristicLang{pythonLang, typescriptLang}

func heuristicLangFor(path string) *heuristicLang {
	for _, l := range heuristicLangs {
		if l.matches(path) {
			return l
		}
	}
	return nil
}

type langCache struct {
	index  map[string]bool
	usable bool
}

func (d *HallucinatedAPI) Scan(s *model.Session) []model.Finding {
	if s.CWD == "" {
		return nil
	}

	var goCache *langCache
	heuristicCaches := map[string]*langCache{}

	var findings []model.Finding
	for i := range s.Events {
		ev := &s.Events[i]
		if !ev.IsFileEdit() {
			continue
		}
		_, newText := combinedDiff(ev)
		if newText == "" {
			continue
		}

		if strings.HasSuffix(ev.FilePath, ".go") {
			if goCache == nil {
				idx, parsed := buildGoSymbolIndex(s.CWD)
				goCache = &langCache{index: idx, usable: parsed > 0}
			}
			if !goCache.usable {
				continue
			}
			for _, name := range candidateCalls(newText) {
				if goCache.index[name] {
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
			continue
		}

		lang := heuristicLangFor(ev.FilePath)
		if lang == nil {
			continue
		}
		cache, ok := heuristicCaches[lang.name]
		if !ok {
			idx, parsed := lang.buildIndex(s.CWD)
			cache = &langCache{index: idx, usable: parsed > 0}
			heuristicCaches[lang.name] = cache
		}
		if !cache.usable {
			continue
		}
		names, unsafe := lang.candidateCalls(newText, ev.FilePath)
		if unsafe {
			continue // e.g. a Python wildcard import makes bare names in this file untrackable
		}
		for _, name := range names {
			if cache.index[name] {
				continue
			}
			findings = append(findings, model.Finding{
				Detector:   d.Name(),
				Severity:   model.SeverityWarning,
				Confidence: lang.confidence,
				Timestamp:  ev.Timestamp,
				Title:      name + "(...) is not defined anywhere in the repo",
				Detail:     "bare call with no matching declaration or import — possibly hallucinated. Heuristic scan (no " + lang.name + " AST available), so treat this one with extra skepticism",
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

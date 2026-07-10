package detect

import (
	"regexp"
	"strings"

	"github.com/surajsrivastav/agentlens/model"
)

var pythonMask = lexRules{
	lineComments: []string{"#"},
	strings:      []string{`"""`, `'''`, `"`, `'`}, // triple-quotes first — longest match wins
	escape:       '\\',
}

var pythonDeclRes = []*regexp.Regexp{
	regexp.MustCompile(`\bdef\s+([A-Za-z_]\w*)\s*\(`),
	regexp.MustCompile(`\bclass\s+([A-Za-z_]\w*)`),
	// Any simple assignment, module-level or local to a function body —
	// Python routinely binds a callable this way ("hash_action = _hash[x];
	// ...; hash_action()", "TestClass = type(...); TestClass()"), no def
	// required. Deliberately unanchored to line-start: Python has no
	// syntax where "NAME = " appears ambiguously outside an assignment,
	// a keyword argument, or a parameter default, and even those looser
	// matches only make the index more lenient, never less precise.
	// [^=] excludes "==" comparisons.
	regexp.MustCompile(`\b([A-Za-z_]\w*)\s*=[^=]`),
	// `with foo() as x:` / `except Exception as e:` — x/e are bound
	// names too, and a bound exception or context-manager result is
	// sometimes itself callable-shaped in real code.
	regexp.MustCompile(`\bas\s+([A-Za-z_]\w*)\b`),
}

// pythonDeclListRes captures a comma-separated list of bound names —
// group 1 is the raw list, split via splitDestructureList. Covers two
// very common Python binding forms a single-name regex can't see:
// for-loop targets ("for k, v in d.items():") and tuple-unpacking
// assignment ("_dict, _tuple, _len = dict, tuple, len" — a real,
// common CPython stdlib idiom for aliasing builtins).
var pythonDeclListRes = []*regexp.Regexp{
	regexp.MustCompile(`\bfor\s+([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+in\b`),
	regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)+)\s*=[^=]`),
}

var pythonWildcardImportRe = regexp.MustCompile(`(?m)^\s*from\s+[\w.]+\s+import\s+\*`)
var pythonPlainImportRe = regexp.MustCompile(`(?m)^\s*import\s+([\w.]+)(?:\s+as\s+(\w+))?`)
var pythonFromImportRe = regexp.MustCompile(`(?m)^\s*from\s+[\w.]+\s+import\s+(\([^)]*\)|[^\n]+)`)

func pythonImportNames(masked string) ([]string, bool) {
	if pythonWildcardImportRe.MatchString(masked) {
		return nil, true
	}
	var names []string
	for _, m := range pythonPlainImportRe.FindAllStringSubmatch(masked, -1) {
		if m[2] != "" {
			names = append(names, m[2])
		} else if m[1] != "" {
			names = append(names, strings.SplitN(m[1], ".", 2)[0])
		}
	}
	for _, m := range pythonFromImportRe.FindAllStringSubmatch(masked, -1) {
		names = append(names, splitImportList(m[1])...)
	}
	return names, false
}

var (
	pythonDefParamsRe   = regexp.MustCompile(`\bdef\s+[A-Za-z_]\w*\s*\(([^()]*)\)`)
	pythonLambdaParamRe = regexp.MustCompile(`\blambda\s+([^:]*):`)
)

// pythonLocalNames extracts def/lambda parameter names bound within
// the snippet, so a callback-style parameter invoked in the same
// snippet (`def apply(fn): return fn()`, `sorted(xs, key=lambda x: x())`)
// is recognized as a real local binding, not a hallucinated call.
func pythonLocalNames(masked string) []string {
	var out []string
	for _, m := range pythonDefParamsRe.FindAllStringSubmatch(masked, -1) {
		out = append(out, splitParamList(m[1])...)
	}
	for _, m := range pythonLambdaParamRe.FindAllStringSubmatch(masked, -1) {
		out = append(out, splitParamList(m[1])...)
	}
	return out
}

// pythonKeywords are language keywords that can be immediately
// followed by "(" — "return(x)", "assert(x)", "import(x)" (rare but
// syntactically fine since parens are just grouping), "for(...)" style
// generator expressions — and must never be mistaken for a call
// target. (Soft keywords "match"/"case"/"type" are deliberately
// excluded: they're valid identifiers in most contexts in Python 3.9+.)
var pythonKeywords = boolSet(
	"False", "None", "True", "and", "as", "assert", "async", "await",
	"break", "class", "continue", "def", "del", "elif", "else", "except",
	"finally", "for", "from", "global", "if", "import", "in", "is",
	"lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try",
	"while", "with", "yield",
)

// pythonBuiltins is not exhaustive — it covers the builtins and
// standard exception types common enough that omitting them would
// make this detector noisy. Add more via PR if you hit a gap.
var pythonBuiltins = mergeSets(pythonKeywords, boolSet(
	"print", "len", "range", "open", "isinstance", "issubclass", "super", "type",
	"dict", "list", "set", "tuple", "frozenset", "str", "int", "float", "bool",
	"bytes", "bytearray", "complex", "object", "property", "staticmethod",
	"classmethod", "enumerate", "zip", "map", "filter", "sorted", "reversed",
	"sum", "min", "max", "abs", "round", "pow", "divmod", "hex", "oct", "bin",
	"chr", "ord", "repr", "format", "vars", "dir", "id", "hash", "iter", "next",
	"callable", "getattr", "setattr", "hasattr", "delattr", "globals", "locals",
	"exec", "eval", "compile", "input", "help", "exit", "quit", "all", "any",
	"slice", "memoryview", "Exception", "ValueError", "TypeError", "KeyError",
	"IndexError", "AttributeError", "RuntimeError", "StopIteration",
	"NotImplementedError", "FileNotFoundError", "OSError", "IOError",
	"ImportError", "ModuleNotFoundError", "ZeroDivisionError", "ArithmeticError",
	"AssertionError", "NameError", "UnboundLocalError", "PermissionError",
	"TimeoutError", "KeyboardInterrupt", "GeneratorExit", "StopAsyncIteration",
	"EOFError", "SystemExit", "SystemError", "UnicodeError", "UnicodeDecodeError",
	"UnicodeEncodeError", "OverflowError", "FloatingPointError", "BufferError",
	"MemoryError", "RecursionError", "ReferenceError", "ConnectionError",
	"BrokenPipeError", "InterruptedError", "IsADirectoryError",
	"NotADirectoryError", "ProcessLookupError", "ChildProcessError",
	"BlockingIOError", "IndentationError", "TabError", "SyntaxWarning",
	"Warning", "DeprecationWarning", "UserWarning", "RuntimeWarning",
	"SyntaxError", "FutureWarning", "PendingDeprecationWarning",
	"ImportWarning", "BytesWarning", "ResourceWarning", "EnvironmentError",
	"LookupError", "ConnectionAbortedError", "ConnectionResetError",
	"ConnectionRefusedError",
))

var pythonLang = &heuristicLang{
	name:         "python",
	exts:         []string{".py"},
	mask:         pythonMask,
	declKeywords: map[string]bool{"def": true, "class": true},
	declRes:      pythonDeclRes,
	declListRes:  pythonDeclListRes,
	builtins:     pythonBuiltins,
	importNames:  pythonImportNames,
	localNames:   pythonLocalNames,
	confidence:   model.ConfidenceLow,
}

func boolSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

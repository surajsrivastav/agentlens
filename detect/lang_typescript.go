package detect

import (
	"regexp"
	"strings"

	"github.com/surajsrivastav/agentlens/model"
)

var typescriptMask = lexRules{
	lineComments:  []string{"//"},
	blockComments: [][2]string{{"/*", "*/"}},
	strings:       []string{"`", `"`, `'`},
	escape:        '\\',
}

var typescriptDeclRes = []*regexp.Regexp{
	// Optional `<...>` tolerates a generic type parameter list between
	// the name and "(" — "function deepCopy<T>(obj: T): T {}".
	regexp.MustCompile(`\bfunction\s*\*?\s+([A-Za-z_$]\w*)\s*(?:<[^>]*>)?\s*\(`),
	// Any const/let/var binding, whatever the right-hand side looks
	// like — including `const foo = useCallback(...)`,
	// `const foo = someLibrary.factory()`, etc. Being this loose is
	// deliberate: for "is this name declared anywhere", the RHS shape
	// doesn't matter, and a looser index only ever reduces false
	// positives.
	regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$]\w*)\s*=`),
	regexp.MustCompile(`\bclass\s+([A-Za-z_$]\w*)`),
}

// typescriptDestructureRes captures array/object destructuring
// declarations — `const [x, setX] = useState(...)`,
// `const { data, error } = useQuery(...)` — an extremely common React
// (and general JS) pattern that a simple "identifier =" regex can't
// see, since the declared names are inside brackets/braces, not a
// single identifier before "=".
var typescriptDestructureRes = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:const|let|var)\s*\[([^\[\]]*)\]\s*=`),
	regexp.MustCompile(`\b(?:const|let|var)\s*\{([^{}]*)\}\s*=`),
}

// jsReservedWords are language keywords that can be immediately
// followed by "(" — "return(x)", "async () => ...", "await foo()",
// "typeof(x)" — and must never be mistaken for a call target.
var jsReservedWords = boolSet(
	"return", "if", "else", "for", "while", "do", "switch", "case",
	"default", "break", "continue", "new", "delete", "typeof",
	"instanceof", "in", "of", "void", "this", "super", "try", "catch",
	"finally", "throw", "yield", "await", "async", "import", "export",
	"from", "as", "static", "get", "set", "class", "extends", "with",
	"debugger", "null", "true", "false", "undefined",
)

// These deliberately do NOT match the quoted module path itself
// (e.g. `from ['"][^'"]+['"]`) — maskCode blanks the entire body of a
// string literal, quotes included, so a pattern requiring those quote
// characters to survive masking can never match. Anchoring on the
// `from` keyword instead sidesteps that entirely.
// The optional `(?:type\s+)?` after "import" handles TypeScript's
// whole-statement type-only import form, "import type { A, B } from
// '...'" — distinct from the per-specifier form "import { type A }"
// that splitImportList already handles.
var (
	tsDefaultImportRe = regexp.MustCompile(`\bimport\s+(?:type\s+)?([A-Za-z_$]\w*)\s*(?:,\s*\{([^}]*)\})?\s*from\b`)
	tsNamedImportRe   = regexp.MustCompile(`\bimport\s*(?:type\s+)?\{([^}]*)\}\s*from\b`)
	tsNamespaceRe     = regexp.MustCompile(`\bimport\s*\*\s*as\s+([A-Za-z_$]\w*)\s*from\b`)
	tsRequireDestrRe  = regexp.MustCompile(`\b(?:const|let|var)\s*\{([^}]*)\}\s*=\s*require\(`)
	tsRequireBareRe   = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$]\w*)\s*=\s*require\(`)
)

var (
	arrowParenParamsRe = regexp.MustCompile(`\(([^()]*)\)\s*=>`)
	arrowBareParamRe   = regexp.MustCompile(`\b([A-Za-z_$]\w*)\s*=>`)
	functionParamsRe   = regexp.MustCompile(`\bfunction\s*\*?\s*[A-Za-z_$]*\s*\(([^()]*)\)`)
)

// typescriptLocalNames extracts parameter names bound within the
// snippet — arrow functions, function expressions/declarations, and
// object/class method shorthand — so a callback parameter that's
// invoked in the same snippet (`(resolve) => resolve(1)`,
// `arr.map(item => item())`) is recognized as a real local binding,
// not a hallucinated call.
func typescriptLocalNames(masked string) []string {
	var out []string
	for _, m := range arrowParenParamsRe.FindAllStringSubmatch(masked, -1) {
		out = append(out, splitParamList(m[1])...)
	}
	for _, m := range arrowBareParamRe.FindAllStringSubmatch(masked, -1) {
		out = append(out, m[1])
	}
	for _, m := range functionParamsRe.FindAllStringSubmatch(masked, -1) {
		out = append(out, splitParamList(m[1])...)
	}
	// Method/object shorthand "name(params) {" — same shape declShapedNames
	// uses to recognize the method name itself; here we also want the
	// params it binds.
	for _, m := range bareCallRe.FindAllStringSubmatchIndex(masked, -1) {
		start := m[0]
		if precededBySelector(masked, start) {
			continue
		}
		openParen := strings.IndexByte(masked[start:], '(') + start
		closeParen := matchingParen(masked, openParen)
		if closeParen >= 0 && followedByBlock(masked, closeParen) {
			out = append(out, splitParamList(masked[openParen+1:closeParen])...)
		}
	}
	return out
}

func typescriptImportNames(masked string) ([]string, bool) {
	var names []string
	for _, m := range tsDefaultImportRe.FindAllStringSubmatch(masked, -1) {
		if m[1] != "" {
			names = append(names, m[1])
		}
		names = append(names, splitImportList(m[2])...)
	}
	for _, m := range tsNamedImportRe.FindAllStringSubmatch(masked, -1) {
		names = append(names, splitImportList(m[1])...)
	}
	for _, m := range tsNamespaceRe.FindAllStringSubmatch(masked, -1) {
		names = append(names, m[1])
	}
	for _, m := range tsRequireDestrRe.FindAllStringSubmatch(masked, -1) {
		names = append(names, splitImportList(m[1])...)
	}
	for _, m := range tsRequireBareRe.FindAllStringSubmatch(masked, -1) {
		names = append(names, m[1])
	}
	return names, false // TS/JS namespace imports require dot access — no bare-name-pollution risk like Python's `import *`
}

// typescriptBuiltins covers the JS/TS globals common enough that
// omitting them would make this detector noisy — not exhaustive.
var typescriptBuiltins = mergeSets(jsReservedWords, boolSet(
	"Object", "Array", "Promise", "Map", "Set", "WeakMap", "WeakSet", "Symbol",
	"Proxy", "Reflect", "RegExp", "Error", "TypeError", "RangeError",
	"SyntaxError", "ReferenceError", "EvalError", "URIError", "Number",
	"String", "Boolean", "Date", "Function", "ArrayBuffer", "Int8Array",
	"Uint8Array", "Uint8ClampedArray", "Int16Array", "Uint16Array", "Int32Array",
	"Uint32Array", "Float32Array", "Float64Array", "BigInt64Array",
	"BigUint64Array", "DataView", "Intl", "BigInt", "JSON", "Math",
	"parseInt", "parseFloat", "isNaN", "isFinite", "encodeURIComponent",
	"decodeURIComponent", "encodeURI", "decodeURI", "setTimeout", "setInterval",
	"clearTimeout", "clearInterval", "structuredClone", "fetch", "btoa", "atob",
	"queueMicrotask", "require", "describe", "it", "test", "expect", "beforeEach",
	"afterEach", "beforeAll", "afterAll",
	// Fetch/Web API globals available in browsers, Node 18+, and edge
	// runtimes (Next.js, Cloudflare Workers) — real global constructors,
	// not repo-local declarations.
	"Request", "Response", "Headers", "URL", "URLSearchParams", "FormData",
	"Blob", "File", "FileReader", "EventSource", "WebSocket", "AbortController",
	"AbortSignal", "ReadableStream", "WritableStream", "TransformStream",
	"TextEncoder", "TextDecoder", "Worker", "MessageChannel", "MessagePort",
	"Event", "EventTarget", "CustomEvent", "performance", "crypto", "process",
	"globalThis", "Buffer",
))

var typescriptLang = &heuristicLang{
	name:         "typescript",
	exts:         []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
	mask:         typescriptMask,
	declKeywords: map[string]bool{"function": true, "class": true},
	declRes:      typescriptDeclRes,
	declListRes:  typescriptDestructureRes,
	builtins:     typescriptBuiltins,
	importNames:  typescriptImportNames,
	localNames:   typescriptLocalNames,
	confidence:   model.ConfidenceLow,
}

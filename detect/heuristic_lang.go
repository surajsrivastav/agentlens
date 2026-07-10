package detect

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/surajsrivastav/agentlens/model"
)

// heuristicLang is the shared implementation behind the non-Go
// languages the hallucinated-API detector supports. Go gets a real
// AST (see hallucinated_api.go); Python and TypeScript/JavaScript
// don't have a Go-stdlib parser available, and pulling in a real one
// means cgo (tree-sitter), which would break plain `go install` for
// everyone — so these languages get a masking lexer (strip strings
// and comments, the exact bug class that bit the Go prototype) plus
// regex-based declaration/import extraction instead.
//
// This is strictly weaker than a real parser and known to be noisier,
// which is why findings from these languages carry ConfidenceLow, not
// the ConfidenceMedium Go gets.
type heuristicLang struct {
	name         string
	exts         []string
	mask         lexRules
	declKeywords map[string]bool  // words that precede "name(" meaning "this is a declaration, not a call"
	declRes      []*regexp.Regexp // group 1 = a declared name (const-assigned fn, class, ...)
	// declListRes captures a bracket/brace list of destructured names
	// — "const [x, setX] = useState(...)", "const {a, b} = obj" —
	// group 1 is the raw list content, split via splitDestructureList.
	declListRes []*regexp.Regexp
	builtins    map[string]bool
	confidence  model.Confidence // always ConfidenceLow-tier for now — heuristic, not AST-verified
	// importNames extracts every name a file's import statements bind
	// locally, from already-masked source. unsafe=true means the file
	// contains a construct (a Python wildcard import) that makes bare
	// names untrackable — callers should skip flagging calls in that
	// file entirely rather than risk a false accusation.
	importNames func(maskedFileSrc string) (names []string, unsafe bool)
	// localNames extracts function/lambda/arrow parameter names bound
	// within the snippet itself — e.g. the `resolve` in
	// `new Promise((resolve) => resolve(1))`. Calling a parameter you
	// just bound (callbacks, promise executors, higher-order
	// functions) is one of the most common shapes in both languages;
	// without this, nearly every callback pattern would be a false
	// positive. Scoped to the snippet, not the whole file, since
	// parameters are inherently snippet-local.
	localNames func(maskedSnippet string) []string
}

func (l *heuristicLang) matches(path string) bool {
	for _, e := range l.exts {
		if strings.HasSuffix(path, e) {
			return true
		}
	}
	return false
}

// buildIndex walks root collecting every name this language profile
// can recognize as "declared or imported somewhere in the repo":
// regex-matched declarations, method/function-shorthand shapes
// ("name(...) {"), and every file's own import bindings unioned
// repo-wide (looser than strictly correct per-file scoping, but
// looseness in the index only ever reduces false positives).
func (l *heuristicLang) buildIndex(root string) (map[string]bool, int) {
	index := map[string]bool{}
	filesParsed := 0

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
		if !l.matches(path) {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		filesParsed++
		masked := maskCode(string(raw), l.mask)

		for _, re := range l.declRes {
			for _, m := range re.FindAllStringSubmatch(masked, -1) {
				if len(m) > 1 && m[1] != "" {
					index[m[1]] = true
				}
			}
		}
		for _, re := range l.declListRes {
			for _, m := range re.FindAllStringSubmatch(masked, -1) {
				if len(m) > 1 {
					for _, n := range splitDestructureList(m[1]) {
						index[n] = true
					}
				}
			}
		}
		for _, name := range declShapedNames(masked) {
			index[name] = true
		}
		if names, _ := l.importNames(masked); len(names) > 0 {
			for _, n := range names {
				index[n] = true
			}
		}
		return nil
	})
	return index, filesParsed
}

// candidateCalls extracts bare call identifiers from a diff's new
// text, then filters out anything bound by the edited file's own
// import statements — read fresh from disk, since a diff hunk rarely
// includes the file's import lines. unsafe mirrors importNames: if the
// file can't be safely resolved (wildcard import), the caller should
// not flag anything in it.
func (l *heuristicLang) candidateCalls(newText, filePath string) (names []string, unsafe bool) {
	masked := maskCode(newText, l.mask)
	seen := map[string]bool{}
	var out []string
	for _, m := range bareCallRe.FindAllStringSubmatchIndex(masked, -1) {
		name := masked[m[2]:m[3]]
		if len(name) < 3 || seen[name] || l.builtins[name] {
			continue
		}
		start := m[0]
		if precededBySelector(masked, start) || precededByAnyKeyword(masked, start, l.declKeywords) {
			continue
		}
		openParen := strings.IndexByte(masked[start:], '(') + start
		if closeParen := matchingParen(masked, openParen); closeParen >= 0 && followedByBlock(masked, closeParen) {
			continue // "name(...) {" is a definition shape, not a call
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, false
	}

	if l.localNames != nil {
		local := map[string]bool{}
		for _, n := range l.localNames(masked) {
			local[n] = true
		}
		var filtered []string
		for _, n := range out {
			if !local[n] {
				filtered = append(filtered, n)
			}
		}
		out = filtered
		if len(out) == 0 {
			return nil, false
		}
	}

	if filePath != "" {
		if raw, err := os.ReadFile(filePath); err == nil {
			fileMasked := maskCode(string(raw), l.mask)
			localNames, u := l.importNames(fileMasked)
			unsafe = u
			local := map[string]bool{}
			for _, n := range localNames {
				local[n] = true
			}
			var filtered []string
			for _, n := range out {
				if !local[n] {
					filtered = append(filtered, n)
				}
			}
			out = filtered
		}
	}
	return out, unsafe
}

var bareCallRe = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// declShapedNames finds "name(...) {" — object/class method shorthand
// in JS/TS (a no-op for Python, which has no brace syntax) — and
// returns the declared names so they land in the index rather than
// being mistaken for calls.
func declShapedNames(masked string) []string {
	var out []string
	for _, m := range bareCallRe.FindAllStringSubmatchIndex(masked, -1) {
		start := m[0]
		if precededBySelector(masked, start) {
			continue
		}
		name := masked[m[2]:m[3]]
		openParen := strings.IndexByte(masked[start:], '(') + start
		if closeParen := matchingParen(masked, openParen); closeParen >= 0 && followedByBlock(masked, closeParen) {
			out = append(out, name)
		}
	}
	return out
}

func precededBySelector(src string, idx int) bool {
	i := idx - 1
	for i >= 0 && (src[i] == ' ' || src[i] == '\t') {
		i--
	}
	return i >= 0 && src[i] == '.'
}

func precededByAnyKeyword(src string, idx int, keywords map[string]bool) bool {
	i := idx - 1
	for i >= 0 && (src[i] == ' ' || src[i] == '\t') {
		i--
	}
	end := i + 1
	for i >= 0 && isIdentByte(src[i]) {
		i--
	}
	return keywords[src[i+1:end]]
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// matchingParen returns the index of the ')' matching the '(' at
// openIdx, or -1 if unbalanced.
func matchingParen(src string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// matchingBracket returns the index of the closing bracket matching
// the opening bracket at the start of s (s[0] must equal open), or -1
// if unbalanced.
func matchingBracket(s string, open, close byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// followedByBlock reports whether closeParenIdx is followed by a
// function/method BODY — either directly ("foo() {") or after a
// TypeScript return-type annotation ("foo(x): Promise<Y> {"), which
// would otherwise be missed and the method mistaken for a bare call.
// Bails out (false) on ";" or a blank line, which mean a call
// statement or a bodyless interface/signature, not a definition.
func followedByBlock(src string, closeParenIdx int) bool {
	i := closeParenIdx + 1
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i < len(src) && src[i] == '{' {
		return true
	}
	if i >= len(src) || src[i] != ':' {
		return false
	}
	depth := 0
	for j := i + 1; j < len(src) && j < i+200; j++ {
		switch src[j] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ';':
			return false
		case '{':
			if depth == 0 {
				return true
			}
		case '\n':
			if depth == 0 && j+1 < len(src) && src[j+1] == '\n' {
				return false
			}
		}
	}
	return false
}

// splitImportList splits a "{a, b as c}" / "a, b as c" style import
// clause on commas, trims a leading "type " (TS type-only imports),
// and resolves "name as alias" to the locally bound alias.
func splitImportList(list string) []string {
	list = strings.Trim(list, "{}() \t\n")
	if list == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "type ")
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, " as "); idx >= 0 {
			part = strings.TrimSpace(part[idx+len(" as "):])
		}
		if part != "" && isIdentifier(part) {
			out = append(out, part)
		}
	}
	return out
}

// splitParamList splits a function/lambda/arrow parameter list into
// the names it binds: a plain param strips a type annotation ("x:
// Foo" -> "x"), a default value ("x = 1" -> "x"), and a rest-parameter
// prefix ("...args" -> "args"); a destructured param ("{ a, b }",
// "[a, b]" — a very common React props pattern) is expanded via
// splitDestructureList instead of being skipped. Splitting on commas
// is depth-aware (splitTopLevelCommas) so a destructured param
// containing its own commas doesn't get shredded by a naive split.
func splitParamList(list string) []string {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil
	}
	var out []string
	for _, part := range splitTopLevelCommas(list) {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "...")
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// A destructured param may carry its own trailing type
		// annotation or default after the closing bracket — "{ a, b
		// }: Props" — so the bracket span must be bounded explicitly
		// rather than trusting strings.Trim, which only trims from a
		// boundary that itself matches the cutset.
		if strings.HasPrefix(part, "{") {
			if end := matchingBracket(part, '{', '}'); end >= 0 {
				out = append(out, splitDestructureList(part[:end+1])...)
				continue
			}
		}
		if strings.HasPrefix(part, "[") {
			if end := matchingBracket(part, '[', ']'); end >= 0 {
				out = append(out, splitDestructureList(part[:end+1])...)
				continue
			}
		}
		if idx := strings.IndexAny(part, ":="); idx >= 0 {
			part = strings.TrimSpace(part[:idx])
		}
		if part != "" && isIdentifier(part) {
			out = append(out, part)
		}
	}
	return out
}

// splitTopLevelCommas splits on commas that aren't nested inside
// {}/[]/()/<> — so a destructured parameter's own internal commas
// don't get mistaken for parameter separators.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '[', '(', '<':
			depth++
		case '}', ']', ')', '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// splitDestructureList splits an array/object destructuring pattern
// ("x, setX" or "data, error: renamedError, count = 0") into the
// names it locally binds. Object rename form "orig: local" resolves
// to local (checked before "=" so a renamed-with-default entry like
// "orig: local = 5" still resolves correctly); nested destructuring
// patterns are skipped (isIdentifier rejects the leftover braces),
// same safe-underextraction trade-off as splitParamList.
func splitDestructureList(list string) []string {
	list = strings.Trim(list, "{}[]() \t\n")
	if list == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "...")
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, ":"); idx >= 0 {
			part = strings.TrimSpace(part[idx+1:])
		}
		if idx := strings.IndexByte(part, '='); idx >= 0 {
			part = strings.TrimSpace(part[:idx])
		}
		if part != "" && isIdentifier(part) {
			out = append(out, part)
		}
	}
	return out
}

// mergeSets unions any number of string sets into one.
func mergeSets(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

func isIdentifier(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return s != ""
}

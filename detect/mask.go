package detect

import "strings"

// lexRules is a minimal, language-parameterized description of where
// comments and string literals live in a language's syntax — just
// enough to blank them out before running identifier regexes over
// source text. This exists because the Go detector's original regex
// prototype flagged identifiers that only appeared inside a string
// literal (a test fixture whose fixture text looked like source code)
// — masking is the general-purpose fix for that bug class in
// languages where a real AST parser isn't available without adding a
// cgo dependency.
type lexRules struct {
	lineComments  []string    // e.g. "//", "#"
	blockComments [][2]string // e.g. {"/*", "*/"}
	strings       []string    // open==close delimiters, LONGEST FIRST (so ''' is tried before ')
	escape        byte        // 0 to disable escape handling
}

// maskCode returns src with every comment and string-literal body
// replaced by spaces (newlines preserved), so a downstream regex scan
// only ever sees real code bytes. Delimiters themselves are blanked
// too; unterminated comments/strings are blanked to end of input
// rather than left unmatched.
func maskCode(src string, rules lexRules) string {
	out := []byte(src)
	n := len(src)
	blank := func(from, to int) {
		for k := from; k < to && k < n; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}

	i := 0
	for i < n {
		if lc := matchPrefix(src, i, rules.lineComments); lc != "" {
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
			continue
		}
		if bc := matchBlockPrefix(src, i, rules.blockComments); bc != nil {
			j := i + len(bc[0])
			for j < n && !strings.HasPrefix(src[j:], bc[1]) {
				j++
			}
			end := j
			if j < n {
				end = j + len(bc[1])
			}
			blank(i, end)
			i = end
			continue
		}
		if sd := matchPrefix(src, i, rules.strings); sd != "" {
			j := i + len(sd)
			for j < n {
				if rules.escape != 0 && src[j] == rules.escape && j+1 < n {
					j += 2
					continue
				}
				if strings.HasPrefix(src[j:], sd) {
					break
				}
				j++
			}
			end := j
			if j < n {
				end = j + len(sd)
			}
			blank(i, end)
			i = end
			continue
		}
		i++
	}
	return string(out)
}

func matchPrefix(src string, i int, candidates []string) string {
	for _, c := range candidates {
		if strings.HasPrefix(src[i:], c) {
			return c
		}
	}
	return ""
}

func matchBlockPrefix(src string, i int, candidates [][2]string) *[2]string {
	for _, c := range candidates {
		if strings.HasPrefix(src[i:], c[0]) {
			return &c
		}
	}
	return nil
}

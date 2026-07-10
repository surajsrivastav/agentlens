package detect

import (
	"strings"
	"testing"
)

func TestMaskCodePython(t *testing.T) {
	src := "x = 1  # Foo(1) in a comment\n" +
		"s = \"Bar(2) in a string\"\n" +
		"doc = '''\nBaz(3) in a docstring\n'''\n" +
		"real = Qux(4)\n"
	masked := maskCode(src, pythonMask)
	for _, forbidden := range []string{"Foo", "Bar", "Baz"} {
		if strings.Contains(masked, forbidden) {
			t.Errorf("masked output should not contain %q:\n%s", forbidden, masked)
		}
	}
	if !strings.Contains(masked, "Qux") {
		t.Errorf("masked output should still contain real code Qux:\n%s", masked)
	}
	// Line count must be preserved (newlines never masked).
	if strings.Count(masked, "\n") != strings.Count(src, "\n") {
		t.Errorf("masking must preserve newline count")
	}
}

func TestMaskCodeTypeScript(t *testing.T) {
	src := "// Foo(1) line comment\n" +
		"/* Bar(2) block comment */\n" +
		"const s = \"Baz(3) in a string\";\n" +
		"const t = `Qux(4) in a template ${Real(5)}`;\n" +
		"real2 = Actual(6);\n"
	masked := maskCode(src, typescriptMask)
	for _, forbidden := range []string{"Foo", "Bar", "Baz", "Qux", "Real"} {
		if strings.Contains(masked, forbidden) {
			t.Errorf("masked output should not contain %q:\n%s", forbidden, masked)
		}
	}
	if !strings.Contains(masked, "Actual") {
		t.Errorf("masked output should still contain real code Actual:\n%s", masked)
	}
}

func TestMatchingParenAndFollowedByBlock(t *testing.T) {
	src := "foo(a, b(c)) { bar() }"
	open := strings.IndexByte(src, '(')
	close_ := matchingParen(src, open)
	if src[close_] != ')' {
		t.Fatalf("matchingParen didn't land on a ')': got %q", src[close_])
	}
	if src[:close_+1] != "foo(a, b(c))" {
		t.Errorf("matchingParen matched wrong span: %q", src[:close_+1])
	}
	if !followedByBlock(src, close_) {
		t.Error("expected followedByBlock to be true for 'foo(...) {'")
	}
}

func TestSplitImportList(t *testing.T) {
	// "type D" keeps D (harmless/safe to treat a type-only import as a
	// known name — looseness in the index only reduces false positives).
	got := splitImportList("{ a, b as c, type D, e }")
	want := []string{"a", "c", "D", "e"}
	if len(got) != len(want) {
		t.Fatalf("splitImportList = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("splitImportList[%d] = %q, want %q", i, got[i], w)
		}
	}
}

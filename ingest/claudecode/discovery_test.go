package claudecode

import (
	"io"
	"strings"
	"testing"
)

// stringsReader is a tiny shim so reader tests avoid importing
// strings just for NewReader at call sites.
func stringsReader(s string) io.Reader { return strings.NewReader(s) }

func TestEncodeProjectPath(t *testing.T) {
	cases := map[string]string{
		"/Users/dev/myrepo":     "-Users-dev-myrepo",
		"/Users/dev/my repo":    "-Users-dev-my-repo",
		"/Users/dev/my_repo.v2": "-Users-dev-my-repo-v2",
		`C:\Users\dev\repo`:     "C--Users-dev-repo",
	}
	for in, want := range cases {
		if got := EncodeProjectPath(in); got != want {
			t.Errorf("EncodeProjectPath(%q) = %q, want %q", in, got, want)
		}
	}
}

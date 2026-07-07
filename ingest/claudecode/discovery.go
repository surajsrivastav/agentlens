// Package claudecode is the ingest adapter for Claude Code session
// logs (~/.claude/projects/<encoded-cwd>/<session-id>.jsonl).
//
// The JSONL format is internal to Claude Code and changes between
// releases, so this adapter is a tolerant reader: it allowlists the
// entry types the detectors need, counts and skips everything else,
// and never fails on a malformed line. Diagnostics (unrecognized
// types, malformed lines, version range) are surfaced on the Session
// so drift degrades loudly.
package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TestedVersions lists the Claude Code major.minor versions covered by
// the fixture corpus. Sessions written by versions outside this set
// still parse (tolerantly) but the report carries a warning.
var TestedVersions = []string{"2.1"}

// SessionFile is a discovered session log, cheap metadata only.
type SessionFile struct {
	ID      string
	Path    string
	ModTime time.Time
	Size    int64
}

// EncodeProjectPath maps a working directory to Claude Code's project
// directory name: every non-alphanumeric byte becomes '-'.
func EncodeProjectPath(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// projectsRoot returns ~/.claude/projects (or an error if no home).
func projectsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// DiscoverSessions lists session files for the given working
// directory, newest first. Layouts differ across Claude Code versions,
// so discovery globs both the flat layout and a sessions/ sublayout
// rather than assuming one.
func DiscoverSessions(cwd string) ([]SessionFile, error) {
	root, err := projectsRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, EncodeProjectPath(cwd))
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("no Claude Code sessions found for %s (looked in %s)", cwd, dir)
	}

	var files []SessionFile
	for _, pattern := range []string{"*.jsonl", filepath.Join("sessions", "*.jsonl")} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			files = append(files, SessionFile{
				ID:      strings.TrimSuffix(filepath.Base(m), ".jsonl"),
				Path:    m,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			})
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no session files in %s", dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })
	return files, nil
}

// FindSession resolves a session by id (exact or unique prefix); with
// an empty id it returns the most recent session.
func FindSession(cwd, id string) (SessionFile, error) {
	files, err := DiscoverSessions(cwd)
	if err != nil {
		return SessionFile{}, err
	}
	if id == "" {
		return files[0], nil
	}
	var matches []SessionFile
	for _, f := range files {
		if f.ID == id {
			return f, nil
		}
		if strings.HasPrefix(f.ID, id) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return SessionFile{}, fmt.Errorf("no session matching %q (run `agentlens sessions` to list)", id)
	default:
		return SessionFile{}, fmt.Errorf("session id %q is ambiguous (%d matches)", id, len(matches))
	}
}

// PeekFirstPrompt scans up to maxLines lines for the first plain user
// prompt, for session listings. Best-effort: any error returns "".
func PeekFirstPrompt(path string, maxLines int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256*1024)
	for i := 0; i < maxLines; i++ {
		line, err := readLine(r)
		if line == nil {
			break
		}
		var raw struct {
			Type    string `json:"type"`
			Message *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &raw) == nil && raw.Type == "user" && raw.Message != nil {
			var s string
			if json.Unmarshal(raw.Message.Content, &s) == nil && isRealPrompt(s) {
				return s
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}

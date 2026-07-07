package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// rawLine mirrors only the JSONL fields the detectors need. Unknown
// fields are ignored by encoding/json — that is the tolerant-reader
// contract.
type rawLine struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	SessionID        string          `json:"sessionId"`
	CWD              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	Version          string          `json:"version"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Message          *rawMessage     `json:"message"`
	CompactMetadata  json.RawMessage `json:"compactMetadata"`
}

type rawMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

type rawEditOp struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type rawToolInput struct {
	FilePath     string      `json:"file_path"`
	NotebookPath string      `json:"notebook_path"`
	OldString    string      `json:"old_string"`
	NewString    string      `json:"new_string"`
	NewSource    string      `json:"new_source"`
	ReplaceAll   bool        `json:"replace_all"`
	Content      string      `json:"content"`
	Command      string      `json:"command"`
	Edits        []rawEditOp `json:"edits"`
}

// ParseFile ingests one session log into a normalized Session.
func ParseFile(path string) (*model.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := Parse(f)
	if err != nil {
		return nil, err
	}
	s.Path = path
	if s.ID == "" {
		s.ID = strings.TrimSuffix(strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".jsonl"), "/")
	}
	return s, nil
}

// Parse reads JSONL from r. Malformed lines are counted, never fatal.
// Unknown top-level types are counted per type and skipped, so silent
// format drift becomes visible in the report.
func Parse(r io.Reader) (*model.Session, error) {
	s := &model.Session{
		Agent:        "claudecode",
		Unrecognized: map[string]int{},
	}
	versions := map[string]bool{}
	usageSeen := map[string]bool{} // assistant message id → usage already attributed

	// Tool-result lines can embed whole files; lines far beyond the
	// default 64KB scanner limit are routine. ReadBytes grows as needed.
	br := bufio.NewReaderSize(r, 1024*1024)
	for {
		line, readErr := readLine(br)
		if line != nil {
			parseLine(s, line, versions, usageSeen)
		}
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			break
		}
	}

	for v := range versions {
		s.Versions = append(s.Versions, v)
	}
	sort.Strings(s.Versions)
	for _, e := range s.Events {
		if !e.Timestamp.IsZero() {
			if s.Start.IsZero() || e.Timestamp.Before(s.Start) {
				s.Start = e.Timestamp
			}
			if e.Timestamp.After(s.End) {
				s.End = e.Timestamp
			}
		}
		if e.Usage != nil {
			s.TotalUsage.InputTokens += e.Usage.InputTokens
			s.TotalUsage.OutputTokens += e.Usage.OutputTokens
			s.TotalUsage.CacheCreationTokens += e.Usage.CacheCreationTokens
			s.TotalUsage.CacheReadTokens += e.Usage.CacheReadTokens
		}
	}
	return s, nil
}

// readLine reads one line of any length. Returns (line, nil) mid-file
// and (lastLine-or-nil, io.EOF) at the end.
func readLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		line = nil
	}
	return line, err
}

func parseLine(s *model.Session, line []byte, versions map[string]bool, usageSeen map[string]bool) {
	var raw rawLine
	if err := json.Unmarshal(line, &raw); err != nil {
		s.MalformedLines++
		return
	}
	if raw.Version != "" {
		versions[raw.Version] = true
	}
	if s.ID == "" && raw.SessionID != "" {
		s.ID = raw.SessionID
	}
	if s.CWD == "" && raw.CWD != "" {
		s.CWD = raw.CWD
	}
	if s.GitBranch == "" && raw.GitBranch != "" {
		s.GitBranch = raw.GitBranch
	}

	ts, _ := time.Parse(time.RFC3339Nano, raw.Timestamp)

	// Subagent (sidechain) events: count, don't analyze (v0.1 scope —
	// honest scoping beats silent wrongness).
	if raw.IsSidechain {
		s.SidechainEvents++
		return
	}

	switch raw.Type {
	case "user":
		parseUser(s, &raw, ts)
	case "assistant":
		parseAssistant(s, &raw, ts, usageSeen)
	case "summary":
		s.Compacted = true
		s.Events = append(s.Events, model.Event{Kind: model.KindCompaction, Timestamp: ts, UUID: raw.UUID, Version: raw.Version})
	case "system":
		if raw.Subtype == "compact_boundary" || len(raw.CompactMetadata) > 0 {
			s.Compacted = true
			s.Events = append(s.Events, model.Event{Kind: model.KindCompaction, Timestamp: ts, UUID: raw.UUID, Version: raw.Version})
		}
		// other system records carry nothing the detectors need
	default:
		key := raw.Type
		if key == "" {
			key = "(no type)"
		}
		s.Unrecognized[key]++
	}
}

func parseUser(s *model.Session, raw *rawLine, ts time.Time) {
	if raw.Message == nil {
		return
	}
	if raw.IsCompactSummary {
		s.Compacted = true
		s.Events = append(s.Events, model.Event{Kind: model.KindCompaction, Timestamp: ts, UUID: raw.UUID, Version: raw.Version})
		return
	}
	// Meta lines (caveats, command wrappers) are not user directives.
	if raw.IsMeta {
		return
	}

	// Content is either a plain string or an array of blocks.
	var text string
	if json.Unmarshal(raw.Message.Content, &text) == nil {
		if isRealPrompt(text) {
			s.Events = append(s.Events, model.Event{
				Kind: model.KindPrompt, Timestamp: ts, UUID: raw.UUID, Version: raw.Version, Text: text,
			})
		}
		return
	}
	var blocks []rawBlock
	if json.Unmarshal(raw.Message.Content, &blocks) != nil {
		s.MalformedLines++
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if isRealPrompt(b.Text) {
				s.Events = append(s.Events, model.Event{
					Kind: model.KindPrompt, Timestamp: ts, UUID: raw.UUID, Version: raw.Version, Text: b.Text,
				})
			}
		case "tool_result":
			s.Events = append(s.Events, model.Event{
				Kind: model.KindToolResult, Timestamp: ts, UUID: raw.UUID, Version: raw.Version, ToolUseID: b.ToolUseID,
			})
		}
	}
}

func parseAssistant(s *model.Session, raw *rawLine, ts time.Time, usageSeen map[string]bool) {
	if raw.Message == nil {
		return
	}
	var blocks []rawBlock
	if json.Unmarshal(raw.Message.Content, &blocks) != nil {
		s.MalformedLines++
		return
	}

	// One assistant message streams across several JSONL lines, each
	// repeating the same usage; attribute it once per message id.
	var usage *model.Usage
	if raw.Message.Usage != nil {
		key := raw.Message.ID
		if key == "" {
			key = raw.UUID
		}
		if !usageSeen[key] {
			usageSeen[key] = true
			usage = &model.Usage{
				InputTokens:         raw.Message.Usage.InputTokens,
				OutputTokens:        raw.Message.Usage.OutputTokens,
				CacheCreationTokens: raw.Message.Usage.CacheCreationInputTokens,
				CacheReadTokens:     raw.Message.Usage.CacheReadInputTokens,
			}
		}
	}

	for _, b := range blocks {
		var ev model.Event
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			ev = model.Event{Kind: model.KindAssistantText, Text: b.Text}
		case "tool_use":
			ev = model.Event{Kind: model.KindToolCall, Tool: b.Name, ToolUseID: b.ID}
			var in rawToolInput
			_ = json.Unmarshal(b.Input, &in) // tolerate odd inputs
			ev.FilePath = in.FilePath
			if ev.FilePath == "" {
				ev.FilePath = in.NotebookPath
			}
			ev.OldString = in.OldString
			ev.NewString = in.NewString
			ev.ReplaceAll = in.ReplaceAll
			ev.Command = in.Command
			if b.Name == "Write" {
				ev.NewString = in.Content
			}
			if b.Name == "NotebookEdit" && in.NewSource != "" {
				ev.NewString = in.NewSource
			}
			for _, op := range in.Edits {
				ev.Edits = append(ev.Edits, model.EditOp{OldString: op.OldString, NewString: op.NewString, ReplaceAll: op.ReplaceAll})
			}
		default:
			continue // thinking etc. — nothing detectors need
		}
		ev.Timestamp = ts
		ev.UUID = raw.UUID
		ev.Version = raw.Version
		if usage != nil {
			ev.Usage = usage
			usage = nil
		}
		s.Events = append(s.Events, ev)
	}
	// Usage present but no renderable block (e.g. thinking-only line):
	// still record it so token totals stay honest.
	if usage != nil {
		s.Events = append(s.Events, model.Event{
			Kind: model.KindAssistantText, Timestamp: ts, UUID: raw.UUID, Version: raw.Version, Usage: usage,
		})
	}
}

// isRealPrompt filters out harness-injected pseudo-prompts so the
// instruction detector only ever quotes words the user actually typed.
func isRealPrompt(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, prefix := range []string{"<", "Caveat:", "[Request interrupted"} {
		if strings.HasPrefix(t, prefix) {
			return false
		}
	}
	return true
}

// UntestedVersions returns the subset of seen versions whose
// major.minor falls outside the tested fixture set.
func UntestedVersions(seen []string) []string {
	var out []string
	for _, v := range seen {
		tested := false
		for _, t := range TestedVersions {
			if v == t || strings.HasPrefix(v, t+".") {
				tested = true
				break
			}
		}
		if !tested {
			out = append(out, v)
		}
	}
	return out
}

// VersionWarning renders the standard drift warning, or "" if all
// seen versions are covered by the fixture corpus.
func VersionWarning(seen []string) string {
	untested := UntestedVersions(seen)
	if len(untested) == 0 {
		return ""
	}
	return fmt.Sprintf("session written by Claude Code %s, outside the tested range (%s) — results are best-effort; please report issues with a fixture",
		strings.Join(untested, ", "), strings.Join(TestedVersions, ", "))
}

package detect

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// Instructions (D1) extracts explicit path-fencing directives from
// user prompts and flags subsequent tool calls that cross the fence.
//
// Scope is deliberately narrow (TRD R2): only high-confidence explicit
// patterns ("don't modify <path>", "leave <path> alone", "only change
// <path>"). Behavioral directives ("use the existing logger") are out
// of scope for v0.1 — precision over recall; a false accusation
// destroys trust in the tool.
//
// Lifecycle: a later user message that mentions the fenced path
// supersedes the directive (supersession beats accusation). Directives
// issued before a compaction boundary survive only in summarized form,
// so their findings are downgraded one confidence level.
type Instructions struct{}

func (d *Instructions) Name() string { return "instruction-violation" }

// pathToken matches something that plausibly names a file or directory:
// contains a slash or a dot-extension. Quotes/backticks optional.
const pathToken = "[`\"']?([\\w~$@.-]*(?:/[\\w~$@*.-]+)+/?|[\\w~$@-]+\\.[\\w*]+|[\\w~$@.-]+/)[`\"']?"

var fencePatterns = []*regexp.Regexp{
	// "don't modify config.yaml", "do not touch src/db/", "never edit X",
	// "please don't change X", "don't delete or modify X"
	regexp.MustCompile(`(?i)\b(?:do\s*n[o']t|never)\s+(?:(?:modify|touch|change|edit|update|delete|remove|rewrite|overwrite)(?:\s*,?\s*(?:or|and)?\s*(?:modify|touch|change|edit|update|delete|remove|rewrite|overwrite))*)\s+(?:the\s+(?:file\s+)?)?` + pathToken),
	// "leave config.yaml alone / unchanged / as is / as-is"
	regexp.MustCompile(`(?i)\bleave\s+` + pathToken + `\s+(?:alone|unchanged|untouched|as[\s-]is)\b`),
	// "keep config.yaml unchanged / as is / intact"
	regexp.MustCompile(`(?i)\bkeep\s+` + pathToken + `\s+(?:unchanged|untouched|intact|as[\s-]is)\b`),
}

var exclusivePattern = regexp.MustCompile(`(?i)\bonly\s+(?:modify|change|edit|touch)\s+(?:the\s+(?:file\s+)?)?` + pathToken)

// supersessionRe marks a later prompt as re-opening a fenced path even
// without naming it ("actually go ahead", "never mind").
var supersessionRe = regexp.MustCompile(`(?i)\b(actually|never\s*mind|nevermind|go ahead|it'?s (?:ok|okay|fine) (?:now|to))\b`)

type directiveKind int

const (
	kindFence     directiveKind = iota // do not touch <path>
	kindExclusive                      // only touch <path>; everything else is fenced
)

type directive struct {
	kind          directiveKind
	path          string // as written by the user
	promptText    string
	issuedAt      time.Time
	supersededAt  time.Time // zero = still active at session end
	supersededBy  string
	preCompaction bool
}

func (d *Instructions) Scan(s *model.Session) []model.Finding {
	directives := extractDirectives(s)
	if len(directives) == 0 {
		return nil
	}

	type hit struct {
		dir    *directive
		events []*model.Event
	}
	hits := map[int]*hit{}

	for i := range s.Events {
		ev := &s.Events[i]
		if ev.Kind != model.KindToolCall {
			continue
		}
		for di := range directives {
			dir := &directives[di]
			if !ev.Timestamp.After(dir.issuedAt) {
				continue
			}
			if !dir.supersededAt.IsZero() && ev.Timestamp.After(dir.supersededAt) {
				continue
			}
			if !violates(dir, ev) {
				continue
			}
			h := hits[di]
			if h == nil {
				h = &hit{dir: dir}
				hits[di] = h
			}
			h.events = append(h.events, ev)
		}
	}

	var findings []model.Finding
	for di := range directives {
		h := hits[di]
		if h == nil {
			continue
		}
		dir := h.dir
		first := h.events[0]

		confidence := model.ConfidenceHigh
		if dir.kind == kindExclusive {
			confidence = model.ConfidenceMedium
		}
		if first.Tool == "Bash" {
			confidence = model.ConfidenceMedium
		}
		detail := describeViolation(dir, h.events)
		if dir.preCompaction {
			confidence = downgrade(confidence)
			detail += " (directive issued before context compaction — reduced confidence)"
		}

		fd := model.Finding{
			Detector:   d.Name(),
			Severity:   model.SeverityViolation,
			Confidence: confidence,
			Timestamp:  first.Timestamp,
			Title:      fmt.Sprintf("You said: %q", truncate(strings.TrimSpace(dir.promptText), 100)),
			Detail:     detail,
			Evidence: []model.Evidence{
				{Label: "your directive", Timestamp: dir.issuedAt, Content: truncate(dir.promptText, 400)},
			},
		}
		for _, ev := range h.events {
			fd.EventUUIDs = append(fd.EventUUIDs, ev.UUID)
			if len(fd.Evidence) < 6 {
				content := excerptEdit(ev)
				if ev.Tool == "Bash" {
					content = "Bash: " + truncate(ev.Command, 240)
				}
				fd.Evidence = append(fd.Evidence, model.Evidence{
					Label: "agent action", Timestamp: ev.Timestamp, Content: content,
				})
			}
		}
		findings = append(findings, fd)
	}
	return findings
}

func extractDirectives(s *model.Session) []directive {
	var out []directive
	compactionSeen := false
	sessionCompactedLater := s.Compacted

	for i := range s.Events {
		ev := &s.Events[i]
		if ev.Kind == model.KindCompaction {
			compactionSeen = true
			continue
		}
		if ev.Kind != model.KindPrompt {
			continue
		}

		// Supersession: a later user message mentioning a fenced path
		// (or a general "actually / go ahead") re-opens it.
		for di := range out {
			dir := &out[di]
			if !dir.supersededAt.IsZero() || !ev.Timestamp.After(dir.issuedAt) {
				continue
			}
			mentions := strings.Contains(strings.ToLower(ev.Text), strings.ToLower(dir.path))
			if mentions || supersessionRe.MatchString(ev.Text) {
				dir.supersededAt = ev.Timestamp
				dir.supersededBy = ev.Text
			}
		}

		// Sentence-level extraction keeps the quoted directive tight.
		for _, sentence := range splitSentences(ev.Text) {
			for _, re := range fencePatterns {
				for _, m := range re.FindAllStringSubmatch(sentence, -1) {
					if p := cleanPath(m[1]); p != "" {
						out = append(out, directive{
							kind: kindFence, path: p,
							promptText: strings.TrimSpace(sentence), issuedAt: ev.Timestamp,
							preCompaction: !compactionSeen && sessionCompactedLater && compactionAfter(s, i),
						})
					}
				}
			}
			for _, m := range exclusivePattern.FindAllStringSubmatch(sentence, -1) {
				if p := cleanPath(m[1]); p != "" {
					out = append(out, directive{
						kind: kindExclusive, path: p,
						promptText: strings.TrimSpace(sentence), issuedAt: ev.Timestamp,
						preCompaction: !compactionSeen && sessionCompactedLater && compactionAfter(s, i),
					})
				}
			}
		}
	}
	return out
}

// compactionAfter reports whether a compaction boundary occurs after
// event index i.
func compactionAfter(s *model.Session, i int) bool {
	for j := i + 1; j < len(s.Events); j++ {
		if s.Events[j].Kind == model.KindCompaction {
			return true
		}
	}
	return false
}

func violates(dir *directive, ev *model.Event) bool {
	switch dir.kind {
	case kindFence:
		if ev.IsFileEdit() && pathMatches(dir.path, ev.FilePath) {
			return true
		}
		if ev.Tool == "Bash" && bashMutates(ev.Command, dir.path) {
			return true
		}
	case kindExclusive:
		// Only Edit-family calls to *other* existing files count; Write
		// may legitimately create new files, so it is excluded to keep
		// false positives down.
		if ev.Kind == model.KindToolCall && (ev.Tool == "Edit" || ev.Tool == "MultiEdit" || ev.Tool == "NotebookEdit") &&
			ev.FilePath != "" && !pathMatches(dir.path, ev.FilePath) {
			return true
		}
	}
	return false
}

// pathMatches compares a user-written path against an absolute tool
// path: basename glob match, suffix match on '/' boundaries, or
// directory-prefix match for fenced directories ("src/db/").
func pathMatches(userPath, toolPath string) bool {
	if userPath == "" || toolPath == "" {
		return false
	}
	tp := filepath.ToSlash(toolPath)
	up := strings.TrimPrefix(filepath.ToSlash(userPath), "./")

	if strings.HasSuffix(up, "/") { // fenced directory
		return strings.Contains(tp, "/"+strings.Trim(up, "/")+"/")
	}
	base := path.Base(tp)
	if ok, _ := path.Match(up, base); ok {
		return true
	}
	return tp == up || strings.HasSuffix(tp, "/"+up)
}

var mutatingBashRe = regexp.MustCompile(`\b(rm|mv|sed\s+-i[^ ]*|tee|truncate|>\s*|>>\s*|chmod|chown)\b`)

func bashMutates(cmd, userPath string) bool {
	up := strings.Trim(strings.TrimPrefix(userPath, "./"), "/")
	if up == "" || !strings.Contains(cmd, up) {
		return false
	}
	return mutatingBashRe.MatchString(cmd)
}

func describeViolation(dir *directive, events []*model.Event) string {
	first := events[0]
	target := first.FilePath
	if target == "" {
		target = dir.path
	}
	action := "edited"
	switch first.Tool {
	case "Write":
		action = "overwrote"
	case "Bash":
		action = "ran a mutating shell command on"
	}
	suffix := ""
	if n := len(events); n > 1 {
		suffix = fmt.Sprintf(" (%d actions)", n)
	}
	return fmt.Sprintf("Agent %s %s%s", action, filepath.Base(target), suffix)
}

func cleanPath(p string) string {
	p = strings.Trim(p, "`\"'.,:;!? ")
	// Common English words that slip through the extension pattern
	// (e.g. "don't change anything.yet" style artifacts) are rejected
	// unless they carry a real path shape.
	if !strings.ContainsAny(p, "/.") {
		return ""
	}
	if strings.HasPrefix(p, ".") && !strings.Contains(p, "/") && !strings.Contains(p[1:], ".") {
		// bare dotfile like ".env" is fine
		return p
	}
	return p
}

// sentenceEndRe splits on terminal punctuation only when followed by
// whitespace or end-of-text, so dots inside paths ("config.yaml")
// never split a sentence.
var sentenceEndRe = regexp.MustCompile(`[.!?]+(\s+|$)|\n+`)

func splitSentences(text string) []string {
	return sentenceEndRe.Split(text, -1)
}

func downgrade(c model.Confidence) model.Confidence {
	if c > model.ConfidenceLow {
		return c - 1
	}
	return c
}

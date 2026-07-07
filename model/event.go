// Package model defines the normalized event schema shared by all
// ingest adapters, detectors, and renderers. Adapters translate
// agent-specific logs into these types; nothing downstream may depend
// on an agent-specific format.
package model

import "time"

// Kind classifies a normalized event.
type Kind int

const (
	// KindPrompt is a user-authored message (a directive source).
	KindPrompt Kind = iota
	// KindAssistantText is assistant prose (used for context correlation).
	KindAssistantText
	// KindToolCall is a tool invocation with parsed inputs.
	KindToolCall
	// KindToolResult is the result of a tool invocation.
	KindToolResult
	// KindCompaction marks a context-compaction boundary. Directives
	// issued before this point may survive only in summarized form, so
	// findings that straddle it carry reduced confidence.
	KindCompaction
)

func (k Kind) String() string {
	switch k {
	case KindPrompt:
		return "prompt"
	case KindAssistantText:
		return "assistant_text"
	case KindToolCall:
		return "tool_call"
	case KindToolResult:
		return "tool_result"
	case KindCompaction:
		return "compaction"
	}
	return "unknown"
}

// Usage is the token accounting for one assistant turn.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
}

// Fresh returns tokens actually generated or newly ingested this turn
// (excludes cache reads, which re-bill prior context).
func (u Usage) Fresh() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationTokens
}

// Total returns all tokens that flowed through the turn.
func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationTokens + u.CacheReadTokens
}

// EditOp is a single old→new replacement within a MultiEdit call.
type EditOp struct {
	OldString  string
	NewString  string
	ReplaceAll bool
}

// Event is one normalized session event. Fields are populated
// according to Kind; unused fields are zero.
type Event struct {
	Kind      Kind
	Timestamp time.Time
	UUID      string
	Sidechain bool   // true for subagent (sidechain) events — excluded from v0.1 analysis
	Version   string // agent version that wrote the underlying log line

	// KindPrompt / KindAssistantText
	Text string

	// KindToolCall
	Tool       string
	ToolUseID  string
	FilePath   string   // Edit/Write/MultiEdit/NotebookEdit/Read target
	OldString  string   // Edit
	NewString  string   // Edit new_string, or Write content
	ReplaceAll bool     // Edit
	Edits      []EditOp // MultiEdit
	Command    string   // Bash

	// Token accounting for the assistant message this event belongs to.
	// Set on at most one event per assistant message (deduped by
	// message id) so summing over events never double-counts.
	Usage *Usage
}

// IsFileEdit reports whether the event mutates a file via a tool call.
func (e *Event) IsFileEdit() bool {
	if e.Kind != KindToolCall {
		return false
	}
	switch e.Tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return e.FilePath != ""
	}
	return false
}

// Session is a fully ingested session plus ingestion diagnostics.
type Session struct {
	ID        string
	Path      string // source log file
	CWD       string
	GitBranch string
	Agent     string // adapter name, e.g. "claudecode"
	Start     time.Time
	End       time.Time

	// Events holds main-session events only, in log order.
	Events []Event

	// Diagnostics from the tolerant reader. Surfaced in reports so
	// format drift degrades loudly instead of silently (TRD R1).
	Versions        []string       // distinct agent versions seen, sorted
	Unrecognized    map[string]int // skipped top-level types → count
	MalformedLines  int
	SidechainEvents int // subagent events detected but not analyzed (v0.2)
	Compacted       bool
	TotalUsage      Usage
}

// Duration returns the wall-clock span of the session.
func (s *Session) Duration() time.Duration {
	if s.Start.IsZero() || s.End.IsZero() {
		return 0
	}
	return s.End.Sub(s.Start)
}

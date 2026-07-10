// Package ingest defines the agent-agnostic adapter contract. Every
// coding-agent integration (Claude Code, Cursor, ...) implements
// Adapter; nothing outside this package's subpackages may depend on
// an agent-specific log format.
package ingest

import (
	"sort"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// SessionInfo is cheap, adapter-agnostic session metadata for listing
// and discovery — no parsing required to produce it.
type SessionInfo struct {
	Agent   string // adapter name, e.g. "claudecode", "cursor"
	ID      string
	Path    string
	ModTime time.Time
	Size    int64
}

// Adapter translates one coding agent's on-disk session logs into the
// normalized model.Session schema. This is the sole contract detectors
// and renderers depend on — adding a new agent means implementing
// this interface, nothing else changes.
type Adapter interface {
	// Name identifies the adapter, e.g. "claudecode".
	Name() string

	// DiscoverSessions lists sessions available for the given working
	// directory, in no particular order. An adapter with no sessions
	// for cwd returns (nil, nil), not an error — absence isn't failure
	// when multiple adapters are queried per repo.
	DiscoverSessions(cwd string) ([]SessionInfo, error)

	// ParseSession ingests one session file into a normalized Session.
	ParseSession(path string) (*model.Session, error)

	// PeekFirstPrompt best-effort extracts the first user prompt for
	// session listings, without a full parse. Returns "" if unknown.
	PeekFirstPrompt(path string) string
}

var registry []Adapter

// Register adds an adapter to the global registry. Adapters call this
// from an init() in their package so importing the package is enough
// to activate it — the same "single-file contribution" ergonomics as
// detectors.
func Register(a Adapter) {
	registry = append(registry, a)
}

// All returns every registered adapter.
func All() []Adapter {
	return registry
}

// DiscoverAll queries every registered adapter for cwd and merges the
// results, newest first.
func DiscoverAll(cwd string) ([]SessionInfo, error) {
	var out []SessionInfo
	var firstErr error
	for _, a := range registry {
		sessions, err := a.DiscoverSessions(cwd)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, sessions...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// ByName returns the registered adapter with the given name, or nil.
func ByName(name string) Adapter {
	for _, a := range registry {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

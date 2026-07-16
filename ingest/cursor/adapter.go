package cursor

import (
	"github.com/surajsrivastav/agentlens/ingest"
	"github.com/surajsrivastav/agentlens/model"
)

// Adapter implements ingest.Adapter for Cursor. EXPERIMENTAL.
type Adapter struct{}

func init() { ingest.Register(Adapter{}) }

func (Adapter) Name() string { return "cursor" }

func (Adapter) DiscoverSessions(cwd string) ([]ingest.SessionInfo, error) {
	return DiscoverSessions(cwd)
}

func (Adapter) ParseSession(path string) (*model.Session, error) {
	return ParseSession(path)
}

// PeekFirstPrompt is not implemented cheaply for Cursor (it requires
// opening the SQLite DB either way), so it does a full-ish parse.
func (Adapter) PeekFirstPrompt(path string) string {
	s, err := ParseSession(path)
	if err != nil {
		return ""
	}
	for _, e := range s.Events {
		if e.Kind == model.KindPrompt {
			return e.Text
		}
	}
	return ""
}

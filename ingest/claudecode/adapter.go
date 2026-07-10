package claudecode

import (
	"github.com/surajsrivastav/agentlens/ingest"
	"github.com/surajsrivastav/agentlens/model"
)

// Adapter implements ingest.Adapter for Claude Code.
type Adapter struct{}

func init() { ingest.Register(Adapter{}) }

func (Adapter) Name() string { return "claudecode" }

func (Adapter) DiscoverSessions(cwd string) ([]ingest.SessionInfo, error) {
	files, err := DiscoverSessions(cwd)
	if err != nil {
		// "no sessions for this adapter" is not a hard failure when
		// multiple adapters are queried per repo.
		return nil, nil
	}
	out := make([]ingest.SessionInfo, len(files))
	for i, f := range files {
		out[i] = ingest.SessionInfo{Agent: "claudecode", ID: f.ID, Path: f.Path, ModTime: f.ModTime, Size: f.Size}
	}
	return out, nil
}

func (Adapter) ParseSession(path string) (*model.Session, error) {
	return ParseFile(path)
}

func (Adapter) PeekFirstPrompt(path string) string {
	return PeekFirstPrompt(path, 200)
}

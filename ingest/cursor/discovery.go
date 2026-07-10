// Package cursor is an EXPERIMENTAL, UNVERIFIED ingest adapter for
// Cursor's local chat history.
//
// Unlike the claudecode adapter — built and tested against real
// session files harvested from this machine — this adapter is
// implemented against publicly documented reverse-engineering of
// Cursor's storage (community write-ups of the state.vscdb SQLite
// schema), with no real Cursor install available to validate against.
// The schema is not officially documented at all (Claude Code's JSONL
// is at least acknowledged as internal-but-real by Anthropic; Cursor's
// on-disk format has no such acknowledgment), so treat every finding
// from this adapter with extra skepticism.
//
// If you have a real Cursor install: please open an issue with an
// anonymized state.vscdb (or just the two ItemTable rows this adapter
// reads) so this can move from experimental to tested.
package cursor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/surajsrivastav/agentlens/ingest"
)

// Warning is surfaced by callers whenever this adapter produces a
// session, so reports never present Cursor findings with the same
// confidence as Claude Code findings.
const Warning = "Cursor support is EXPERIMENTAL and unverified against a real Cursor install (no documented schema, reverse-engineered only) — treat findings with extra skepticism and please report issues"

// chatDataKey and composerDataKeyPrefix are the two ItemTable keys
// community tooling has observed holding chat history. Cursor's format
// has changed across versions; both are checked, tolerantly.
const (
	chatDataKey           = "workbench.panel.aichat.view.aichat.chatdata"
	composerDataKeyPrefix = "composerData:"
)

// storageRoot returns the platform-specific Cursor User directory.
func storageRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Cursor", "User"), nil
	default: // linux and friends
		return filepath.Join(home, ".config", "Cursor", "User"), nil
	}
}

// DiscoverSessions finds workspaceStorage entries whose recorded
// folder matches cwd. Each matching workspace's state.vscdb is one
// "session" for listing purposes (Cursor doesn't have Claude Code's
// clean per-session-file model — the whole workspace history lives in
// one DB, so re-analyzing shows the same underlying data growing).
func DiscoverSessions(cwd string) ([]ingest.SessionInfo, error) {
	root, err := storageRoot()
	if err != nil {
		return nil, err
	}
	wsRoot := filepath.Join(root, "workspaceStorage")
	entries, err := os.ReadDir(wsRoot)
	if err != nil {
		return nil, fmt.Errorf("no Cursor workspace storage found (looked in %s): %w", wsRoot, err)
	}

	cwdClean := filepath.Clean(cwd)
	var out []ingest.SessionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wsDir := filepath.Join(wsRoot, e.Name())
		dbPath := filepath.Join(wsDir, "state.vscdb")
		info, err := os.Stat(dbPath)
		if err != nil {
			continue
		}
		folder, err := workspaceFolder(filepath.Join(wsDir, "workspace.json"))
		if err != nil || filepath.Clean(folder) != cwdClean {
			continue
		}
		out = append(out, ingest.SessionInfo{
			Agent: "cursor", ID: e.Name(), Path: dbPath, ModTime: info.ModTime(), Size: info.Size(),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Cursor session found for %s", cwd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// workspaceJSON mirrors the one field this adapter needs from
// workspace.json: the file:// URI of the project root.
type workspaceJSON struct {
	Folder string `json:"folder"`
}

func workspaceFolder(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var wj workspaceJSON
	if err := json.Unmarshal(data, &wj); err != nil {
		return "", err
	}
	u, err := url.Parse(wj.Folder)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

// openDB opens a Cursor state.vscdb read-only — this adapter never
// writes to Cursor's own storage.
func openDB(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// itemValue reads one ItemTable row by key.
func itemValue(db *sql.DB, key string) ([]byte, error) {
	var val []byte
	err := db.QueryRow(`SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&val)
	return val, err
}

// itemValuesLike reads every ItemTable row whose key matches a SQL
// LIKE pattern (used for composerData:* rows, one per composer tab).
func itemValuesLike(db *sql.DB, pattern string) (map[string][]byte, error) {
	rows, err := db.Query(`SELECT key, value FROM ItemTable WHERE key LIKE ?`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		out[k] = v
	}
	return out, rows.Err()
}

func isCursorPrompt(s string) bool {
	return strings.TrimSpace(s) != ""
}

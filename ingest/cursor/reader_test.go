package cursor

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/surajsrivastav/agentlens/model"
)

// makeTestDB builds a synthetic state.vscdb matching the schema this
// adapter assumes. It validates the adapter's parsing logic, but it is
// not a substitute for testing against a real Cursor install.
func makeTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE, value BLOB)`); err != nil {
		t.Fatal(err)
	}

	chatJSON := `{"tabs":[{"tabId":"t1","chatTitle":"session","bubbles":[
		{"type":"user","text":"add a health check endpoint"},
		{"type":"ai","text":"added /healthz"},
		{"type":"system","text":"noise"}
	]}]}`
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, chatDataKey, chatJSON); err != nil {
		t.Fatal(err)
	}

	composerJSON := `{"conversation":[
		{"type":"user","text":"now add a test for it","timingInfo":{"clientStartTime":1751803200000}},
		{"type":2,"text":"added the test"}
	]}`
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, composerDataKeyPrefix+"abc123", composerJSON); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSessionExtractsPromptsAndAssistantText(t *testing.T) {
	path := makeTestDB(t)
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	if s.Agent != "cursor" {
		t.Errorf("Agent = %q, want cursor", s.Agent)
	}

	var prompts, assistant int
	for _, e := range s.Events {
		switch e.Kind {
		case model.KindPrompt:
			prompts++
		case model.KindAssistantText:
			assistant++
		case model.KindToolCall, model.KindToolResult:
			t.Errorf("cursor adapter must not fabricate tool-call events, got %+v", e)
		}
	}
	if prompts != 2 {
		t.Errorf("prompts = %d, want 2", prompts)
	}
	if assistant != 2 {
		t.Errorf("assistant text = %d, want 2", assistant)
	}
	if s.Unrecognized["bubble:system"] != 1 {
		t.Errorf("unrecognized bubble type not counted: %+v", s.Unrecognized)
	}
}

func TestParseSessionNoRecognizableData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := ParseSession(path); err == nil {
		t.Fatal("expected an error for a DB with no recognizable chat data, got nil")
	}
}

func TestComposerTurnIsUser(t *testing.T) {
	cases := []struct {
		typeJSON string
		want     bool
	}{
		{`"user"`, true},
		{`"USER"`, true},
		{`"ai"`, false},
		{`1`, true},
		{`2`, false},
	}
	for _, c := range cases {
		turn := composerTurn{Type: []byte(c.typeJSON)}
		if got := turn.isUser(); got != c.want {
			t.Errorf("isUser(%s) = %v, want %v", c.typeJSON, got, c.want)
		}
	}
}

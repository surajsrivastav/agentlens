package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// chatData is the legacy chat-tab schema (workbench.panel.aichat...).
type chatData struct {
	Tabs []struct {
		TabID     string       `json:"tabId"`
		ChatTitle string       `json:"chatTitle"`
		Bubbles   []chatBubble `json:"bubbles"`
	} `json:"tabs"`
}

type chatBubble struct {
	Type string `json:"type"` // "user" | "ai"
	Text string `json:"text"`
}

// composerData is the newer Composer/Agent-mode schema
// (composerData:<id>). Field names beyond conversation/type/text are
// unverified against a real install and deliberately not relied on —
// see package doc.
type composerData struct {
	Conversation []composerTurn `json:"conversation"`
}

type composerTurn struct {
	Type       json.RawMessage `json:"type"` // may be a string or a numeric enum across versions
	Text       string          `json:"text"`
	TimingInfo *struct {
		ClientStartTime int64 `json:"clientStartTime"` // ms epoch
	} `json:"timingInfo"`
}

func (t composerTurn) isUser() bool {
	var s string
	if json.Unmarshal(t.Type, &s) == nil {
		return strings.EqualFold(s, "user")
	}
	var n int
	if json.Unmarshal(t.Type, &n) == nil {
		return n == 1 // observed convention: 1=user, 2=assistant
	}
	return false
}

// ParseSession reads a Cursor state.vscdb and extracts prompt / assistant
// text events only.
//
// Deliberately not extracted: file edits, tool calls, bash commands.
// Cursor's on-disk representation of those (inside composer
// "toolFormerData" / capability blocks) is not documented anywhere
// this adapter's author could verify, and guessing field names risks
// fabricating edit events that don't actually correspond to what the
// agent did — precisely the false-positive failure mode this whole
// tool exists to avoid. As a result, D1/D2/D3 currently find nothing
// in Cursor sessions; only D-level prompt text is available. Widening
// this requires a real anonymized state.vscdb fixture — see package
// doc.
func ParseSession(path string) (*model.Session, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer db.Close()

	s := &model.Session{
		Agent:        "cursor",
		Path:         path,
		Unrecognized: map[string]int{},
	}
	if info, err := os.Stat(path); err == nil {
		s.ID = info.Name()
	}

	sawAny := false

	if raw, err := itemValue(db, chatDataKey); err == nil && len(raw) > 0 {
		var cd chatData
		if json.Unmarshal(raw, &cd) == nil {
			for _, tab := range cd.Tabs {
				for _, b := range tab.Bubbles {
					if !isCursorPrompt(b.Text) {
						continue
					}
					sawAny = true
					switch b.Type {
					case "user":
						s.Events = append(s.Events, model.Event{Kind: model.KindPrompt, Text: b.Text})
					case "ai":
						s.Events = append(s.Events, model.Event{Kind: model.KindAssistantText, Text: b.Text})
					default:
						s.Unrecognized["bubble:"+b.Type]++
					}
				}
			}
		} else {
			s.MalformedLines++
		}
	}

	composerRows, _ := itemValuesLike(db, composerDataKeyPrefix+"%")
	for _, raw := range composerRows {
		var cd composerData
		if json.Unmarshal(raw, &cd) != nil {
			s.MalformedLines++
			continue
		}
		for _, turn := range cd.Conversation {
			if !isCursorPrompt(turn.Text) {
				continue
			}
			sawAny = true
			ev := model.Event{Text: turn.Text}
			if turn.isUser() {
				ev.Kind = model.KindPrompt
			} else {
				ev.Kind = model.KindAssistantText
			}
			if turn.TimingInfo != nil && turn.TimingInfo.ClientStartTime > 0 {
				ev.Timestamp = time.UnixMilli(turn.TimingInfo.ClientStartTime)
			}
			s.Events = append(s.Events, ev)
		}
	}

	if !sawAny {
		return nil, fmt.Errorf("no recognizable chat data in %s (schema may have changed — this adapter is experimental)", path)
	}

	for _, e := range s.Events {
		if e.Timestamp.IsZero() {
			continue
		}
		if s.Start.IsZero() || e.Timestamp.Before(s.Start) {
			s.Start = e.Timestamp
		}
		if e.Timestamp.After(s.End) {
			s.End = e.Timestamp
		}
	}
	return s, nil
}

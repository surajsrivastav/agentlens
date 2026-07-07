package detect

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// Rework (D3) flags files the agent kept rewriting: ≥3 distinct edit
// episodes, or A→B→A oscillation where an edit undoes an earlier one.
// Token cost is estimated from the usage of assistant turns inside the
// loop window and is always labeled an estimate.
type Rework struct {
	// EpisodeThreshold is the minimum number of edit episodes before a
	// file is flagged. Defaults to 3 (per PRD).
	EpisodeThreshold int
}

func (d *Rework) Name() string { return "rework-loop" }

type editRec struct {
	ev      *model.Event
	episode int
}

func (d *Rework) Scan(s *model.Session) []model.Finding {
	threshold := d.EpisodeThreshold
	if threshold == 0 {
		threshold = 3
	}

	// Group edits per file; consecutive edits to the same file (no
	// other file touched in between) form one episode, so a burst of
	// sequential edits while building a file isn't counted as rework.
	perFile := map[string][]editRec{}
	lastFile := ""
	for i := range s.Events {
		ev := &s.Events[i]
		if !ev.IsFileEdit() {
			continue
		}
		f := ev.FilePath
		recs := perFile[f]
		episode := 1
		if len(recs) > 0 {
			episode = recs[len(recs)-1].episode
			if lastFile != f {
				episode++
			}
		}
		perFile[f] = append(recs, editRec{ev: ev, episode: episode})
		lastFile = f
	}

	files := make([]string, 0, len(perFile))
	for f := range perFile {
		files = append(files, f)
	}
	sort.Strings(files)

	var findings []model.Finding
	for _, f := range files {
		recs := perFile[f]
		episodes := recs[len(recs)-1].episode
		oscillation := findOscillation(recs)

		if episodes < threshold && oscillation == nil {
			continue
		}

		first, last := recs[0].ev.Timestamp, recs[len(recs)-1].ev.Timestamp
		tokens := tokensBetween(s, first, last)

		var times []string
		seen := 0
		for _, r := range recs {
			if r.episode > seen {
				seen = r.episode
				times = append(times, r.ev.Timestamp.Local().Format("15:04"))
			}
		}

		fd := model.Finding{
			Detector:   d.Name(),
			Severity:   model.SeverityInfo,
			Confidence: model.ConfidenceHigh,
			Timestamp:  first,
			Title:      fmt.Sprintf("%s rewritten %d× (%s)", filepath.Base(f), episodes, strings.Join(times, ", ")),
			TokensEst:  tokens,
		}
		if tokens > 0 {
			fd.Detail = fmt.Sprintf("est. %s tokens on repeated work", humanTokens(tokens))
		}
		if oscillation != nil {
			fd.Severity = model.SeverityWarning
			fd.Detail = strings.TrimSpace("edit at " + oscillation.undo.Timestamp.Local().Format("15:04") +
				" reverses the edit at " + oscillation.orig.Timestamp.Local().Format("15:04") + " (A→B→A). " + fd.Detail)
			fd.Evidence = append(fd.Evidence,
				model.Evidence{Label: "original edit", Timestamp: oscillation.orig.Timestamp, Content: excerptEdit(oscillation.orig)},
				model.Evidence{Label: "reverting edit", Timestamp: oscillation.undo.Timestamp, Content: excerptEdit(oscillation.undo)},
			)
		}
		for _, r := range recs {
			fd.EventUUIDs = append(fd.EventUUIDs, r.ev.UUID)
			if oscillation == nil && len(fd.Evidence) < 5 {
				fd.Evidence = append(fd.Evidence, model.Evidence{
					Label:     fmt.Sprintf("edit %d (%s)", len(fd.Evidence)+1, r.ev.Tool),
					Timestamp: r.ev.Timestamp,
					Content:   excerptEdit(r.ev),
				})
			}
		}
		findings = append(findings, fd)
	}
	return findings
}

type oscillationPair struct{ orig, undo *model.Event }

// findOscillation looks for a later edit that undoes an earlier one:
// exact reversal (old/new swapped) or a new_string highly similar to
// content the file held before the intervening edit.
func findOscillation(recs []editRec) *oscillationPair {
	type frag struct {
		old, new string
		ev       *model.Event
	}
	var frags []frag
	for _, r := range recs {
		if len(r.ev.Edits) > 0 {
			for _, op := range r.ev.Edits {
				frags = append(frags, frag{op.OldString, op.NewString, r.ev})
			}
		} else {
			frags = append(frags, frag{r.ev.OldString, r.ev.NewString, r.ev})
		}
	}
	for j := 1; j < len(frags); j++ {
		for i := 0; i < j; i++ {
			if frags[i].ev == frags[j].ev {
				continue
			}
			// Exact reversal.
			if frags[j].old != "" && frags[j].old == frags[i].new && frags[j].new == frags[i].old {
				return &oscillationPair{orig: frags[i].ev, undo: frags[j].ev}
			}
			// Near-reversal: re-introducing what an earlier edit removed.
			if len(frags[j].new) >= 24 && len(frags[i].old) >= 24 &&
				frags[j].old == frags[i].new &&
				similarity(frags[j].new, frags[i].old) > 0.9 {
				return &oscillationPair{orig: frags[i].ev, undo: frags[j].ev}
			}
		}
	}
	return nil
}

// tokensBetween sums fresh tokens of assistant turns inside [from, to].
func tokensBetween(s *model.Session, from, to time.Time) int {
	total := 0
	for i := range s.Events {
		ev := &s.Events[i]
		if ev.Usage == nil || ev.Timestamp.IsZero() {
			continue
		}
		if !ev.Timestamp.Before(from) && !ev.Timestamp.After(to) {
			total += ev.Usage.Fresh()
		}
	}
	return total
}

func humanTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func excerptEdit(ev *model.Event) string {
	const max = 240
	content := ev.NewString
	if content == "" && len(ev.Edits) > 0 {
		content = ev.Edits[0].NewString
	}
	return fmt.Sprintf("%s %s: %s", ev.Tool, ev.FilePath, truncate(strings.TrimSpace(content), max))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// similarity returns a normalized [0,1] similarity via Levenshtein
// distance. Inputs are capped so pathological strings stay cheap.
func similarity(a, b string) float64 {
	const limit = 4096
	if len(a) > limit {
		a = a[:limit]
	}
	if len(b) > limit {
		b = b[:limit]
	}
	if a == b {
		return 1
	}
	la, lb := len(a), len(b)
	if la == 0 || lb == 0 {
		return 0
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	dist := prev[lb]
	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	return 1 - float64(dist)/float64(maxLen)
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

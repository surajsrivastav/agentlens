package render

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/surajsrivastav/agentlens/model"
)

// ANSI helpers; disabled when not a TTY or when NO_COLOR is set.
type palette struct{ bold, dim, red, yellow, cyan, green, reset string }

func colors(w io.Writer) palette {
	if os.Getenv("NO_COLOR") != "" {
		return palette{}
	}
	if f, ok := w.(*os.File); ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return palette{}
		}
	} else {
		return palette{}
	}
	return palette{
		bold: "\x1b[1m", dim: "\x1b[2m", red: "\x1b[31m",
		yellow: "\x1b[33m", cyan: "\x1b[36m", green: "\x1b[32m", reset: "\x1b[0m",
	}
}

// Terminal writes the compact, screenshot-friendly report.
func Terminal(w io.Writer, r *Report) {
	c := colors(w)
	s := r.Session

	fmt.Fprintf(w, "\n%sAGENTLENS REPORT%s — session %s (%s, %d events, ~%s tokens)\n",
		c.bold, c.reset,
		s.Start.Local().Format("2006-01-02 15:04"),
		humanDuration(s), len(s.Events), humanK(s.TotalUsage.Fresh()))
	if s.CWD != "" {
		fmt.Fprintf(w, "%s%s", c.dim, s.CWD)
		if s.GitBranch != "" {
			fmt.Fprintf(w, " (%s)", s.GitBranch)
		}
		fmt.Fprintf(w, "%s\n", c.reset)
	}

	secs := r.sections()
	if len(secs) == 0 {
		fmt.Fprintf(w, "\n%s✅ No findings — %d events clean%s\n", c.green, r.CleanEvents, c.reset)
	}
	for _, sec := range secs {
		color := c.yellow
		if sec.Detector == "instruction-violation" {
			color = c.red
		}
		fmt.Fprintf(w, "\n%s%s%s %s (%d)%s\n", color, sec.Icon, c.reset+c.bold, sec.Title, len(sec.Findings), c.reset)
		for _, f := range sec.Findings {
			ts := "     "
			if !f.Timestamp.IsZero() {
				ts = f.Timestamp.Local().Format("15:04")
			}
			fmt.Fprintf(w, "  %s%s%s  %s\n", c.cyan, ts, c.reset, f.Title)
			if f.Detail != "" {
				fmt.Fprintf(w, "         %s\n", f.Detail)
			}
			fmt.Fprintf(w, "         %sconfidence: %s%s\n", c.dim, f.Confidence, c.reset)
			if r.Explain {
				for _, ev := range f.Evidence {
					fmt.Fprintf(w, "         %s└ %s: %s%s\n", c.dim, ev.Label, oneLine(ev.Content), c.reset)
				}
			}
		}
	}

	if len(secs) > 0 {
		fmt.Fprintf(w, "\n%s✅ %d events clean%s\n", c.green, r.CleanEvents, c.reset)
	}

	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "%s⚠ %s%s\n", c.dim, warning, c.reset)
	}
	if !r.Explain && len(r.Findings) > 0 {
		fmt.Fprintf(w, "%sRun with --explain to see the evidence behind each finding.%s\n", c.dim, c.reset)
	}
	fmt.Fprintf(w, "\n%s%s%s\n", c.dim, funnelFooter, c.reset)
}

func humanDuration(s *model.Session) string {
	d := s.Duration()
	if d <= 0 {
		return "unknown duration"
	}
	if m := int(d.Minutes()); m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func humanK(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

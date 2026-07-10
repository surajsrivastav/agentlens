// agentlens — flight recorder for coding agents.
//
// Reads the coding-agent session logs already on your disk and reports
// every mistake the agent made: instructions it ignored, tests it
// weakened, and tokens it burned going in circles.
//
// 100% local: no network calls, no telemetry. Session content never
// leaves the machine.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/surajsrivastav/agentlens/detect"
	"github.com/surajsrivastav/agentlens/ingest"
	"github.com/surajsrivastav/agentlens/ingest/claudecode"
	"github.com/surajsrivastav/agentlens/ingest/cursor"
	"github.com/surajsrivastav/agentlens/model"
	"github.com/surajsrivastav/agentlens/render"
)

// version is stamped at release time via -ldflags "-X main.version=vX.Y.Z".
var version = "0.1.0-dev"

const usage = `agentlens — flight recorder for coding agents

Usage:
  agentlens analyze [--session <id>] [--agent <name>] [--html <out.html>] [--json] [--explain] [--fail-on <severity>]
  agentlens trend [--json] [--limit <n>]
  agentlens sessions
  agentlens --version

Commands:
  analyze    Analyze one session for the current directory: the most
             recent by default, or a specific one via --session.
  trend      Aggregate findings across every discovered session for
             the current directory, oldest first.
  sessions   List discovered sessions for the current directory.

Flags for analyze:
  --session <id>     Session id (or unique prefix) to analyze.
  --agent <name>     Restrict to one adapter: claudecode, cursor.
  --html <path>      Also write a self-contained HTML report.
  --json             Emit machine-readable JSON to stdout instead of text.
  --explain          Show the evidence behind each finding.
  --fail-on <level>  Exit 1 if any finding is at or above this severity
                     (violation, warning, info). For CI. Unset = always exit 0.

Flags for trend:
  --json          Emit machine-readable JSON to stdout instead of text.
  --limit <n>     Max sessions to include, most recent first (default 20).

agentlens is 100% local: no network calls, no telemetry.
Cursor support is experimental — see README.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "analyze":
		os.Exit(cmdAnalyze(args[1:]))
	case "trend":
		os.Exit(cmdTrend(args[1:]))
	case "sessions":
		os.Exit(cmdSessions())
	case "--version", "version", "-v":
		fmt.Printf("agentlens %s\n", version)
	case "--help", "help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

// pickSession resolves a session across every registered adapter,
// optionally restricted to one agent, by exact id or unique prefix.
// With no id, it returns the most recent session overall.
func pickSession(cwd, agent, id string) (ingest.SessionInfo, error) {
	infos, err := ingest.DiscoverAll(cwd)
	if err != nil {
		return ingest.SessionInfo{}, err
	}
	if agent != "" {
		var filtered []ingest.SessionInfo
		for _, i := range infos {
			if i.Agent == agent {
				filtered = append(filtered, i)
			}
		}
		infos = filtered
	}
	if len(infos) == 0 {
		return ingest.SessionInfo{}, fmt.Errorf("no sessions found for %s (run `agentlens sessions` to check)", cwd)
	}
	if id == "" {
		return infos[0], nil
	}
	var matches []ingest.SessionInfo
	for _, i := range infos {
		if i.ID == id {
			return i, nil
		}
		if strings.HasPrefix(i.ID, id) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return ingest.SessionInfo{}, fmt.Errorf("no session matching %q", id)
	default:
		return ingest.SessionInfo{}, fmt.Errorf("session id %q is ambiguous (%d matches)", id, len(matches))
	}
}

var failOnLevels = map[string]model.Severity{
	"info": model.SeverityInfo, "warning": model.SeverityWarning, "violation": model.SeverityViolation,
}

func cmdAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	sessionID := fs.String("session", "", "session id (or unique prefix)")
	agent := fs.String("agent", "", "restrict to one adapter: claudecode, cursor")
	htmlOut := fs.String("html", "", "write self-contained HTML report to this path")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	explain := fs.Bool("explain", false, "show evidence behind each finding")
	failOn := fs.String("fail-on", "", "exit 1 if any finding is at or above this severity: violation, warning, info")
	_ = fs.Parse(args)

	var failThreshold *model.Severity
	if *failOn != "" {
		lvl, ok := failOnLevels[*failOn]
		if !ok {
			return fail(fmt.Errorf("--fail-on must be one of: violation, warning, info (got %q)", *failOn))
		}
		failThreshold = &lvl
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	sf, err := pickSession(cwd, *agent, *sessionID)
	if err != nil {
		return fail(err)
	}
	adapter := ingest.ByName(sf.Agent)
	session, err := adapter.ParseSession(sf.Path)
	if err != nil {
		return fail(fmt.Errorf("parsing %s: %w", sf.Path, err))
	}

	var findings []model.Finding
	for _, d := range detect.All() {
		findings = append(findings, d.Scan(session)...)
	}

	report := render.Build(session, findings, sessionWarnings(session), version)
	report.Explain = *explain

	if *htmlOut != "" {
		f, err := os.Create(*htmlOut)
		if err != nil {
			return fail(err)
		}
		if err := render.HTML(f, report); err != nil {
			f.Close()
			return fail(err)
		}
		f.Close()
		fmt.Fprintf(os.Stderr, "HTML report written to %s\n", *htmlOut)
	}

	if *jsonOut {
		if err := render.JSON(os.Stdout, report); err != nil {
			return fail(err)
		}
	} else {
		render.Terminal(os.Stdout, report)
	}

	if failThreshold != nil {
		for _, f := range findings {
			if f.Severity >= *failThreshold {
				return 1
			}
		}
	}
	return 0
}

func cmdTrend(args []string) int {
	fs := flag.NewFlagSet("trend", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	limit := fs.Int("limit", 20, "max sessions to include, most recent first")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	infos, err := ingest.DiscoverAll(cwd)
	if err != nil {
		return fail(err)
	}
	if len(infos) > *limit {
		infos = infos[:*limit]
	}

	var rows []render.TrendRow
	sawCursor := false
	for _, info := range infos {
		adapter := ingest.ByName(info.Agent)
		if adapter == nil {
			continue
		}
		if info.Agent == "cursor" {
			sawCursor = true
		}
		row := render.TrendRow{SessionID: info.ID, Agent: info.Agent, Path: info.Path}
		session, err := adapter.ParseSession(info.Path)
		if err != nil {
			row.ParseError = err.Error()
			rows = append(rows, row)
			continue
		}
		var findings []model.Finding
		for _, d := range detect.All() {
			findings = append(findings, d.Scan(session)...)
		}
		row.Start = session.Start
		row.Duration = session.Duration()
		row.Events = len(session.Events)
		row.Tokens = session.TotalUsage.Fresh()
		row.Counts = map[string]int{}
		for _, f := range findings {
			row.Counts[f.Detector]++
		}
		rows = append(rows, row)
	}

	var trendWarnings []string
	if sawCursor {
		trendWarnings = append(trendWarnings, cursor.Warning)
	}

	report := render.BuildTrend(rows, trendWarnings, version)
	if *jsonOut {
		if err := render.TrendJSON(os.Stdout, report); err != nil {
			return fail(err)
		}
	} else {
		render.TrendTerminal(os.Stdout, report)
	}
	return 0
}

// sessionWarnings turns ingest diagnostics into report warnings so
// drift and scoping limits degrade loudly instead of silently.
func sessionWarnings(s *model.Session) []string {
	var out []string
	if s.Agent == "cursor" {
		out = append(out, cursor.Warning)
	}
	if w := claudecode.VersionWarning(s.Versions); w != "" {
		out = append(out, w)
	}
	if s.SidechainEvents > 0 {
		out = append(out, fmt.Sprintf("%d subagent events detected, not analyzed (v0.2)", s.SidechainEvents))
	}
	if s.Compacted {
		out = append(out, "session was compacted — directives issued before compaction are analyzed with reduced confidence")
	}
	if n := total(s.Unrecognized); n > 0 {
		out = append(out, fmt.Sprintf("%d unrecognized events skipped (%s)", n, typeList(s.Unrecognized)))
	}
	if s.MalformedLines > 0 {
		out = append(out, fmt.Sprintf("%d malformed lines skipped", s.MalformedLines))
	}
	return out
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func typeList(m map[string]int) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) > 4 {
		keys = keys[:4]
		return strings.Join(keys, ", ") + ", …"
	}
	return strings.Join(keys, ", ")
}

func cmdSessions() int {
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	infos, err := ingest.DiscoverAll(cwd)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%-38s %-11s %-17s %9s  %s\n", "SESSION", "AGENT", "MODIFIED", "SIZE", "FIRST PROMPT")
	sawCursor := false
	for _, f := range infos {
		adapter := ingest.ByName(f.Agent)
		prompt := ""
		if adapter != nil {
			prompt = adapter.PeekFirstPrompt(f.Path)
		}
		if len(prompt) > 60 {
			prompt = prompt[:60] + "…"
		}
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		agentLabel := f.Agent
		if f.Agent == "cursor" {
			agentLabel += "*"
			sawCursor = true
		}
		fmt.Printf("%-38s %-11s %-17s %9s  %s\n",
			f.ID, agentLabel, f.ModTime.Local().Format("2006-01-02 15:04"), humanSize(f.Size), prompt)
	}
	if sawCursor {
		fmt.Printf("\n* %s\n", cursor.Warning)
	}
	return 0
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "agentlens: %v\n", err)
	return 1
}

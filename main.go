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
	"github.com/surajsrivastav/agentlens/ingest/claudecode"
	"github.com/surajsrivastav/agentlens/model"
	"github.com/surajsrivastav/agentlens/render"
)

// version is stamped at release time via -ldflags "-X main.version=vX.Y.Z".
var version = "0.1.0-dev"

const usage = `agentlens — flight recorder for coding agents

Usage:
  agentlens analyze [--session <id>] [--html <out.html>] [--json] [--explain]
  agentlens sessions
  agentlens --version

Commands:
  analyze    Analyze the latest Claude Code session for the current
             directory (or a specific one via --session).
  sessions   List discovered sessions for the current directory.

Flags for analyze:
  --session <id>    Session id (or unique prefix) to analyze.
  --html <path>     Also write a self-contained HTML report.
  --json            Emit machine-readable JSON to stdout instead of text.
  --explain         Show the evidence behind each finding.

agentlens is 100%% local: no network calls, no telemetry.
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

func cmdAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	sessionID := fs.String("session", "", "session id (or unique prefix)")
	htmlOut := fs.String("html", "", "write self-contained HTML report to this path")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	explain := fs.Bool("explain", false, "show evidence behind each finding")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	sf, err := claudecode.FindSession(cwd, *sessionID)
	if err != nil {
		return fail(err)
	}
	session, err := claudecode.ParseFile(sf.Path)
	if err != nil {
		return fail(fmt.Errorf("parsing %s: %w", sf.Path, err))
	}

	var findings []model.Finding
	for _, d := range detect.All() {
		findings = append(findings, d.Scan(session)...)
	}

	report := render.Build(session, findings, warnings(session), version)
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
	return 0
}

// warnings turns ingest diagnostics into report warnings so drift and
// scoping limits degrade loudly instead of silently.
func warnings(s *model.Session) []string {
	var out []string
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
	files, err := claudecode.DiscoverSessions(cwd)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("%-38s %-17s %9s  %s\n", "SESSION", "MODIFIED", "SIZE", "FIRST PROMPT")
	for _, f := range files {
		prompt := claudecode.PeekFirstPrompt(f.Path, 200)
		if len(prompt) > 60 {
			prompt = prompt[:60] + "…"
		}
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		fmt.Printf("%-38s %-17s %9s  %s\n",
			f.ID, f.ModTime.Local().Format("2006-01-02 15:04"), humanSize(f.Size), prompt)
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

# agentlens

**See what your coding agent actually did last session.**

I was tired of opening the final diff and having no idea how the agent got there — which instructions it ignored, which tests it "fixed" by removing assertions, and how many times it rewrote the same file chasing its own tail. The session data was already on disk in `~/.claude/projects/`, but it was unread.

So agentlens reads that session data for you and shows you the real story.

- Zero config
- Read-only: it diagnoses, it never changes your code
- 100% local: no network calls, no telemetry

## Install

```sh
go install github.com/surajsrivastav/agentlens@latest
```

## Use

```sh
cd my-repo
agentlens analyze
```

Example output:

```sh
AGENTLENS REPORT — session 2026-07-06 14:02 (47 min, 312 events, ~184k tokens)

INSTRUCTION VIOLATIONS (1)
  14:14  You said: "Do not modify config.yaml"
         Agent edited config.yaml
         confidence: high

TEST INTEGRITY (1)
  14:22  auth_test.go: 2 assertion(s) removed, 1 skip/disable annotation(s) added
         near "make the tests pass" context
         confidence: high

REWORK LOOPS (1)
  14:10  auth.go rewritten 3× (14:10, 14:25, 14:38)
         est. 41k tokens on repeated work
         confidence: high

214 events clean
```

That is the real signal: a concrete issue from your own session, not a demo. Everything else in the tool exists to help you get to that faster.

## More commands

```sh
agentlens analyze [--session <id>] [--agent <name>] [--html <out.html>] [--json] [--explain] [--fail-on <severity>]
agentlens trend [--json] [--limit <n>]
agentlens sessions
agentlens --version
```

Commands:

- `analyze` — analyze one session for the current directory: the most recent by default, or a specific one via `--session`
- `trend` — aggregate findings across every discovered session for the current directory, oldest first
- `sessions` — list discovered sessions for the current directory

Analyze flags:

- `--session <id>` — session id (or unique prefix) to analyze
- `--agent <name>` — restrict to one adapter: `claudecode`, `cursor`
- `--html <path>` — also write a self-contained HTML report
- `--json` — emit machine-readable JSON to stdout instead of text
- `--explain` — show the evidence behind each finding
- `--fail-on <level>` — exit 1 if any finding is at or above this severity (`violation`, `warning`, `info`); unset = always exit 0

Trend flags:

- `--json` — emit machine-readable JSON to stdout instead of text
- `--limit <n>` — max sessions to include, most recent first (default 20)

agentlens is 100% local: no network calls, no telemetry.
Cursor support is experimental — see README.

## What it finds

- **Instruction violations** — the agent ignored a directive and still changed a file.
- **Test integrity issues** — assertions removed, tests skipped or deleted, or checks weakened.
- **Rework loops** — files rewritten repeatedly or bounced back and forth.
- **Hallucinated APIs** *(Go, Python, TypeScript/JavaScript)* — the agent called a function that doesn't exist in your repo.

## When to use it

- `agentlens analyze` for a one-off session review
- `agentlens trend` to compare sessions across time
- `agentlens analyze --fail-on violation` to gate CI on session problems

Example GitHub Actions step:

```yaml
- name: Check the agent's session for violations
  run: agentlens analyze --fail-on violation
```

## Trust rules

This tool shows evidence — you decide what it means.

- Every finding includes a confidence label.
- `--explain` shows the exact prompt and tool call behind each finding.
- Detectors are intentionally conservative.
- If a later instruction overrides an earlier one, the earlier instruction is not treated as a violation.

Hallucinated-API detection:

- Go uses real syntax parsing, so it avoids string/comment false positives.
- Python and TypeScript/JavaScript use a lexer-and-regex approach to stay dependency-free and keep `go install` working.
- Because of that, Python/TypeScript findings are lower confidence than Go findings.
- All findings only trigger if the name is missing from your repo today.

If agentlens encounters unknown or malformed session data, it surfaces that loudly instead of ignoring it. If a session format is misparsed, please open an issue with an anonymized fixture.

## Cursor support

Cursor support is experimental.

Cursor's local chat storage is undocumented, so this implementation is based on community reverse-engineering. It reads prompts and assistant text, but it does not yet analyze Cursor tool calls or file-edit metadata. If you use Cursor and can share anonymized `state.vscdb` rows, that would help make this support reliable.

## Contributing

- `ingest/` — agent adapters (claudecode, cursor) that normalize events
- `detect/` — detectors; add one file implementing `Detector`
- `render/` — terminal, HTML, and JSON output
- `fixtures/` — anonymized sessions for CI-tested edge cases

Adding a detector should be a weekend task: implement `Scan(*model.Session) []model.Finding`, register it in `detect.All()`, and you are done.

Roadmap: CI mode and repo-level session trends are already done. Next up is a real Cursor fixture corpus, an Agent Failure Taxonomy, and a pluggable detector SDK.

## License

MIT

---

*Want this recorded permanently in git history? → [gitwhy](https://github.com/surajsrivastav/gitwhy)*

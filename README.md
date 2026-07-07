# agentlens

**Flight recorder for coding agents.** A zero-config CLI that reads the coding-agent session logs already on your disk and tells you every mistake your agent made: instructions it ignored, tests it weakened, and tokens it burned going in circles.

> 🔒 **100% local. No network calls. No telemetry.** Your session content never leaves your machine. agentlens is a read-only auditor — it diagnoses; it never intervenes.

```
AGENTLENS REPORT — session 2026-07-06 14:02 (47 min, 312 events, ~184k tokens)

⛔ INSTRUCTION VIOLATIONS (1)
  14:14  You said: "Do not modify config.yaml"
         Agent edited config.yaml
         confidence: high

⚠️ TEST INTEGRITY (1)
  14:22  auth_test.go: 2 assertion(s) removed, 1 skip/disable annotation(s) added
         near "make the tests pass" context
         confidence: high

🔁 REWORK LOOPS (1)
  14:10  auth.go rewritten 3× (14:10, 14:25, 14:38)
         est. 41k tokens on repeated work
         confidence: high

✅ 214 events clean
```

## Why

You review your agent's final diff — not its *process*. Silent instruction violations, weakened tests, and redundant rework loops are invisible until they cost you a production incident or a blown token budget. The evidence is already on your disk: Claude Code persists complete session transcripts under `~/.claude/projects/`. Nobody reads them because they're large and unstructured. agentlens reads them for you and renders a verdict.

## Install

```sh
go install github.com/surajsrivastav/agentlens@latest
```

(brew formula and binary releases coming with v0.1.)

## Use

```sh
cd my-repo
agentlens analyze              # analyzes the latest Claude Code session for this repo
agentlens analyze --explain    # show the evidence behind each finding
agentlens analyze --html report.html   # shareable single-file dark-mode report
agentlens analyze --json       # machine-readable, for CI / tooling
agentlens sessions             # list discovered sessions for this repo
```

No config file. No setup. Zero-config is a feature.

## What it detects (v0.1)

| Detector | What it catches |
|---|---|
| **Instruction violations** | You said "don't touch `config.yaml`" — the agent edited it anyway. Explicit path-fencing directives are extracted from your prompts and cross-referenced against every file edit and mutating shell command. |
| **Test integrity** | Assertions removed, tests deleted, skip/disable annotations added (`t.Skip`, `@pytest.mark.skip`, `it.skip`, …), assertions weakened (`assertEqual` → `assertTrue`) — ranked higher when it happens near "make the tests pass" pressure. |
| **Rework loops** | Files rewritten 3+ times or oscillating A→B→A, with an estimate of the tokens burned on repeated work. |

### Honesty rules

The tool presents evidence; **you** render the verdict.

- Every finding carries a **confidence label** and, via `--explain`, the exact prompt line and the exact tool call that back it.
- Detectors are deliberately conservative: precision over recall. A false accusation is worse than a miss.
- A later message from you that mentions a fenced path **supersedes** the directive — supersession beats accusation.
- Directives given before a context compaction are analyzed with **reduced confidence**, not fake certainty.
- Subagent sessions are detected but not analyzed in v0.1 (`N subagent events detected, not analyzed`).

## Format drift

The Claude Code JSONL format is internal and changes between releases. agentlens uses a tolerant reader: unknown event types are counted and skipped (and surfaced in the report), malformed lines never crash the run, and every session's agent version is checked against the tested fixture corpus — when your version is outside it, agentlens says so and keeps going. If it misparses your session, please [open an issue](../../issues) with an anonymized fixture.

## Architecture

```
ingest/    agent-specific adapters (v0.1: claudecode) → normalized events
model/     normalized event + finding schema
detect/    detectors: Scan(session) → findings   ← add yours here (single file)
render/    terminal / HTML / JSON
fixtures/  real anonymized sessions per agent version, CI-tested
```

The adapter interface is agent-agnostic by design; Cursor and friends are v0.2. Adding a detector is a single-file contribution.

## Roadmap

- **v0.2** — Cursor adapter · hallucinated-API detector · `agentlens trend` (cross-session aggregates) · subagent session graphs
- **v0.3** — CI mode (fail a PR if the session had violations) · team aggregate reports
- **v1.0** — pluggable detector SDK · published Agent Failure Taxonomy

## License

MIT

---

*Want this recorded permanently in git history? → [gitwhy](https://github.com/surajsrivastav/gitwhy)*

# agentlens

**See every mistake your coding agent made last session.**

I got tired of reviewing my agent's final diff and having no idea what actually happened to get there — which instructions it quietly ignored, which tests it "fixed" by deleting the assertion, how many times it rewrote the same file chasing its own tail. The data for all of this was sitting right there in `~/.claude/projects/`, unread. So agentlens reads it for you and tells you straight.

Zero config. Read-only — it diagnoses, it never touches your code.

🔒 **100% local — no network calls, no telemetry.** Your session content never leaves your machine.

## Install

```sh
go install github.com/surajsrivastav/agentlens@latest
```

## Use

```sh
cd my-repo
agentlens analyze
```

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

That's the "holy shit" moment — a real finding from your own session, not a demo. Everything else in this tool exists to get you there faster.

More:

```sh
agentlens analyze --explain            # show the evidence behind each finding
agentlens analyze --html report.html   # shareable single-file report, dark mode
agentlens analyze --json               # machine-readable, for scripting
agentlens analyze --fail-on violation  # exit 1 if a violation was found — for CI
agentlens trend                        # aggregate findings across every session in this repo
agentlens sessions                     # list sessions found for this repo
```

## What it looks for

- **Instruction violations** — you said "don't touch `config.yaml`"; the agent edited it anyway.
- **Test integrity** — assertions removed, tests deleted or skipped, `assertEqual` quietly weakened to `assertTrue`.
- **Rework loops** — a file rewritten 3+ times, or worse, oscillating A→B→A, with the token cost that burned.
- **Hallucinated APIs** *(Go, Python, TypeScript/JavaScript)* — the agent calls `computeTotally(x)` like it's a real function. It isn't, anywhere in your repo. This one's genuinely uncertain by nature (see below), so it's held to a higher bar than the others — Go findings carry medium confidence (real AST parse), Python/TS carry low confidence (heuristic, no AST — see below).

## Two more ways to use it

**`agentlens trend`** rolls up every session agentlens can find for the current repo into one table — dates, durations, token spend, and finding counts side by side, oldest first. Good for "is this getting better or worse" instead of "what happened once."

**CI mode.** `--fail-on violation` (or `warning`, or `info`) turns a session review into a gate: wire it into a GitHub Action and a PR fails if the session behind it had a real instruction violation. Example:

```yaml
- name: Check the agent's session for violations
  run: agentlens analyze --fail-on violation
```

## Trust rules — read this before you trust a finding

The tool presents evidence; you render the verdict. I'd rather this tool find nothing than falsely accuse your agent of something it didn't do, so:

- Every finding carries a confidence label. `--explain` shows you the exact prompt line and the exact tool call behind it — check it yourself before you believe it.
- Detectors are conservative on purpose. If you later say "actually, go ahead," the earlier directive is treated as superseded, not violated.
- The hallucinated-API detector's Go path parses real Go syntax (not a text-guessing regex) so it doesn't mistake a string literal or comment for a function call. Python and TypeScript/JavaScript don't have a Go-stdlib parser available — pulling in a real one means a cgo dependency, which would break plain `go install` for everyone — so they get a masking lexer (strip strings/comments, same fix) plus regex-based declaration/import extraction instead. That's why their findings carry low, not medium, confidence: it's tested against real-world code (the actual Python 3.9 stdlib and a production TypeScript monorepo, not just curated fixtures) and tuned down to a low single-digit false-positive rate, but it's still a heuristic, not a parser. All three only flag a call if the name resolves to nothing anywhere in your repo today, as-is.
- Unknown log formats degrade loudly, never silently. Unrecognized events, malformed lines, subagent sessions (not analyzed yet), and untested Claude Code versions are all surfaced, not swallowed. If agentlens misparses your session, [open an issue](../../issues) with an anonymized fixture and I'll fix it.

**Cursor support is experimental**, and I want to be upfront about why: Cursor's local chat storage isn't documented anywhere, by anyone, officially. What's implemented here is built from community reverse-engineering with zero real Cursor logs to check it against — I don't run Cursor. It reads prompts and assistant text; it deliberately does *not* try to guess at Cursor's tool-call/file-edit schema, so the instruction/test/rework detectors will find nothing in a Cursor session for now. If you use Cursor, an anonymized `state.vscdb` (or even just the two relevant rows) would let me move this from experimental to actually tested — please send one.

## Contributing

```
ingest/    agent adapters (claudecode, cursor) → normalized events
detect/    detectors — add yours as a single file implementing Detector
render/    terminal / HTML / JSON
fixtures/  anonymized sessions per agent version, CI-tested
```

Adding a detector is meant to be a weekend, not a rewrite: implement `Scan(*model.Session) []model.Finding`, register it in `detect.All()`, done.

Roadmap: CI mode and cross-session trends are done; next up is a real Cursor fixture corpus, then a published "Agent Failure Taxonomy" and a pluggable detector SDK.

## License

MIT

---

*Want this recorded permanently in git history? → [gitwhy](https://github.com/surajsrivastav/gitwhy)*

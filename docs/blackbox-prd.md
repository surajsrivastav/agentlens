# PRD: `blackbox` — Flight Recorder for Coding Agents

**Author:** Suraj
**Status:** Draft v0.1
**Date:** July 6, 2026
**Working name:** `blackbox` (alternates: `agentcrash`, `postmortem`, `whatdiditdo`) — validate availability on GitHub/brew/crates before launch.

---

## 1. One-liner

A zero-config CLI that reads the coding-agent session logs already on your disk and tells you every mistake your agent made: instructions it ignored, tests it deleted, code it hallucinated, and tokens it burned going in circles.

## 2. Problem

Developers using Claude Code, Cursor, and similar agents have no visibility into agent behavior after the fact. The agent edits dozens of files across a long session; the developer reviews the final diff, not the *process*. Failure modes that matter — silent instruction violations, test weakening, redundant rework loops, hallucinated APIs — are invisible until they cause a production incident or a blown token budget.

The evidence already exists. Claude Code persists complete session transcripts as JSONL under `~/.claude/projects/`. Cursor and other agents keep equivalents. Nobody reads them because they are large, unstructured, and unreadable by humans.

**The gap:** the data is universal, the pain is daily and loud (see any Claude Code / Cursor forum), and no tool occupies the "audit what my agent did" slot.

## 3. Why now / why us

- Agent adoption is outpacing agent trust. Every team running agents has this problem; no default tool exists yet.
- The input data requires **zero behavior change** — unlike provenance capture (gitwhy), which requires forward adoption.
- Author advantage: operating experience across a large agent fleet (BLUE, 190+ teams), existing JSONL session-bridge parsing work from gitwhy, an articulated SDD/drift-detection thesis, and proven launch distribution (HN + LinkedIn).
- Strategic fit: top-of-funnel for gitwhy and Spoke; conference-talk and research-artifact material.

## 4. Goals

1. **Time-to-insight under 60 seconds** from `brew install` to a report the user screenshots.
2. Become the default answer to "why did my agent do that?" in the Claude Code ecosystem within 6 months.
3. Generate the launch artifact: a shareable report that carries the product on HN/LinkedIn/X.

**Success metrics (12 months):**
- 1,000 stars in first 30 days (validates trending path); 5–10k in 12 months (bull case).
- ≥3 external blog posts / videos featuring the report output.
- ≥1 conference talk sourced from aggregate findings ("what N sessions taught us about agent failure modes").
- Cited in ≥2 agent-reliability or agent-safety discussions/papers.

## 5. Non-goals (v0.1)

- ❌ No SaaS, dashboard, or hosted anything.
- ❌ No real-time monitoring or live session interception (that's Spoke territory).
- ❌ No Temporal, no server, no daemon.
- ❌ No fixing/blocking behavior — read-only analysis. We diagnose; we do not intervene.
- ❌ No LLM calls required for core detectors (deterministic parsing first; optional LLM-assisted analysis later).
- ❌ No support for agents beyond Claude Code in v0.1 (but the parser interface must be agent-agnostic — see §9).

## 6. Target user

**Primary:** Individual developer using Claude Code daily, suspicious that the agent is doing things they didn't ask for, or burning budget. Installs via brew, runs one command, gets a verdict.

**Secondary (v0.2+):** Tech lead running a team on agents, wants per-repo or per-week aggregate reliability reports.

**Tertiary (not now):** Platform/enablement org wanting fleet-level governance → funnel to Spoke.

## 7. Core user journey (v0.1)

```
$ brew install blackbox
$ cd my-repo
$ blackbox analyze              # finds latest Claude Code session for this repo
```

Output (terminal, with `--html` for shareable report):

```
BLACKBOX REPORT — session 2026-07-06 14:02 (47 min, 312 events, ~184k tokens)

⛔ INSTRUCTION VIOLATIONS (2)
  14:14  You said: "do not modify config.yaml"
         Agent edited config.yaml (3 lines changed)
  14:31  You said: "use the existing logger"
         Agent added new logging package (zap)

⚠️ TEST INTEGRITY (1)
  14:22  auth_test.go: 2 assertions removed while making TestLogin pass

🔁 REWORK LOOPS (1)
  auth.go rewritten 3× (14:10, 14:25, 14:38) — est. 41k tokens on repeated work

✅ 214 events clean
```

**The "holy shit" moment:** the first instruction violation surfaced from the user's own real session. Every design decision serves getting the user to that moment fastest.

## 8. Feature scope — v0.1 (two-weekend MVP)

Exactly three detectors. Anything beyond these before launch is scope creep.

### D1 — Instruction Violation Detector
- Extract user directives from prompts (imperative + negation patterns: "don't touch X", "only modify Y", "use Z not W", "keep A unchanged").
- Cross-reference against tool-call events (file edits, bash commands, package installs).
- Flag edits to files/paths/behaviors the user explicitly fenced off.
- Precision over recall: a false accusation destroys trust in the tool. Ship conservative patterns; log near-misses for tuning.

### D2 — Test Integrity Detector
- Detect edits to test files (`*_test.go`, `test_*.py`, `*.test.ts`, `*.spec.*`, etc.).
- Classify: assertion removal, test deletion, skip/disable annotations (`t.Skip`, `@pytest.mark.skip`, `xit`, `.skip`), assertion weakening (`assertEqual` → `assertTrue`).
- Correlate with nearby "make the tests pass" context to rank severity.

### D3 — Rework Loop Detector
- Track per-file edit counts and edit-distance between successive versions within a session.
- Flag files rewritten ≥3× or oscillating (A→B→A patterns).
- Estimate token cost attributable to rework (from event token counts where available).

### Report output
- Terminal renderer (default): color, compact, screenshot-friendly.
- `--html`: single self-contained HTML file, timeline layout, dark mode. This is the shareable artifact — invest disproportionate polish here.
- `--json`: machine-readable, for CI or downstream tooling.
- Footer: "Want this recorded permanently in git history? → gitwhy" (the funnel).

### CLI surface (complete for v0.1)
```
blackbox analyze [--session <id>] [--html out.html] [--json]
blackbox sessions            # list discovered sessions for cwd
blackbox --version
```
No config file. No flags beyond these. Zero-config is a feature.

## 9. Architecture

- **Language:** Go. Single static binary, brew + `go install` + GitHub releases. Reuse gitwhy's JSONL parsing experience.
- **Layered design:**
  1. `ingest/` — agent-specific adapters. v0.1 ships `claudecode` only, but the adapter interface (`SessionSource → []Event`) is the contract everything else depends on. This is the moat: cross-agent from day one architecturally, single-agent in scope.
  2. `model/` — normalized event schema (prompt, tool_call, file_edit, bash, tokens, timestamps).
  3. `detect/` — detectors implementing `Detector interface { Scan([]Event) []Finding }`. Adding a detector must be a single-file contribution — this is the community-contribution surface.
  4. `render/` — terminal / HTML / JSON.
- **Privacy stance (non-negotiable, front of README):** 100% local. No network calls. No telemetry in v0.1. Session content never leaves the machine. This is both ethically required (logs contain proprietary code) and the credibility basis for "independent auditor" positioning.
- **Performance target:** analyze a 50MB session file in <5s.

## 10. Launch plan

1. **Pre-launch:** run against own BLUE/gitwhy session archives; harvest 3–5 anonymized real findings for the README and launch posts. The README leads with a report screenshot, not an architecture diagram.
2. **Show HN:** "Show HN: Blackbox – see every mistake your coding agent made last session." Lead with a finding, not the tech.
3. **LinkedIn:** thesis framing — "We audit humans' code. Nobody audits the agent's behavior." Ties to SDD/governance body of work.
4. **Follow-up content engine:** weekly "failure mode of the week" posts from aggregate patterns → sustains trending momentum and builds the conference-talk dataset.

## 11. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Indie competitor ships first | **High, near-term** | Two-weekend MVP discipline; launch before polishing |
| Anthropic ships first-party session review | Medium, 6–18 mo | Independence + cross-agent positioning (vendor can't credibly self-audit; vendor won't build multi-agent) |
| False-positive violations erode trust | High | Conservative detector patterns, confidence labels, easy `--explain` showing the evidence for each finding |
| Log format changes break parser | Medium | Adapter isolation; version-sniff the log format; CI against fixture corpus |
| Scope creep delays launch | High (self-inflicted, known pattern) | v0.1 = 3 detectors, 3 commands, hard stop |

## 12. Roadmap (post-v0.1, directional only)

- **v0.2:** Cursor adapter; hallucinated-API detector (symbol existence check against repo index); `blackbox trend` (cross-session aggregates).
- **v0.3:** CI mode (fail PR if session had violations); team aggregate reports.
- **v1.0:** pluggable detector SDK; published "Agent Failure Taxonomy" (the research artifact).
- **Commercial adjacency (not OSS scope):** fleet-level, real-time, policy-enforcing version = Spoke integration.

## 13. Open questions

1. Name — needs a decision before repo creation; check brew formula collisions.
2. License — MIT (max adoption, category-default play) vs AGPL (protects against SaaS wrappers, consistent with Spoke). Recommendation: **MIT** for this one; the moat is the taxonomy and the community, not the code.
3. Directive extraction: pure regex/heuristics, or optional local-LLM assist behind a flag? v0.1 answer: heuristics only.
4. How much of gitwhy's session-bridge parser is directly liftable?

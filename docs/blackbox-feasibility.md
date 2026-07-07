# Tech Feasibility Analysis: `blackbox`

**Companion to:** blackbox-prd.md · **Date:** July 6, 2026
**Verdict up front:** Feasible in the two-weekend window, with one major risk (format instability) that has a concrete mitigation, one detector (D1) that needs a scoped-down v0.1 definition, and one uncomfortable market finding you need to see.

---

## 1. The uncomfortable finding first

The session-log *viewing* layer is already a crowded space: claude-code-log (Python, HTML/MD renderer), lm-assist (web UI session browser), claude-devtools (structured viewer with token attribution and search), Claude Code Tracer (live TUI/desktop viewer), claude-JSONL-browser (web converter). Several are actively maintained and one is heavily SEO'd.

**Implication:** "read the logs and render them" is a solved, commoditized problem — do not build another viewer. The open slot is the **judgment layer**: none of these tools detect failure modes. They show *what happened*; nothing on the market says *what went wrong*. This sharpens the PRD, it doesn't kill it. Detectors and findings are the entire product; rendering is a commodity you keep minimal. It also raises launch urgency — the ecosystem is clearly circling this data.

## 2. Input data: confirmed structure

The raw material is confirmed and rich:

- Location: `~/.claude/projects/<encoded-project-path>/<session-id>.jsonl` (project path with non-alphanumerics replaced; per-session file named by UUID). Windows: `%USERPROFILE%\.claude\projects\`.
- One JSON object per line, append-only. Top-level fields: `type`, `uuid`, `parentUuid`, `timestamp`, `sessionId`, `cwd`, `gitBranch`, `version`.
- `message.content` blocks: `text`, `thinking`, `tool_use` (id, name, input), `tool_result` (references `tool_use_id`).
- Token accounting per assistant turn in `message.usage`: `input_tokens`, `output_tokens`, cache creation/read tokens.
- Sessions include system prompt at start, compaction summary records, subagent spawns linked via `parentUuid` chains (subagent state can span multiple files).

Everything the three detectors need — user prompts, exact tool inputs (file paths, old/new strings, bash commands), timestamps, token counts — is present.

## 3. Risk register (ranked)

### R1 — Format instability (HIGH, the defining engineering risk)
Anthropic's docs explicitly state the JSONL entry format is internal to Claude Code and changes between versions; direct parsers can break on any release. Field practitioners confirm: the format is undocumented, discovered by observation, full of edge cases, and most entry types are noise.

**Mitigation (solution §5):** tolerant-reader parsing + fixture-corpus CI + version-sniffing + hook-based capture as an optional stable channel. This is manageable, not disqualifying — every competitor viewer lives with the same risk, and your adapter-isolation architecture is designed for exactly this.

### R2 — D1 (instruction violations) false positives (HIGH product risk, MEDIUM technical)
Natural-language directive extraction is genuinely hard: negations can be conditional ("don't touch X *yet*"), temporal ("skip tests *for now*"), superseded by later user messages ("actually, go ahead"), or scoped ambiguously. A false accusation in the launch screenshot kills credibility.

**Mitigation:** ship D1 as a deliberately narrow v0.1:
- Only high-confidence patterns: explicit file/path fencing ("don't modify/touch/change/edit `<path>`", "leave `<path>` alone", "only change `<path>`").
- Directive lifecycle tracking: a later user message mentioning the same path re-opens it (supersession beats accusation).
- Every finding carries a confidence label + `--explain` showing the exact prompt line and the exact tool_use event as evidence. The tool presents evidence; the human renders the verdict. This framing is both honest and legally/socially safer.
- Compaction caveat: directives given before a compaction event may survive only in summarized form. v0.1: mark post-compaction analysis as reduced-confidence rather than pretending certainty.

### R3 — Subagent/multi-file session graphs (MEDIUM)
Team/subagent sessions spread state across multiple JSONL files linked by parent UUIDs; reconstruction requires explicit merge phases. **v0.1 answer: don't.** Analyze the main session file only; print "N subagent sessions detected, not analyzed (v0.2)". Honest scoping beats silent wrongness.

### R4 — File size / retention (LOW)
Files can reach tens of MB; older sessions get compacted/trimmed/removed. Streaming line-by-line parse in Go handles size trivially (<5s / 50MB target is comfortable). Retention just means "analyze recent sessions" — which is the use case anyway.

## 4. Detector-by-detector feasibility

| Detector | Feasibility | Approach | Notes |
|---|---|---|---|
| **D2 Test integrity** | **HIGH — build first** | Filter `tool_use` events where `name` ∈ {Edit, Write, MultiEdit} and file path matches test conventions. Diff `old_string`/`new_string` inputs. Regex tier: skip annotations (`t.Skip`, `@pytest.mark.skip`, `.skip`, `xit`), assertion deletions, `assertEqual→assertTrue` weakenings. | Deterministic, evidence is literal tool input. Optional tree-sitter AST tier post-launch for precision; regex tier is launch-sufficient. |
| **D3 Rework loops** | **HIGH** | Group edit events per file; flag ≥3 rewrites or A→B→A oscillation via normalized edit distance between successive contents. Attribute token cost from `message.usage` of turns in the loop window. | Pure counting + arithmetic on data that's definitely present. Token attribution is an estimate — label it as such. |
| **D1 Instruction violations** | **MEDIUM — scoped as in R2** | Extract path-fencing directives from user `text` blocks; maintain directive state machine (active/superseded); match against subsequent tool_use file paths. | The demo-magic detector and the hardest. Conservative v0.1 pattern set; optional LLM-assist flag deferred (keeps zero-network promise). |

Build order: **D3 → D2 → D1.** Get wins on the deterministic ones while the D1 pattern corpus matures against your own session archive.

## 5. Solution architecture

Pipeline (validated independently by every serious parser in this space): **raw line → tolerant parse → classify → normalized event → detectors → findings → render.**

```
ingest/
  claudecode/       # adapter: discovery + version sniff + tolerant JSONL reader
model/
  event.go          # Prompt | ToolCall | ToolResult | Compaction | SessionMeta
detect/
  rework.go  testintegrity.go  instructions.go   # Detector: Scan([]Event) []Finding
render/
  terminal.go  html.go  json.go
fixtures/
  v2.1.x/ v2.2.x/ ...   # real anonymized session files per CC version, CI-tested
```

Key engineering decisions:

1. **Tolerant reader:** unknown top-level `type`s → skipped and counted (surfaced as "N unrecognized events" so silent drift is visible); unknown fields ignored; malformed lines logged, never fatal. Assume most entry types are noise; allowlist what detectors need, ignore the rest.
2. **Version sniffing:** every line carries `version`; the adapter records the range seen and warns when it exceeds the tested fixture set — turning "breaks on any release" into "degrades loudly with a clear issue-report path."
3. **Fixture corpus CI:** harvest sessions from your own BLUE/gitwhy work across CC versions, anonymize, commit as fixtures. Every format break becomes a failing test + a community contribution surface ("add a fixture from your version").
4. **Stable-channel hedge:** Claude Code hooks (e.g., PostToolUse) are a supported extension surface. Optional `blackbox install-hooks` writes events to blackbox's *own* stable schema going forward — historical analysis stays best-effort JSONL parsing, forward analysis gets a format you control. This is your gitwhy session-bridge pattern, reused. Anthropic also recommends `/export` / script interfaces as the sanctioned path — worth a v0.2 adapter as a second stable channel.
5. **Discovery:** encode `cwd` the same way CC does to map current repo → project dir; handle both the flat `<session-id>.jsonl` layout and any `sessions/` sublayout variants observed in the wild (accounts differ across versions — discover by glob, not by assumption).
6. **Go stack:** stdlib `encoding/json` streaming decoder + `bufio.Scanner` (raised buffer for long lines — tool results embedding file contents produce very long lines; this *will* bite the default 64KB scanner limit). No heavyweight deps for v0.1. HTML report via `html/template`, single self-contained file.

## 6. Feasibility verdict

| Dimension | Verdict |
|---|---|
| Data availability | ✅ Confirmed — everything needed is on disk |
| Parsing | ✅ Feasible; format drift is real but mitigable, and mitigation doubles as community surface |
| D2/D3 detectors | ✅ Deterministic, low-risk, two-weekend realistic |
| D1 detector | ⚠️ Feasible only in the narrowed path-fencing form; broaden post-launch with real-world corpus |
| Performance | ✅ Trivial for Go streaming parse |
| Differentiation | ⚠️ Viewer space is taken — judgment layer is open, but the window is measured in weeks |
| Overall | **GO — with D1 scoped down, subagents deferred, and launch urgency raised** |

## 7. Immediate next actions

1. Dump one line of your own latest session (`head -1 ... | jq .`) and confirm field names against §2 on your CC version — one hour of reality-checking before any code.
2. Build the tolerant reader + `blackbox sessions` discovery command (weekend 1, day 1).
3. D3, then D2, against your own real sessions — your BLUE/gitwhy archives are both the fixture corpus and the launch-post material.
4. D1 pattern set tuned on that corpus until zero false positives on your own history, then freeze for v0.1.

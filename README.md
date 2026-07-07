# agentlens

**See every mistake your coding agent made last session.**

agentlens reads the Claude Code session logs already on your disk and reports instructions the agent ignored, tests it weakened, and tokens it burned going in circles. Zero config. Read-only.

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

More:

```sh
agentlens analyze --explain            # show the evidence behind each finding
agentlens analyze --html report.html   # shareable single-file report
agentlens analyze --json               # machine-readable, for CI / tooling
agentlens sessions                     # list sessions found for this repo
```

## What it detects

- **Instruction violations** — you said "don't touch `config.yaml`"; the agent edited it anyway.
- **Test integrity** — assertions removed, tests deleted or skipped, `assertEqual` weakened to `assertTrue`.
- **Rework loops** — files rewritten 3+ times or oscillating A→B→A, with the estimated token cost.

## Trust rules

The tool presents evidence; you render the verdict.

- Every finding has a confidence label; `--explain` shows the exact prompt line and tool call behind it.
- Detectors are conservative — precision over recall. If you later say "go ahead", the directive is superseded and not counted.
- Unknown log formats degrade loudly, never silently: unrecognized events, malformed lines, and untested Claude Code versions are all reported. Misparse? [Open an issue](../../issues) with an anonymized fixture.

## Contributing

```
ingest/    agent adapters (v0.1: claudecode) → normalized events
detect/    detectors — add yours as a single file
render/    terminal / HTML / JSON
fixtures/  anonymized sessions per agent version, CI-tested
```

Roadmap: Cursor adapter, hallucinated-API detector, cross-session trends (v0.2) · CI mode (v0.3) · detector SDK (v1.0).

## License

MIT

---

*Want this recorded permanently in git history? → [gitwhy](https://github.com/surajsrivastav/gitwhy)*

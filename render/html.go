package render

import (
	"html/template"
	"io"
	"sort"
	"time"

	"github.com/surajsrivastav/agentlens/model"
)

// HTML writes the shareable single-file report: dark mode, timeline
// layout, zero external assets. This is the launch artifact — it gets
// disproportionate polish (PRD §8).
func HTML(w io.Writer, r *Report) error {
	s := r.Session

	type item struct {
		Time       string
		Detector   string
		Class      string
		Icon       string
		Title      string
		Detail     string
		Confidence string
		Evidence   []model.Evidence
	}
	var items []item
	fs := append([]model.Finding(nil), r.Findings...)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Timestamp.Before(fs[j].Timestamp) })
	for _, f := range fs {
		it := item{
			Detector:   f.Detector,
			Title:      f.Title,
			Detail:     f.Detail,
			Confidence: f.Confidence.String(),
			Evidence:   f.Evidence,
		}
		if !f.Timestamp.IsZero() {
			it.Time = f.Timestamp.Local().Format("15:04")
		}
		switch f.Detector {
		case "instruction-violation":
			it.Class, it.Icon = "violation", "⛔"
		case "test-integrity":
			it.Class, it.Icon = "test", "⚠️"
		case "rework-loop":
			it.Class, it.Icon = "rework", "🔁"
		case "hallucinated-api":
			it.Class, it.Icon = "hallucinated", "👻"
		default:
			it.Class, it.Icon = "other", "🔎"
		}
		items = append(items, it)
	}

	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.Detector]++
	}

	data := map[string]any{
		"SessionID":      s.ID,
		"Date":           s.Start.Local().Format("Jan 2, 2006 15:04"),
		"Duration":       humanDuration(s),
		"Events":         len(s.Events),
		"Tokens":         humanK(s.TotalUsage.Fresh()),
		"CacheRead":      humanK(s.TotalUsage.CacheReadTokens),
		"CWD":            s.CWD,
		"Branch":         s.GitBranch,
		"Items":          items,
		"Violations":     counts["instruction-violation"],
		"TestIssues":     counts["test-integrity"],
		"ReworkLoops":    counts["rework-loop"],
		"Hallucinations": counts["hallucinated-api"],
		"Clean":          r.CleanEvents,
		"Warnings":       r.Warnings,
		"Funnel":         funnelFooter,
		"Version":        r.Version,
		"Generated":      time.Now().Local().Format("Jan 2, 2006 15:04"),
	}
	return htmlTmpl.Execute(w, data)
}

var htmlTmpl = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>agentlens report — {{.SessionID}}</title>
<style>
:root{
  --bg:#0d1117;--panel:#161b22;--border:#30363d;--fg:#e6edf3;--dim:#8b949e;
  --red:#f85149;--yellow:#d29922;--blue:#58a6ff;--green:#3fb950;--purple:#bc8cff;
  --mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace;
}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--fg);font:15px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;padding:40px 20px}
.wrap{max-width:860px;margin:0 auto}
header h1{font-size:22px;letter-spacing:.5px}
header h1 .lens{color:var(--blue)}
header .meta{color:var(--dim);font-family:var(--mono);font-size:13px;margin-top:6px}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:12px;margin:28px 0}
.stat{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:14px 16px}
.stat .n{font-size:26px;font-weight:700;font-family:var(--mono)}
.stat .l{color:var(--dim);font-size:12px;text-transform:uppercase;letter-spacing:.8px;margin-top:2px}
.stat.violation .n{color:var(--red)}.stat.test .n{color:var(--yellow)}
.stat.rework .n{color:var(--purple)}.stat.clean .n{color:var(--green)}
.stat.hallucinated .n{color:var(--blue)}
.timeline{position:relative;margin:36px 0 0 8px;padding-left:28px;border-left:2px solid var(--border)}
.entry{position:relative;margin-bottom:22px}
.entry::before{content:"";position:absolute;left:-35px;top:8px;width:12px;height:12px;border-radius:50%;background:var(--dim);border:2px solid var(--bg)}
.entry.violation::before{background:var(--red)}
.entry.test::before{background:var(--yellow)}
.entry.rework::before{background:var(--purple)}
.entry.hallucinated::before{background:var(--blue)}
.card{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:14px 18px}
.card .head{display:flex;gap:10px;align-items:baseline;flex-wrap:wrap}
.card .time{font-family:var(--mono);color:var(--dim);font-size:13px}
.card .title{font-weight:600}
.card .detail{color:var(--dim);margin-top:6px;font-size:14px}
.badge{font-size:11px;font-family:var(--mono);border:1px solid var(--border);border-radius:20px;padding:1px 9px;color:var(--dim);margin-left:auto;white-space:nowrap}
.entry.violation .badge{border-color:var(--red);color:var(--red)}
.entry.test .badge{border-color:var(--yellow);color:var(--yellow)}
.entry.rework .badge{border-color:var(--purple);color:var(--purple)}
.entry.hallucinated .badge{border-color:var(--blue);color:var(--blue)}
details{margin-top:10px}
summary{cursor:pointer;color:var(--blue);font-size:13px;user-select:none}
.evidence{margin-top:8px;background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:10px 12px}
.evidence .lbl{color:var(--dim);font-size:11px;text-transform:uppercase;letter-spacing:.8px}
.evidence pre{font-family:var(--mono);font-size:12.5px;white-space:pre-wrap;word-break:break-word;margin-top:4px;color:var(--fg)}
.clean-banner{margin-top:28px;background:var(--panel);border:1px solid var(--border);border-left:4px solid var(--green);border-radius:10px;padding:14px 18px;color:var(--green);font-weight:600}
.warn{color:var(--dim);font-size:13px;margin-top:10px}
.warn::before{content:"⚠ ";color:var(--yellow)}
footer{margin-top:44px;padding-top:18px;border-top:1px solid var(--border);color:var(--dim);font-size:13px;display:flex;justify-content:space-between;flex-wrap:wrap;gap:8px}
footer a{color:var(--blue);text-decoration:none}
</style>
</head>
<body>
<div class="wrap">
<header>
  <h1>agent<span class="lens">lens</span> report</h1>
  <div class="meta">session {{.SessionID}} · {{.Date}} · {{.Duration}}{{if .CWD}} · {{.CWD}}{{end}}{{if .Branch}} ({{.Branch}}){{end}}</div>
</header>

<div class="stats">
  <div class="stat violation"><div class="n">{{.Violations}}</div><div class="l">Instruction violations</div></div>
  <div class="stat test"><div class="n">{{.TestIssues}}</div><div class="l">Test integrity</div></div>
  <div class="stat rework"><div class="n">{{.ReworkLoops}}</div><div class="l">Rework loops</div></div>
  <div class="stat hallucinated"><div class="n">{{.Hallucinations}}</div><div class="l">Hallucinated APIs</div></div>
  <div class="stat clean"><div class="n">{{.Clean}}</div><div class="l">Events clean</div></div>
  <div class="stat"><div class="n">{{.Events}}</div><div class="l">Events</div></div>
  <div class="stat"><div class="n">~{{.Tokens}}</div><div class="l">Fresh tokens</div></div>
</div>

{{if .Items}}
<div class="timeline">
  {{range .Items}}
  <div class="entry {{.Class}}">
    <div class="card">
      <div class="head">
        {{if .Time}}<span class="time">{{.Time}}</span>{{end}}
        <span class="title">{{.Icon}} {{.Title}}</span>
        <span class="badge">{{.Confidence}} confidence</span>
      </div>
      {{if .Detail}}<div class="detail">{{.Detail}}</div>{{end}}
      {{if .Evidence}}
      <details>
        <summary>evidence ({{len .Evidence}})</summary>
        {{range .Evidence}}
        <div class="evidence"><div class="lbl">{{.Label}}</div><pre>{{.Content}}</pre></div>
        {{end}}
      </details>
      {{end}}
    </div>
  </div>
  {{end}}
</div>
{{else}}
<div class="clean-banner">✅ No findings — this session looks clean.</div>
{{end}}

{{if .Items}}<div class="clean-banner">✅ {{.Clean}} events clean</div>{{end}}

{{range .Warnings}}<div class="warn">{{.}}</div>{{end}}

<footer>
  <span>{{.Funnel}}</span>
  <span>agentlens {{.Version}} · generated {{.Generated}} · 100% local, no network calls</span>
</footer>
</div>
</body>
</html>
`))

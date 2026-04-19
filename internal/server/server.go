package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"shelby/internal/config"
	"shelby/internal/store"
)

// Server wraps a Scheduler with an HTTP dashboard + JSON API.
type Server struct {
	Sched *Scheduler
	Store *store.Store
	Addr  string
}

func NewServer(sched *Scheduler, st *store.Store, addr string) *Server {
	return &Server{Sched: sched, Store: st, Addr: addr}
}

// ListenAndServe starts the HTTP listener. Call Shutdown via returned *http.Server.
func (s *Server) ListenAndServe(ctx context.Context) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/p/", s.handleDetail)
	mux.HandleFunc("/api/pipelines", s.handleAPIList)
	mux.HandleFunc("/api/pipelines/", s.handleAPIPipeline) // /api/pipelines/<slug>/run

	hs := &http.Server{Addr: s.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("http: %v\n", err)
		}
	}()
	return hs
}

type pipelineView struct {
	Slug     string
	Name     string
	Path     string
	Interval string
	LastRun  string
	Status   string
	Duration string
	RunID    string
	Error    string
}

func (s *Server) collect() []pipelineView {
	regs, _ := s.Store.List()
	out := make([]pipelineView, 0, len(regs))
	for _, r := range regs {
		pv := pipelineView{Slug: r.Slug, Name: r.Name, Path: r.Path, Interval: "?", LastRun: "-", Status: "-"}
		if p, err := config.Load(r.Path); err == nil {
			pv.Interval = p.Interval.String()
		}
		if last, _ := s.Store.LastRun(r.Slug); last != nil {
			pv.LastRun = last.StartedAt.Local().Format("2006-01-02 15:04:05")
			pv.Status = last.Status
			pv.Duration = last.Duration.String()
			pv.RunID = last.RunID
			pv.Error = last.Error
		}
		out = append(out, pv)
	}
	return out
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Shelby</title>
<meta http-equiv="refresh" content="5">
<style>
body{font-family:ui-monospace,monospace;background:#0e1116;color:#d7dde5;margin:24px;}
h1{color:#9cdcfe;margin:0 0 4px 0;}
.sub{color:#6a737d;margin-bottom:16px;}
table{border-collapse:collapse;width:100%;}
th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #222;}
th{color:#6a737d;font-weight:normal;}
tr:hover{background:#161b22;}
a{color:#9cdcfe;text-decoration:none;}
a:hover{text-decoration:underline;}
.ok{color:#4ade80;} .fail{color:#f87171;} .dim{color:#6a737d;}
form{display:inline;}
button{background:#1f2937;color:#d7dde5;border:1px solid #374151;padding:3px 10px;border-radius:4px;cursor:pointer;}
button:hover{background:#2d3748;}
</style></head>
<body>
<h1>SHELBY</h1>
<div class="sub">{{len .Pipelines}} pipelines · {{.Root}} · auto-refresh 5s</div>
<table>
<tr><th>SLUG</th><th>NAME</th><th>INTERVAL</th><th>LAST RUN</th><th>STATUS</th><th>DUR</th><th></th></tr>
{{range .Pipelines}}
<tr>
<td><a href="/p/{{.Slug}}">{{.Slug}}</a></td>
<td>{{.Name}}</td>
<td>{{.Interval}}</td>
<td>{{.LastRun}}</td>
<td class="{{if eq .Status "ok"}}ok{{else if eq .Status "fail"}}fail{{else}}dim{{end}}">{{.Status}}</td>
<td class="dim">{{.Duration}}</td>
<td><form method="post" action="/api/pipelines/{{.Slug}}/run"><button type="submit">run</button></form></td>
</tr>
{{else}}
<tr><td colspan="7" class="dim">no pipelines registered. use: shelby add &lt;file.yaml&gt;</td></tr>
{{end}}
</table>
</body></html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := struct {
		Root      string
		Pipelines []pipelineView
	}{Root: s.Store.Root, Pipelines: s.collect()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, data)
}

var detailTmpl = template.Must(template.New("detail").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Reg.Name}} · Shelby</title>
<style>
body{font-family:ui-monospace,monospace;background:#0e1116;color:#d7dde5;margin:24px;}
h1{color:#9cdcfe;margin:0;} h2{color:#9cdcfe;margin-top:24px;}
.sub{color:#6a737d;margin-bottom:16px;}
table{border-collapse:collapse;width:100%;}
th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #222;}
th{color:#6a737d;font-weight:normal;}
.ok{color:#4ade80;} .fail{color:#f87171;} .dim{color:#6a737d;}
a{color:#9cdcfe;} pre{background:#161b22;padding:12px;border-radius:6px;overflow:auto;}
button{background:#1f2937;color:#d7dde5;border:1px solid #374151;padding:6px 14px;border-radius:4px;cursor:pointer;}
</style></head>
<body>
<a href="/">← back</a>
<h1>{{.Reg.Name}}</h1>
<div class="sub">{{.Reg.Slug}} · {{.Reg.Path}}</div>
<form method="post" action="/api/pipelines/{{.Reg.Slug}}/run"><button type="submit">run now</button></form>
<h2>recent runs</h2>
<table>
<tr><th>WHEN</th><th>RUN</th><th>STATUS</th><th>DUR</th><th>STEPS</th><th>ERROR</th></tr>
{{range .Runs}}
<tr>
<td>{{.StartedAt.Local.Format "2006-01-02 15:04:05"}}</td>
<td class="dim">{{.RunID}}</td>
<td class="{{if eq .Status "ok"}}ok{{else}}fail{{end}}">{{.Status}}</td>
<td class="dim">{{.Duration}}</td>
<td>{{len .Steps}}</td>
<td class="fail">{{.Error}}</td>
</tr>
{{else}}
<tr><td colspan="6" class="dim">no runs yet</td></tr>
{{end}}
</table>
{{if .Last}}
<h2>last run steps</h2>
<table>
<tr><th>STEP</th><th>TYPE</th><th>OK</th><th>DUR</th><th>ERROR</th></tr>
{{range .Last.Steps}}
<tr><td>{{.ID}}</td><td>{{.Type}}</td>
<td class="{{if .OK}}ok{{else}}fail{{end}}">{{if .OK}}yes{{else}}no{{end}}</td>
<td class="dim">{{.Duration}}</td><td class="fail">{{.Error}}</td></tr>
{{end}}
</table>
{{if .Last.Output}}
<h2>output</h2>
<pre>{{.OutputJSON}}</pre>
{{end}}
{{end}}
</body></html>`))

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/p/")
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	reg, err := s.Store.Get(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	runs, _ := s.Store.Runs(reg.Slug, 20)
	var last *store.RunRecord
	var outJSON string
	if len(runs) > 0 {
		last = &runs[0]
		if len(last.Output) > 0 {
			b, _ := json.MarshalIndent(last.Output, "", "  ")
			outJSON = string(b)
		}
	}
	data := struct {
		Reg        store.Registration
		Runs       []store.RunRecord
		Last       *store.RunRecord
		OutputJSON string
	}{Reg: reg, Runs: runs, Last: last, OutputJSON: outJSON}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = detailTmpl.Execute(w, data)
}

func (s *Server) handleAPIList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.collect())
}

// handleAPIPipeline routes /api/pipelines/<slug>/... — currently only .../run (POST).
func (s *Server) handleAPIPipeline(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "run" {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	res, ran, err := s.Sched.RunOnce(ctx, slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// if browser (form submit), redirect back
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/p/"+slug, http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ran":   ran,
		"run":   res.RunID,
		"error": errString(res.Err),
	})
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

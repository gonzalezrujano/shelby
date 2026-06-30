package viz

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/flosch/pongo2/v6"
)

//go:embed builtins
var builtinsFS embed.FS

// LLMsText returns the contents of the embedded llms.txt file.
func LLMsText() ([]byte, error) {
	return builtinsFS.ReadFile("builtins/llms.txt")
}

// WidgetRender pairs a widget definition with its rendered HTML.
// Fields use names that pongo2 can resolve via strings.Title lookup:
//   - wr.widget  → Widget *WidgetDef
//   - wr.html    → Html string
type WidgetRender struct {
	Widget *WidgetDef
	Html   string
}

// combinedLoader implements pongo2.ILoader, searching the user directory
// first and falling back to embedded builtins.
type combinedLoader struct {
	userDir string
	fsys    embed.FS
}

func (l *combinedLoader) Abs(base, name string) string {
	if base == "" || strings.HasPrefix(name, "/") {
		return name
	}
	dir := filepath.Dir(base)
	joined := filepath.Join(dir, name)
	// Normalise to forward slashes for embed.FS compatibility.
	return filepath.ToSlash(joined)
}

func (l *combinedLoader) Get(path string) (io.Reader, error) {
	// Try the user directory first.
	if l.userDir != "" {
		full := filepath.Join(l.userDir, filepath.FromSlash(path))
		if f, err := os.Open(full); err == nil {
			return f, nil
		}
	}
	// Fall back to the embedded builtins FS.
	embPath := "builtins/" + strings.TrimPrefix(path, "/")
	f, err := l.fsys.Open(embPath)
	if err != nil {
		return nil, fmt.Errorf("viz: template not found: %s", path)
	}
	return f, nil
}

func init() {
	// Register custom pongo2 filters required by the builtin templates.

	pongo2.RegisterFilter("float", func(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		return pongo2.AsValue(in.Float()), nil
	})

	pongo2.RegisterFilter("abs", func(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		f := in.Float()
		if f < 0 {
			f = -f
		}
		return pongo2.AsValue(f), nil
	})

	pongo2.RegisterFilter("filesizeformat", func(in *pongo2.Value, _ *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		v := in.Float()
		for _, unit := range []string{"B", "KB", "MB", "GB", "TB"} {
			if v < 1000 {
				return pongo2.AsValue(fmt.Sprintf("%.1f %s", v, unit)), nil
			}
			v /= 1000
		}
		return pongo2.AsValue(fmt.Sprintf("%.1f PB", v)), nil
	})

	pongo2.RegisterFilter("ratio", func(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		total := param.Float()
		if total == 0 {
			return pongo2.AsValue(0.0), nil
		}
		return pongo2.AsValue(in.Float() / total), nil
	})
}

// Renderer resolves widget bindings and renders pongo2 templates.
type Renderer struct {
	dashboardPath string
	shelbyURL     string
	pipelines     *PipelinesContext
	mock          *MockPipelinesContext
	tplSet        *pongo2.TemplateSet
}

// NewRenderer creates a live renderer backed by a Shelby API server.
func NewRenderer(dashboardPath, shelbyURL string) *Renderer {
	r := &Renderer{
		dashboardPath: resolveDashboardDir(dashboardPath),
		shelbyURL:     shelbyURL,
		pipelines:     &PipelinesContext{BaseURL: shelbyURL},
	}
	r.tplSet = buildTemplateSet(r.dashboardPath)
	return r
}

// NewSandboxRenderer creates a renderer using mock data from sandbox.yml.
func NewSandboxRenderer(dashboardPath string, mock *MockPipelinesContext) *Renderer {
	r := &Renderer{
		dashboardPath: resolveDashboardDir(dashboardPath),
		mock:          mock,
	}
	r.tplSet = buildTemplateSet(r.dashboardPath)
	return r
}

func resolveDashboardDir(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return filepath.Dir(path)
	}
	return path
}

func buildTemplateSet(userDir string) *pongo2.TemplateSet {
	loader := &combinedLoader{userDir: userDir, fsys: builtinsFS}
	return pongo2.NewSet("shelby-viz", loader)
}

// resolveBinding converts one binding config entry into live data.
func (r *Renderer) resolveBinding(cfg any) any {
	m, ok := cfg.(map[string]any)
	if !ok {
		return cfg
	}

	if _, hasSeries := m["series"]; hasSeries {
		slug, _ := m["pipeline"].(string)
		field, _ := m["series"].(string)
		limit := 50
		if v, ok := m["limit"]; ok {
			limit = int(toFloat64(v))
		}
		unit, _ := m["unit"].(string)
		if r.mock != nil {
			return r.mock.Get(slug).Series(field, limit, unit)
		}
		return r.pipelines.Get(slug).Series(field, limit, unit)
	}

	if runsVal, hasRuns := m["runs"]; hasRuns {
		slug, _ := m["pipeline"].(string)
		limit := 20
		if v, ok := runsVal.(int); ok {
			limit = v
		} else if v, ok := runsVal.(float64); ok {
			limit = int(v)
		}
		if r.mock != nil {
			return r.mock.Get(slug).Runs(limit)
		}
		return r.pipelines.Get(slug).Runs(limit)
	}

	if val, hasVal := m["value"]; hasVal {
		return val
	}

	return cfg
}

// RenderWidget fetches live data and renders one widget to an HTML string.
func (r *Renderer) RenderWidget(widget *WidgetDef) (string, error) {
	data := make(map[string]any, len(widget.Binding))
	for key, bindCfg := range widget.Binding {
		data[key] = r.resolveBinding(bindCfg)
	}

	templatePath := widget.Template
	if widget.Type != "custom" {
		templatePath = "widgets/" + widget.Type + ".html.j2"
	}

	tmpl, err := r.tplSet.FromCache(templatePath)
	if err != nil {
		return "", fmt.Errorf("viz: load template %s: %w", templatePath, err)
	}

	out, err := tmpl.Execute(pongo2.Context{
		"data":   data,
		"config": widget.Config,
		"meta": map[string]string{
			"title": widget.Title,
			"id":    widget.ID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("viz: render widget %s: %w", widget.ID, err)
	}
	return out, nil
}

// RenderDashboard renders the full dashboard page with all widgets inlined.
func (r *Renderer) RenderDashboard(dashboard *DashboardDef, liveReload bool) (string, error) {
	renders := make([]WidgetRender, 0, len(dashboard.Widgets))
	for _, w := range dashboard.Widgets {
		html, err := r.RenderWidget(w)
		if err != nil {
			html = widgetError(err)
		}
		renders = append(renders, WidgetRender{Widget: w, Html: html})
	}

	tmpl, err := r.tplSet.FromCache("shell/base.html")
	if err != nil {
		return "", fmt.Errorf("viz: load base template: %w", err)
	}

	out, err := tmpl.Execute(pongo2.Context{
		"dashboard":      dashboard,
		"widget_renders": renders,
		"shelby_url":     r.shelbyURL,
		"live_reload":    liveReload,
	})
	if err != nil {
		return "", fmt.Errorf("viz: render dashboard: %w", err)
	}
	return out, nil
}

// ValidateTemplates checks that all widget templates exist and parse without error.
// Returns a list of (widgetID, templatePath, error) tuples for each problem found.
func (r *Renderer) ValidateTemplates(dashboard *DashboardDef) []string {
	var errs []string
	for _, w := range dashboard.Widgets {
		tpath := w.Template
		if w.Type != "custom" {
			tpath = "widgets/" + w.Type + ".html.j2"
		}
		// Check template is reachable.
		accessible := false
		if r.dashboardPath != "" {
			if _, err := os.Stat(filepath.Join(r.dashboardPath, filepath.FromSlash(tpath))); err == nil {
				accessible = true
			}
		}
		if !accessible {
			embPath := "builtins/" + strings.TrimPrefix(tpath, "/")
			if _, err := fs.Stat(builtinsFS, embPath); err == nil {
				accessible = true
			}
		}
		if !accessible {
			errs = append(errs, fmt.Sprintf("  %s (%s): template not found: %s", w.ID, w.Type, tpath))
			continue
		}
		if _, err := r.tplSet.FromFile(tpath); err != nil {
			errs = append(errs, fmt.Sprintf("  %s (%s): parse error: %v", w.ID, w.Type, err))
		}
	}
	return errs
}

func widgetError(err error) string {
	return fmt.Sprintf(
		`<style>:host{display:flex;align-items:center;justify-content:center;height:100%%}.err{color:#f44336;font-size:.75rem;padding:8px;text-align:center}</style><div class="err">%v</div>`,
		err,
	)
}

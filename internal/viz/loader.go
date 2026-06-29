package viz

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// WidgetDef describes a single dashboard widget from dashboard.yml.
type WidgetDef struct {
	ID       string         `yaml:"id"`
	Title    string         `yaml:"title"`
	Type     string         `yaml:"type"`
	Col      int            `yaml:"col"`
	Row      int            `yaml:"row"`
	Width    int            `yaml:"width"`
	Height   int            `yaml:"height"`
	Binding  map[string]any `yaml:"binding"`
	Config   map[string]any `yaml:"config"`
	Template string         `yaml:"template"`
}

// Id returns the widget ID — accessible from pongo2 templates as widget.id.
func (w *WidgetDef) Id() string { return w.ID }

// DashboardDef is the parsed representation of a dashboard.yml file.
type DashboardDef struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Refresh     int            `yaml:"-"`
	Columns     int            `yaml:"-"`
	Widgets     []*WidgetDef   `yaml:"-"`
	Vars        map[string]any `yaml:"vars"`
}

// rawDashboard is used for YAML unmarshaling before transformation.
type rawDashboard struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Refresh     string         `yaml:"refresh"`
	Layout      struct {
		Columns int `yaml:"columns"`
	} `yaml:"layout"`
	Widgets []map[string]any `yaml:"widgets"`
	Vars    map[string]any   `yaml:"vars"`
}

// LoadDashboard loads a dashboard.yml from path (which may be a directory).
func LoadDashboard(path string) (*DashboardDef, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("viz: stat %s: %w", path, err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "dashboard.yml")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("viz: read %s: %w", path, err)
	}

	var raw rawDashboard
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("viz: parse %s: %w", path, err)
	}

	refresh := 60
	if raw.Refresh != "" {
		refresh = parseDuration(raw.Refresh)
	}
	columns := 12
	if raw.Layout.Columns > 0 {
		columns = raw.Layout.Columns
	}

	widgets := make([]*WidgetDef, 0, len(raw.Widgets))
	for _, w := range raw.Widgets {
		wtype, _ := w["type"].(string)
		if wtype == "" {
			wtype = "custom"
		}
		tmpl, _ := w["template"].(string)
		if wtype == "custom" && tmpl == "" {
			id, _ := w["id"].(string)
			return nil, fmt.Errorf("viz: widget %q has type=custom but no template field", id)
		}
		wd := &WidgetDef{
			ID:       stringVal(w, "id"),
			Title:    stringVal(w, "title"),
			Type:     wtype,
			Col:      intVal(w, "col", 1),
			Row:      intVal(w, "row", 1),
			Width:    intVal(w, "width", 4),
			Height:   intVal(w, "height", 2),
			Template: tmpl,
		}
		if b, ok := w["binding"].(map[string]any); ok {
			wd.Binding = b
		} else {
			wd.Binding = map[string]any{}
		}
		if c, ok := w["config"].(map[string]any); ok {
			wd.Config = c
		} else {
			wd.Config = map[string]any{}
		}
		widgets = append(widgets, wd)
	}

	name := raw.Name
	if name == "" {
		name = "Dashboard"
	}

	return &DashboardDef{
		Name:        name,
		Description: raw.Description,
		Refresh:     refresh,
		Columns:     columns,
		Widgets:     widgets,
		Vars:        raw.Vars,
	}, nil
}

// LoadSandbox loads sandbox.yml mock data from path (may be a directory).
// Returns a map of {slug: {field: [values]}}.
func LoadSandbox(path string) (map[string]map[string][]float64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return map[string]map[string][]float64{}, nil
	}
	if info.IsDir() {
		path = filepath.Join(path, "sandbox.yml")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return map[string]map[string][]float64{}, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("viz: read sandbox %s: %w", path, err)
	}

	var raw struct {
		Pipelines map[string]map[string][]any `yaml:"pipelines"`
	}
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("viz: parse sandbox %s: %w", path, err)
	}

	result := make(map[string]map[string][]float64, len(raw.Pipelines))
	for slug, fields := range raw.Pipelines {
		fm := make(map[string][]float64, len(fields))
		for field, vals := range fields {
			floats := make([]float64, 0, len(vals))
			for _, v := range vals {
				floats = append(floats, toFloat64(v))
			}
			fm[field] = floats
		}
		result[slug] = fm
	}
	return result, nil
}

// parseDuration converts "30s", "5m", "2h" (or bare int string) to seconds.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "s") {
		n, _ := strconv.Atoi(s[:len(s)-1])
		return n
	}
	if strings.HasSuffix(s, "m") {
		n, _ := strconv.Atoi(s[:len(s)-1])
		return n * 60
	}
	if strings.HasSuffix(s, "h") {
		n, _ := strconv.Atoi(s[:len(s)-1])
		return n * 3600
	}
	n, _ := strconv.Atoi(s)
	return n
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func intVal(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return def
}

func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

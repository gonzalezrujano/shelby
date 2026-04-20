package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"shelby/internal/store"
)

// Read API shared by the built-in dashboard and any external viz client
// (e.g. the future shelby-viz). All endpoints are GET and JSON.

type runSummary struct {
	RunID      string    `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Duration   string    `json:"duration"`
	Status     string    `json:"status"`
	StepsOK    int       `json:"steps_ok"`
	StepsTotal int       `json:"steps_total"`
	Error      string    `json:"error,omitempty"`
}

func summarize(r store.RunRecord) runSummary {
	ok := 0
	for _, s := range r.Steps {
		if s.OK {
			ok++
		}
	}
	return runSummary{
		RunID:      r.RunID,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		Duration:   r.Duration.String(),
		Status:     r.Status,
		StepsOK:    ok,
		StepsTotal: len(r.Steps),
		Error:      r.Error,
	}
}

// handleAPIRunsList: GET /api/pipelines/:slug/runs?limit=N
func (s *Server) handleAPIRunsList(w http.ResponseWriter, r *http.Request, slug string) {
	reg, err := s.Store.Get(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	limit := intParam(r, "limit", 20, 1, 500)
	runs, err := s.Store.Runs(reg.Slug, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]runSummary, 0, len(runs))
	for _, rr := range runs {
		out = append(out, summarize(rr))
	}
	writeJSON(w, map[string]any{
		"pipeline": reg.Name,
		"slug":     reg.Slug,
		"runs":     out,
	})
}

// handleAPIRunDetail: GET /api/pipelines/:slug/runs/:run_id
func (s *Server) handleAPIRunDetail(w http.ResponseWriter, r *http.Request, slug, runID string) {
	reg, err := s.Store.Get(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	rec, err := s.Store.Run(reg.Slug, runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, rec)
}

// seriesPoint is one datum in the widget contract timeseries payload.
type seriesPoint struct {
	T     time.Time `json:"t"`
	V     float64   `json:"v"`
	RunID string    `json:"run_id,omitempty"`
}

type seriesPayload struct {
	Kind   string         `json:"kind"`
	Unit   string         `json:"unit,omitempty"`
	Points []seriesPoint  `json:"points"`
	Meta   map[string]any `json:"meta"`
}

// handleAPISeries: GET /api/pipelines/:slug/series?field=<dotted>&limit=N&unit=<u>
//
// v0: kind always "timeseries", agg=none, one field per request. Skips runs
// where the field is missing or non-numeric.
func (s *Server) handleAPISeries(w http.ResponseWriter, r *http.Request, slug string) {
	reg, err := s.Store.Get(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	field := r.URL.Query().Get("field")
	if field == "" {
		http.Error(w, "missing required query param: field", http.StatusBadRequest)
		return
	}
	limit := intParam(r, "limit", 50, 1, 1000)
	unit := r.URL.Query().Get("unit")

	runs, err := s.Store.Runs(reg.Slug, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	points := make([]seriesPoint, 0, len(runs))
	for _, rr := range runs {
		v, ok := store.LookupPath(rr.Output, field)
		if !ok {
			continue
		}
		f, ok := toFloat(v)
		if !ok {
			continue
		}
		points = append(points, seriesPoint{T: rr.StartedAt, V: f, RunID: rr.RunID})
	}
	// Oldest→newest. store.Runs is filename-sorted, which collapses sub-second
	// ordering for runs in the same second, so sort by real timestamp.
	sort.Slice(points, func(i, j int) bool { return points[i].T.Before(points[j].T) })

	writeJSON(w, seriesPayload{
		Kind:   "timeseries",
		Unit:   unit,
		Points: points,
		Meta: map[string]any{
			"pipeline":     reg.Name,
			"slug":         reg.Slug,
			"field":        field,
			"limit":        limit,
			"generated_at": time.Now().UTC(),
		},
	})
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func intParam(r *http.Request, key string, def, lo, hi int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Printf("api: encode: %v\n", err)
	}
}

// withCORS permits cross-origin reads from the viz client. Safe because the
// API is read-only GETs — mutating routes (run) stay on their own handler
// without this wrapper.
func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

// routeAPIPipeline dispatches /api/pipelines/<slug>/<tail...>.
// Returns true if handled, false if the caller should fall through.
func (s *Server) routeAPIPipeline(w http.ResponseWriter, r *http.Request) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return false
	}
	slug := parts[0]
	switch parts[1] {
	case "runs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		if len(parts) == 2 {
			withCORS(func(w http.ResponseWriter, r *http.Request) {
				s.handleAPIRunsList(w, r, slug)
			})(w, r)
			return true
		}
		if len(parts) == 3 {
			runID := parts[2]
			withCORS(func(w http.ResponseWriter, r *http.Request) {
				s.handleAPIRunDetail(w, r, slug, runID)
			})(w, r)
			return true
		}
		return false
	case "series":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		withCORS(func(w http.ResponseWriter, r *http.Request) {
			s.handleAPISeries(w, r, slug)
		})(w, r)
		return true
	}
	return false
}

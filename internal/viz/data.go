package viz

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DataPoint is a single time-series sample.
// Fields are exported so pongo2 can access them via reflection.
// T, V are accessible in templates as p.t, p.v.
// RunId() method is accessible as p.runid.
type DataPoint struct {
	T     string  `json:"t"`
	V     float64 `json:"v"`
	RunID string  `json:"run_id"`
}

// RunId returns the run ID string — accessible from pongo2 templates as p.runid.
func (d DataPoint) RunId() string { return d.RunID }

// SeriesData wraps a /api/pipelines/:slug/series payload with helper methods.
// All methods are accessible from pongo2 templates via capitalization lookup.
type SeriesData struct {
	Kind string
	Unit string
	Pts  []DataPoint
	Meta map[string]any
}

// Points returns the slice of data points — template access: series.points.
func (s *SeriesData) Points() []DataPoint {
	if s == nil {
		return nil
	}
	return s.Pts
}

// Last returns the most recent value — template access: series.last.
func (s *SeriesData) Last() float64 {
	if s == nil || len(s.Pts) == 0 {
		return 0
	}
	return s.Pts[len(s.Pts)-1].V
}

// Previous returns the second-most-recent value — template access: series.previous.
func (s *SeriesData) Previous() float64 {
	if s == nil || len(s.Pts) < 2 {
		return 0
	}
	return s.Pts[len(s.Pts)-2].V
}

// Trend returns Last - Previous — template access: series.trend.
func (s *SeriesData) Trend() float64 {
	return s.Last() - s.Previous()
}

// Max returns the maximum value in the series — template access: series.max.
func (s *SeriesData) Max() float64 {
	if s == nil || len(s.Pts) == 0 {
		return 0
	}
	m := s.Pts[0].V
	for _, p := range s.Pts[1:] {
		if p.V > m {
			m = p.V
		}
	}
	return m
}

// Min returns the minimum value in the series — template access: series.min.
func (s *SeriesData) Min() float64 {
	if s == nil || len(s.Pts) == 0 {
		return 0
	}
	m := s.Pts[0].V
	for _, p := range s.Pts[1:] {
		if p.V < m {
			m = p.V
		}
	}
	return m
}

// Reversed returns the points slice in reverse chronological order (newest first).
// Template access: series.Reversed (pongo2 capitalizes first letter).
func (s *SeriesData) Reversed() []DataPoint {
	if s == nil || len(s.Pts) == 0 {
		return nil
	}
	out := make([]DataPoint, len(s.Pts))
	for i, j := 0, len(s.Pts)-1; i <= j; i, j = i+1, j-1 {
		out[i], out[j] = s.Pts[j], s.Pts[i]
	}
	return out
}

// Avg returns the average value in the series — template access: series.avg.
func (s *SeriesData) Avg() float64 {
	if s == nil || len(s.Pts) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range s.Pts {
		sum += p.V
	}
	return sum / float64(len(s.Pts))
}

// errSeries returns an empty SeriesData carrying an error message.
func errSeries(unit, msg string) *SeriesData {
	return &SeriesData{Kind: "timeseries", Unit: unit, Pts: nil, Meta: map[string]any{"error": msg}}
}

// PipelineProxy fetches live data for one pipeline slug from the Shelby API.
type PipelineProxy struct {
	Slug    string
	BaseURL string
	client  *http.Client
}

func newProxy(slug, baseURL string) *PipelineProxy {
	return &PipelineProxy{
		Slug:    slug,
		BaseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Series fetches time-series data for the given field.
func (p *PipelineProxy) Series(field string, limit int, unit string) *SeriesData {
	url := fmt.Sprintf("%s/api/pipelines/%s/series", p.BaseURL, p.Slug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return errSeries(unit, err.Error())
	}
	q := req.URL.Query()
	q.Set("field", field)
	q.Set("limit", fmt.Sprint(limit))
	if unit != "" {
		q.Set("unit", unit)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return errSeries(unit, err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var payload struct {
		Kind   string         `json:"kind"`
		Unit   string         `json:"unit"`
		Points []DataPoint    `json:"points"`
		Meta   map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return errSeries(unit, err.Error())
	}
	if payload.Kind == "" {
		payload.Kind = "timeseries"
	}
	if payload.Unit == "" {
		payload.Unit = unit
	}
	return &SeriesData{Kind: payload.Kind, Unit: payload.Unit, Pts: payload.Points, Meta: payload.Meta}
}

// Runs fetches recent run records for the pipeline.
func (p *PipelineProxy) Runs(limit int) map[string]any {
	url := fmt.Sprintf("%s/api/pipelines/%s/runs?limit=%d", p.BaseURL, p.Slug, limit)
	resp, err := p.client.Get(url)
	if err != nil {
		return map[string]any{"runs": []any{}, "error": err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"runs": []any{}, "error": err.Error()}
	}
	return result
}

// PipelinesContext resolves pipeline slugs to PipelineProxy instances.
type PipelinesContext struct {
	BaseURL string
}

// Get returns a PipelineProxy for the given slug.
func (c *PipelinesContext) Get(slug string) *PipelineProxy {
	return newProxy(slug, c.BaseURL)
}

// MockPipelineProxy is backed by static lists of values from sandbox.yml.
type MockPipelineProxy struct {
	Slug   string
	Fields map[string][]float64
}

// Series generates synthetic time-series data from the mock field values.
func (p *MockPipelineProxy) Series(field string, limit int, unit string) *SeriesData {
	vals := p.Fields[field]
	if len(vals) > limit {
		vals = vals[len(vals)-limit:]
	}
	now := time.Now().UTC()
	pts := make([]DataPoint, len(vals))
	for i, v := range vals {
		t := now.Add(-time.Duration(len(vals)-i-1) * time.Minute)
		pts[i] = DataPoint{
			T:     t.Format("2006-01-02T15:04:05Z"),
			V:     v,
			RunID: fmt.Sprintf("mock-%04d", i),
		}
	}
	return &SeriesData{Kind: "timeseries", Unit: unit, Pts: pts, Meta: map[string]any{"mock": true}}
}

// Runs returns an empty mock run list.
func (p *MockPipelineProxy) Runs(_ int) map[string]any {
	return map[string]any{"runs": []any{}, "mock": true}
}

// MockPipelinesContext is a drop-in replacement for PipelinesContext backed by sandbox.yml data.
type MockPipelinesContext struct {
	Data map[string]map[string][]float64 // slug → field → values
}

// Get returns a MockPipelineProxy for the given slug.
func (c *MockPipelinesContext) Get(slug string) *MockPipelineProxy {
	fields := c.Data[slug]
	if fields == nil {
		fields = map[string][]float64{}
	}
	return &MockPipelineProxy{Slug: slug, Fields: fields}
}

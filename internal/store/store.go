package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"shelby/internal/engine"
)

// Store persists pipeline registrations and run history under Root.
// Layout:
//
//	<Root>/pipelines/<slug>.json  -- registration (pointer to YAML path)
//	<Root>/runs/<slug>/<ts>_<runid>.json  -- recorded runs
type Store struct {
	Root string
}

type Registration struct {
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Path         string    `json:"path"`
	RegisteredAt time.Time `json:"registered_at"`
}

type StepRecord struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	OK       bool          `json:"ok"`
	Duration time.Duration `json:"duration_ns"`
	Error    string        `json:"error,omitempty"`
}

type RunRecord struct {
	RunID        string         `json:"run_id"`
	PipelineName string         `json:"pipeline_name"`
	PipelineSlug string         `json:"pipeline_slug"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
	Duration     time.Duration  `json:"duration_ns"`
	Status       string         `json:"status"` // ok | fail
	Error        string         `json:"error,omitempty"`
	Steps        []StepRecord   `json:"steps"`
	Output       map[string]any `json:"output,omitempty"`
}

// New returns a store rooted at $SHELBY_HOME, or ~/.shelby by default.
func New() (*Store, error) {
	root, err := defaultRoot()
	if err != nil {
		return nil, err
	}
	return NewAt(root)
}

func NewAt(root string) (*Store, error) {
	s := &Store{Root: root}
	for _, sub := range []string{"pipelines", "runs"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	return s, nil
}

func defaultRoot() (string, error) {
	if h := os.Getenv("SHELBY_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shelby"), nil
}

// Add registers the pipeline by its Name. Source YAML is NOT copied; Path
// remains a pointer, so edits to the source take effect on next run.
func (s *Store) Add(p *engine.Pipeline, sourcePath string) (Registration, error) {
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return Registration{}, err
	}
	slug := Slugify(p.Name)
	if slug == "" {
		return Registration{}, fmt.Errorf("cannot slugify name %q", p.Name)
	}
	reg := Registration{
		Name:         p.Name,
		Slug:         slug,
		Path:         abs,
		RegisteredAt: time.Now().UTC(),
	}
	return reg, s.writeRegistration(reg)
}

// Update repoints an existing registration at a new YAML file. Slug and
// RegisteredAt are preserved so run history stays attached. The new YAML
// must slugify to the same slug; rename via rm + add.
func (s *Store) Update(nameOrSlug string, p *engine.Pipeline, sourcePath string) (Registration, error) {
	reg, err := s.Get(nameOrSlug)
	if err != nil {
		return Registration{}, err
	}
	newSlug := Slugify(p.Name)
	if newSlug == "" {
		return Registration{}, fmt.Errorf("cannot slugify name %q", p.Name)
	}
	if newSlug != reg.Slug {
		return Registration{}, fmt.Errorf("name %q would re-slug %q -> %q; use rm + add to rename", p.Name, reg.Slug, newSlug)
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return Registration{}, err
	}
	reg.Name = p.Name
	reg.Path = abs
	return reg, s.writeRegistration(reg)
}

func (s *Store) writeRegistration(reg Registration) error {
	path := filepath.Join(s.Root, "pipelines", reg.Slug+".json")
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Get resolves name (or slug) to a Registration.
func (s *Store) Get(nameOrSlug string) (Registration, error) {
	slug := Slugify(nameOrSlug)
	path := filepath.Join(s.Root, "pipelines", slug+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Registration{}, fmt.Errorf("pipeline %q not registered", nameOrSlug)
		}
		return Registration{}, err
	}
	var reg Registration
	if err := json.Unmarshal(b, &reg); err != nil {
		return Registration{}, fmt.Errorf("corrupt registration %s: %w", path, err)
	}
	return reg, nil
}

func (s *Store) List() ([]Registration, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "pipelines"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var regs []Registration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Root, "pipelines", e.Name()))
		if err != nil {
			continue
		}
		var reg Registration
		if err := json.Unmarshal(b, &reg); err != nil {
			continue
		}
		regs = append(regs, reg)
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].Slug < regs[j].Slug })
	return regs, nil
}

func (s *Store) Remove(nameOrSlug string) error {
	slug := Slugify(nameOrSlug)
	path := filepath.Join(s.Root, "pipelines", slug+".json")
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pipeline %q not registered", nameOrSlug)
		}
		return err
	}
	// best-effort runs cleanup
	_ = os.RemoveAll(filepath.Join(s.Root, "runs", slug))
	return nil
}

// RecordRun appends a run history entry under runs/<slug>/.
func (s *Store) RecordRun(slug string, rec RunRecord) error {
	dir := filepath.Join(s.Root, "runs", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ts := rec.StartedAt.UTC().Format("20060102T150405Z")
	fname := fmt.Sprintf("%s_%s.json", ts, rec.RunID)
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fname), b, 0o644)
}

// Runs returns the most recent run records (newest first), up to limit.
// limit <= 0 returns all.
func (s *Store) Runs(slug string, limit int) ([]RunRecord, error) {
	dir := filepath.Join(s.Root, "runs", slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // lexicographic = chronological with our ts prefix
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	runs := make([]RunRecord, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		runs = append(runs, r)
	}
	return runs, nil
}

func (s *Store) LastRun(slug string) (*RunRecord, error) {
	runs, err := s.Runs(slug, 1)
	if err != nil || len(runs) == 0 {
		return nil, err
	}
	return &runs[0], nil
}

// Slugify produces a filesystem-safe identifier: lowercase, alnum+dash only,
// no leading/trailing dashes.
func Slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := true
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// BuildRunRecord turns an engine RunContext + error into a storable record.
func BuildRunRecord(runID string, p *engine.Pipeline, rc *engine.RunContext, runErr error, started, finished time.Time, output map[string]any) RunRecord {
	rec := RunRecord{
		RunID:        runID,
		PipelineName: p.Name,
		PipelineSlug: Slugify(p.Name),
		StartedAt:    started,
		FinishedAt:   finished,
		Duration:     finished.Sub(started),
		Status:       "ok",
		Output:       output,
	}
	if runErr != nil {
		rec.Status = "fail"
		rec.Error = runErr.Error()
	}
	for _, s := range p.Steps {
		out := rc.Steps[s.ID]
		rec.Steps = append(rec.Steps, StepRecord{
			ID:       s.ID,
			Type:     string(s.Type),
			OK:       out.OK,
			Duration: out.Duration,
			Error:    out.Error,
		})
	}
	return rec
}

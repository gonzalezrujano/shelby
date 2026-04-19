package store

import (
	"testing"
	"time"

	"shelby/internal/engine"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Monitor Salud Alpha": "monitor-salud-alpha",
		"  MSA__v2 ":          "msa-v2",
		"foo/bar baz":         "foo-bar-baz",
		"UPPER":               "upper",
		"a--b":                "a-b",
		"---":                 "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestAddGetListRemove(t *testing.T) {
	s := newStore(t)
	p := &engine.Pipeline{Name: "Monitor Salud Alpha"}
	reg, err := s.Add(p, "/tmp/x.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Slug != "monitor-salud-alpha" || reg.Name != "Monitor Salud Alpha" {
		t.Fatalf("reg: %+v", reg)
	}

	got, err := s.Get("Monitor Salud Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != reg.Slug {
		t.Fatalf("get: %+v", got)
	}

	// also by slug
	if _, err := s.Get("monitor-salud-alpha"); err != nil {
		t.Fatal(err)
	}

	regs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 {
		t.Fatalf("list: %v", regs)
	}

	if err := s.Remove("monitor-salud-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("monitor-salud-alpha"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestGetNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRecordAndListRuns(t *testing.T) {
	s := newStore(t)
	p := &engine.Pipeline{Name: "X"}
	rc := &engine.RunContext{
		Steps: map[string]engine.Output{
			"a": {StepID: "a", OK: true, Duration: 5 * time.Millisecond},
			"b": {StepID: "b", OK: false, Duration: 1 * time.Millisecond, Error: "boom"},
		},
	}
	p.Steps = []engine.Step{{ID: "a", Type: "http_get"}, {ID: "b", Type: "shell"}}
	start := time.Now().Add(-time.Second)
	rec := BuildRunRecord("r1", p, rc, nil, start, time.Now(), map[string]any{"v": 1})
	if err := s.RecordRun("x", rec); err != nil {
		t.Fatal(err)
	}
	// record another a moment later
	time.Sleep(10 * time.Millisecond)
	rec2 := BuildRunRecord("r2", p, rc, nil, time.Now().Add(-time.Second), time.Now(), nil)
	if err := s.RecordRun("x", rec2); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs("x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs: %v", runs)
	}
	// newest first
	if runs[0].RunID != "r2" {
		t.Fatalf("order: %v", runs)
	}
	if len(runs[0].Steps) != 2 || runs[0].Steps[0].ID != "a" {
		t.Fatalf("steps: %v", runs[0].Steps)
	}
	last, err := s.LastRun("x")
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.RunID != "r2" {
		t.Fatalf("last: %v", last)
	}
}

func TestRemoveAlsoDropsRunHistory(t *testing.T) {
	s := newStore(t)
	p := &engine.Pipeline{Name: "Z"}
	reg, _ := s.Add(p, "/tmp/z.yaml")
	rec := BuildRunRecord("r1", p, &engine.RunContext{Steps: map[string]engine.Output{}}, nil,
		time.Now().Add(-time.Second), time.Now(), nil)
	_ = s.RecordRun(reg.Slug, rec)
	if runs, _ := s.Runs(reg.Slug, 0); len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	_ = s.Remove(reg.Slug)
	if runs, _ := s.Runs(reg.Slug, 0); len(runs) != 0 {
		t.Fatalf("expected runs cleaned, got %d", len(runs))
	}
}

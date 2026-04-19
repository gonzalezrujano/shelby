package server

import (
	"context"
	"log"
	"sync"
	"time"

	"shelby/internal/config"
	"shelby/internal/engine"
	"shelby/internal/runner"
	"shelby/internal/store"
)

// Scheduler runs one goroutine per registered pipeline, invoking
// runner.Execute at the pipeline's Interval. Overlapping runs are skipped
// (TryLock) rather than queued.
type Scheduler struct {
	Store *store.Store

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-slug lock to prevent overlap
	wg    sync.WaitGroup
}

func NewScheduler(st *store.Store) *Scheduler {
	return &Scheduler{Store: st, locks: map[string]*sync.Mutex{}}
}

// Start launches per-pipeline goroutines. Returns immediately.
// Call Wait() to block until ctx cancellation drains them.
func (s *Scheduler) Start(ctx context.Context) error {
	regs, err := s.Store.List()
	if err != nil {
		return err
	}
	for _, reg := range regs {
		s.wg.Add(1)
		go s.loop(ctx, reg)
	}
	log.Printf("scheduler: %d pipelines started", len(regs))
	return nil
}

func (s *Scheduler) Wait() { s.wg.Wait() }

// RunOnce triggers a pipeline manually, respecting the overlap lock.
// Blocks until done (or skipped).
func (s *Scheduler) RunOnce(ctx context.Context, slug string) (runner.Result, bool, error) {
	reg, err := s.Store.Get(slug)
	if err != nil {
		return runner.Result{}, false, err
	}
	p, err := config.Load(reg.Path)
	if err != nil {
		return runner.Result{}, false, err
	}
	lock := s.lockFor(reg.Slug)
	if !lock.TryLock() {
		return runner.Result{}, false, nil // skipped
	}
	defer lock.Unlock()
	res := runner.Execute(ctx, p, s.Store, reg.Slug)
	return res, true, nil
}

func (s *Scheduler) lockFor(slug string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.locks[slug]
	if !ok {
		m = &sync.Mutex{}
		s.locks[slug] = m
	}
	return m
}

func (s *Scheduler) loop(ctx context.Context, reg store.Registration) {
	defer s.wg.Done()
	p, err := config.Load(reg.Path)
	if err != nil {
		log.Printf("[%s] load failed: %v", reg.Slug, err)
		return
	}
	if p.Interval <= 0 {
		log.Printf("[%s] no interval, not scheduling", reg.Slug)
		return
	}

	// kick off an initial run immediately
	s.tickOnce(ctx, reg, p)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] stopping", reg.Slug)
			return
		case <-t.C:
			// reload YAML so edits to the registered file take effect
			if latest, err := config.Load(reg.Path); err == nil {
				p = latest
			} else {
				log.Printf("[%s] reload: %v (using cached)", reg.Slug, err)
			}
			s.tickOnce(ctx, reg, p)
		}
	}
}

func (s *Scheduler) tickOnce(ctx context.Context, reg store.Registration, p *engine.Pipeline) {
	lock := s.lockFor(reg.Slug)
	if !lock.TryLock() {
		log.Printf("[%s] skip tick: previous still running", reg.Slug)
		return
	}
	defer lock.Unlock()
	start := time.Now()
	res := runner.Execute(ctx, p, s.Store, reg.Slug)
	status := "ok"
	if res.Err != nil {
		status = "fail"
	}
	log.Printf("[%s] %s run=%s dur=%s", reg.Slug, status, res.RunID, time.Since(start))
}

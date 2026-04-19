package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"shelby/internal/collectors"
	"shelby/internal/engine"
	"shelby/internal/executors"
	"shelby/internal/store"
)

// NewEngine returns an engine with all built-in executors registered.
func NewEngine() *engine.Engine {
	eng := engine.New()
	eng.Register(engine.StepHTTPGet, collectors.HTTPGet{})
	eng.Register(engine.StepSysStat, collectors.SysStat{})
	eng.Register(engine.StepShell, collectors.Shell{})
	eng.Register(engine.StepScript, executors.Script{})
	eng.Register(engine.StepConditional, executors.Conditional{})
	eng.Register(engine.StepAggregator, executors.Aggregator{})
	return eng
}

func NewRunID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "r_" + hex.EncodeToString(b[:])
}

type Result struct {
	RunID    string
	Started  time.Time
	Finished time.Time
	RC       *engine.RunContext
	Err      error
	Output   map[string]any
}

// Execute runs a pipeline end-to-end. If recordStore and slug are both set,
// the run is persisted to history.
func Execute(ctx context.Context, p *engine.Pipeline, recordStore *store.Store, slug string) Result {
	eng := NewEngine()
	runID := NewRunID()
	started := time.Now()
	rc, err := eng.Run(ctx, p, runID)
	finished := time.Now()
	final, finalErr := engine.FinalOutput(p, rc)
	if finalErr != nil && err == nil {
		err = finalErr
	}
	res := Result{
		RunID: runID, Started: started, Finished: finished,
		RC: rc, Err: err, Output: final,
	}
	if recordStore != nil && slug != "" {
		rec := store.BuildRunRecord(runID, p, rc, err, started, finished, final)
		_ = recordStore.RecordRun(slug, rec)
	}
	return res
}

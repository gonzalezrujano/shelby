package engine

import (
	"context"
	"fmt"
	"time"
)

type Engine struct {
	executors map[StepType]Executor
}

func New() *Engine {
	return &Engine{executors: map[StepType]Executor{}}
}

func (e *Engine) Register(t StepType, ex Executor) {
	e.executors[t] = ex
}

func (e *Engine) Run(ctx context.Context, p *Pipeline, runID string) (*RunContext, error) {
	rc := &RunContext{Pipeline: p, Steps: map[string]Output{}, RunID: runID, Engine: e}
	for _, s := range p.Steps {
		out, err := e.RunStep(ctx, s, rc)
		rc.Steps[s.ID] = out
		if err != nil {
			return rc, fmt.Errorf("step %s: %w", s.ID, err)
		}
	}
	return rc, nil
}

// RunStep executes a single step and applies ref resolution to its Input.
// Exported so executors (e.g. Conditional) can dispatch nested Then/Else steps.
func (e *Engine) RunStep(ctx context.Context, s Step, rc *RunContext) (Output, error) {
	ex, ok := e.executors[s.Type]
	if !ok {
		return Output{StepID: s.ID, OK: false, Error: "unknown step type"}, fmt.Errorf("no executor for %s", s.Type)
	}
	// resolve ${refs} in Input against prior step outputs
	if s.Input != nil {
		r, err := Resolve(s.Input, rc)
		if err != nil {
			return Output{StepID: s.ID, OK: false, Error: err.Error()}, err
		}
		if m, ok := r.(map[string]any); ok {
			s.Input = m
		}
	}
	start := time.Now()
	sctx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		sctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	out, err := ex.Execute(sctx, s, rc)
	out.StepID = s.ID
	out.StartedAt = start
	out.Duration = time.Since(start)
	return out, err
}

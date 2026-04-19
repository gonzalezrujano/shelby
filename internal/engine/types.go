package engine

import (
	"context"
	"time"
)

type StepType string

const (
	StepHTTPGet     StepType = "http_get"
	StepSysStat     StepType = "sys_stat"
	StepLogRead     StepType = "log_read"
	StepShell       StepType = "shell"
	StepScript      StepType = "script"
	StepConditional StepType = "conditional"
	StepAggregator  StepType = "aggregator"
)

type Pipeline struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Interval    time.Duration     `yaml:"interval"`
	Steps       []Step            `yaml:"steps"`
	Output      map[string]string `yaml:"output,omitempty"`
	Export      ExportConfig      `yaml:"export,omitempty"`
}

type Step struct {
	ID      string   `yaml:"id"`
	Type    StepType `yaml:"type"`
	Source  string   `yaml:"source,omitempty"`
	Extract string   `yaml:"extract,omitempty"`

	// http
	Headers map[string]string `yaml:"headers,omitempty"`

	// shell
	Command string       `yaml:"command,omitempty"`
	Parse   *ParseConfig `yaml:"parse,omitempty"`

	// script
	Runtime string         `yaml:"runtime,omitempty"`
	File    string         `yaml:"file,omitempty"`
	Input   map[string]any `yaml:"input,omitempty"`
	EnvKeys []string       `yaml:"env_keys,omitempty"` // os env vars to forward to subprocess

	When string `yaml:"when,omitempty"`
	Then []Step `yaml:"then,omitempty"`
	Else []Step `yaml:"else,omitempty"`

	Op    string   `yaml:"op,omitempty"`
	Over  []string `yaml:"over,omitempty"`
	Field string   `yaml:"field,omitempty"` // aggregator: extract key from array-of-maps

	Timeout time.Duration `yaml:"timeout,omitempty"`
}

// ParseConfig selects a parser applied to a shell step's stdout.
// Engines:
//   - textfsm: Template path required
//   - json:    Path optional (dot-path into parsed JSON; "" = root)
//   - lines:   Skip optional (drop first N lines)
//   - regex:   Pattern required (must use named capture groups)
//
// Types optionally coerces named record fields after parsing
// (useful for textfsm/regex whose captures are always strings).
// Supported values: "int", "float", "bool".
type ParseConfig struct {
	Engine   string            `yaml:"engine"`
	Template string            `yaml:"template,omitempty"`
	Pattern  string            `yaml:"pattern,omitempty"`
	Path     string            `yaml:"path,omitempty"`
	Skip     int               `yaml:"skip,omitempty"`
	Types    map[string]string `yaml:"types,omitempty"`
}

type Output struct {
	StepID    string         `json:"step_id"`
	OK        bool           `json:"ok"`
	Data      map[string]any `json:"data"`
	Error     string         `json:"error,omitempty"`
	Duration  time.Duration  `json:"duration"`
	StartedAt time.Time      `json:"started_at"`
}

type RunContext struct {
	Pipeline *Pipeline
	Steps    map[string]Output
	RunID    string
	// Engine is set by Engine.Run so executors can dispatch nested steps
	// (e.g. Conditional running Then/Else branches).
	Engine *Engine
}

type Executor interface {
	Execute(ctx context.Context, step Step, rc *RunContext) (Output, error)
}

type ExportConfig struct {
	LocalPort     int    `yaml:"local_port,omitempty"`
	CloudEndpoint string `yaml:"cloud_endpoint,omitempty"`
}

type ScriptRequest struct {
	StepID   string                 `json:"step_id"`
	RunID    string                 `json:"run_id"`
	Pipeline string                 `json:"pipeline"`
	Input    map[string]any         `json:"input"`
	Context  ScriptRequestContext   `json:"context"`
	Env      map[string]string      `json:"env,omitempty"`
}

type ScriptRequestContext struct {
	Steps map[string]Output `json:"steps"`
}

type ScriptResponse struct {
	OK      bool           `json:"ok"`
	Data    map[string]any `json:"data"`
	Error   string         `json:"error,omitempty"`
	Metrics map[string]any `json:"metrics,omitempty"`
}

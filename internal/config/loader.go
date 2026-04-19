package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"shelby/internal/engine"
)

type rawPipeline struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description,omitempty"`
	Interval    string              `yaml:"interval"`
	Steps       []rawStep           `yaml:"steps"`
	Output      map[string]string   `yaml:"output,omitempty"`
	Export      engine.ExportConfig `yaml:"export,omitempty"`
}

type rawStep struct {
	ID      string              `yaml:"id"`
	Type    string              `yaml:"type"`
	Source  string              `yaml:"source,omitempty"`
	Extract string              `yaml:"extract,omitempty"`
	Headers map[string]string   `yaml:"headers,omitempty"`
	Command string              `yaml:"command,omitempty"`
	Parse   *engine.ParseConfig `yaml:"parse,omitempty"`
	Runtime string              `yaml:"runtime,omitempty"`
	File    string              `yaml:"file,omitempty"`
	Input   map[string]any      `yaml:"input,omitempty"`
	EnvKeys []string            `yaml:"env_keys,omitempty"`
	When    string              `yaml:"when,omitempty"`
	Then    []rawStep           `yaml:"then,omitempty"`
	Else    []rawStep           `yaml:"else,omitempty"`
	Op      string              `yaml:"op,omitempty"`
	Over    []string            `yaml:"over,omitempty"`
	Field   string              `yaml:"field,omitempty"`
	Timeout string              `yaml:"timeout,omitempty"`
}

func Load(path string) (*engine.Pipeline, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return parse(b, filepath.Dir(abs))
}

// Parse parses YAML without a base dir (script File paths stay as-is).
func Parse(b []byte) (*engine.Pipeline, error) { return parse(b, "") }

func parse(b []byte, baseDir string) (*engine.Pipeline, error) {
	var r rawPipeline
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if r.Name == "" {
		return nil, fmt.Errorf("pipeline name required")
	}
	interval, err := parseDur(r.Interval)
	if err != nil {
		return nil, fmt.Errorf("interval: %w", err)
	}
	steps, err := convertSteps(r.Steps, baseDir)
	if err != nil {
		return nil, err
	}
	return &engine.Pipeline{
		Name:        r.Name,
		Description: r.Description,
		Interval:    interval,
		Steps:       steps,
		Output:      r.Output,
		Export:      r.Export,
	}, nil
}

func parseDur(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

func convertSteps(rs []rawStep, baseDir string) ([]engine.Step, error) {
	out := make([]engine.Step, len(rs))
	for i, r := range rs {
		if r.ID == "" {
			return nil, fmt.Errorf("step[%d]: id required", i)
		}
		if r.Type == "" {
			return nil, fmt.Errorf("step %s: type required", r.ID)
		}
		timeout, err := parseDur(r.Timeout)
		if err != nil {
			return nil, fmt.Errorf("step %s timeout: %w", r.ID, err)
		}
		then, err := convertSteps(r.Then, baseDir)
		if err != nil {
			return nil, err
		}
		els, err := convertSteps(r.Else, baseDir)
		if err != nil {
			return nil, err
		}
		file := r.File
		if file != "" && baseDir != "" && !filepath.IsAbs(file) {
			file = filepath.Join(baseDir, file)
		}
		parse := r.Parse
		if parse != nil && parse.Template != "" && baseDir != "" && !filepath.IsAbs(parse.Template) {
			p := *parse
			p.Template = filepath.Join(baseDir, p.Template)
			parse = &p
		}
		out[i] = engine.Step{
			ID:      r.ID,
			Type:    engine.StepType(r.Type),
			Source:  r.Source,
			Extract: r.Extract,
			Headers: r.Headers,
			Command: r.Command,
			Parse:   parse,
			Runtime: r.Runtime,
			File:    file,
			Input:   r.Input,
			EnvKeys: r.EnvKeys,
			When:    r.When,
			Then:    then,
			Else:    els,
			Op:      r.Op,
			Over:    r.Over,
			Field:   r.Field,
			Timeout: timeout,
		}
	}
	return out, nil
}

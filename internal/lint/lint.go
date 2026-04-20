// Package lint provides static analysis for pipeline YAML files.
// Validate returns hard errors (semantic/structural); Lint returns soft warnings.
package lint

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"shelby/internal/engine"
)

type Severity int

const (
	Error Severity = iota
	Warning
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

type Issue struct {
	Severity Severity
	Where    string // step id, "pipeline", or "output.<key>"
	Msg      string
}

func (i Issue) String() string {
	return fmt.Sprintf("[%s] %s: %s", i.Severity, i.Where, i.Msg)
}

var (
	validTypes = map[engine.StepType]bool{
		engine.StepHTTPGet:     true,
		engine.StepSysStat:     true,
		engine.StepLogRead:     true,
		engine.StepShell:       true,
		engine.StepScript:      true,
		engine.StepConditional: true,
		engine.StepAggregator:  true,
	}
	validParseEngines = map[string]bool{
		"textfsm": true,
		"json":    true,
		"lines":   true,
		"regex":   true,
	}
	validSysSources = map[string]bool{
		"/":      true,
		"memory": true,
		"cpu":    true,
	}
	refRe   = regexp.MustCompile(`\$\{([^}]+)\}`)
	idStyle = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Validate returns hard errors for a loaded pipeline. File paths in the pipeline
// are expected to be absolute (config.Load already rewrites them).
func Validate(p *engine.Pipeline) []Issue {
	var out []Issue
	if p.Name == "" {
		out = append(out, Issue{Error, "pipeline", "name required"})
	}
	if p.Interval <= 0 {
		out = append(out, Issue{Error, "pipeline", "interval required (e.g. 60s)"})
	}
	if len(p.Steps) == 0 {
		out = append(out, Issue{Error, "pipeline", "at least one step required"})
	}

	out = append(out, duplicatesPass(p.Steps)...)
	ids := collectIDs(p.Steps)
	out = append(out, checkSteps(p.Steps, ids)...)
	out = append(out, checkRefs(p, ids)...)
	return filterSeverity(out, Error)
}

func filterSeverity(in []Issue, s Severity) []Issue {
	out := in[:0]
	for _, i := range in {
		if i.Severity == s {
			out = append(out, i)
		}
	}
	return out
}

// Lint returns soft warnings (style, best practices).
func Lint(p *engine.Pipeline) []Issue {
	var out []Issue
	if p.Description == "" {
		out = append(out, Issue{Warning, "pipeline", "description missing"})
	}
	if p.Interval > 0 && p.Interval.Seconds() < 5 {
		out = append(out, Issue{Warning, "pipeline", "interval < 5s may cause thrash"})
	}

	refs := collectRefTargets(p)
	seen := map[string]bool{}
	lintSteps(p.Steps, &out, refs, seen)

	ids := collectIDs(p.Steps)
	out = append(out, filterSeverity(checkSteps(p.Steps, ids), Warning)...)
	return out
}

func lintSteps(steps []engine.Step, out *[]Issue, refs map[string]bool, seen map[string]bool) {
	for _, s := range steps {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true

		if !idStyle.MatchString(s.ID) {
			*out = append(*out, Issue{Warning, s.ID, "id should be snake_case"})
		}
		needsTimeout := s.Type == engine.StepHTTPGet || s.Type == engine.StepShell || s.Type == engine.StepScript
		if needsTimeout && s.Timeout == 0 {
			*out = append(*out, Issue{Warning, s.ID, "timeout not set"})
		}
		if dup := firstDup(s.EnvKeys); dup != "" {
			*out = append(*out, Issue{Warning, s.ID, "duplicate env_keys: " + dup})
		}
		if s.Type != engine.StepConditional && s.Type != engine.StepAggregator {
			if !refs[s.ID] {
				*out = append(*out, Issue{Warning, s.ID, "step output never referenced"})
			}
		}
		lintSteps(s.Then, out, refs, seen)
		lintSteps(s.Else, out, refs, seen)
	}
}

func checkSteps(steps []engine.Step, ids map[string]bool) []Issue {
	var out []Issue
	for _, s := range steps {
		if s.ID == "" {
			out = append(out, Issue{Error, "pipeline", "step with empty id"})
			continue
		}
		if !validTypes[s.Type] {
			out = append(out, Issue{Error, s.ID, fmt.Sprintf("invalid type %q", s.Type)})
			continue
		}
		switch s.Type {
		case engine.StepHTTPGet:
			if s.Source == "" {
				out = append(out, Issue{Error, s.ID, "http_get: source required"})
			}
		case engine.StepSysStat:
			if s.Source == "" {
				out = append(out, Issue{Error, s.ID, "sys_stat: source required (/, memory, cpu)"})
			} else if !validSysSources[s.Source] {
				out = append(out, Issue{Warning, s.ID, fmt.Sprintf("sys_stat: unusual source %q", s.Source)})
			}
		case engine.StepLogRead:
			if s.Source == "" {
				out = append(out, Issue{Error, s.ID, "log_read: source required"})
			}
		case engine.StepShell:
			if s.Command == "" {
				out = append(out, Issue{Error, s.ID, "shell: command required"})
			}
			if s.Parse != nil {
				out = append(out, checkParse(s.ID, s.Parse)...)
			}
		case engine.StepScript:
			if s.Runtime == "" {
				out = append(out, Issue{Error, s.ID, "script: runtime required"})
			}
			if s.File == "" {
				out = append(out, Issue{Error, s.ID, "script: file required"})
			} else if _, err := os.Stat(s.File); err != nil {
				out = append(out, Issue{Error, s.ID, fmt.Sprintf("script file not found: %s", s.File)})
			}
		case engine.StepConditional:
			if s.When == "" {
				out = append(out, Issue{Error, s.ID, "conditional: when required"})
			}
			if len(s.Then) == 0 && len(s.Else) == 0 {
				out = append(out, Issue{Warning, s.ID, "conditional: then and else both empty"})
			}
			out = append(out, checkSteps(s.Then, ids)...)
			out = append(out, checkSteps(s.Else, ids)...)
		case engine.StepAggregator:
			if s.Op == "" {
				out = append(out, Issue{Error, s.ID, "aggregator: op required"})
			}
			if len(s.Over) == 0 {
				out = append(out, Issue{Error, s.ID, "aggregator: over required"})
			}
			for _, ref := range s.Over {
				if _, ok := ids[ref]; !ok {
					out = append(out, Issue{Error, s.ID, fmt.Sprintf("aggregator: over references unknown step %q", ref)})
				}
			}
		}
	}
	return out
}

func checkParse(stepID string, p *engine.ParseConfig) []Issue {
	var out []Issue
	if p.Engine == "" {
		out = append(out, Issue{Error, stepID, "parse: engine required"})
		return out
	}
	if !validParseEngines[p.Engine] {
		out = append(out, Issue{Error, stepID, fmt.Sprintf("parse: invalid engine %q", p.Engine)})
		return out
	}
	switch p.Engine {
	case "textfsm":
		if p.Template == "" {
			out = append(out, Issue{Error, stepID, "parse textfsm: template required"})
		} else if _, err := os.Stat(p.Template); err != nil {
			out = append(out, Issue{Error, stepID, fmt.Sprintf("parse template not found: %s", p.Template)})
		}
	case "regex":
		if p.Pattern == "" {
			out = append(out, Issue{Error, stepID, "parse regex: pattern required"})
		}
	}
	return out
}

func checkRefs(p *engine.Pipeline, ids map[string]bool) []Issue {
	var out []Issue
	walkRefs(p.Steps, ids, &out)
	for k, expr := range p.Output {
		for _, ref := range extractStepRefs(expr) {
			if _, ok := ids[ref]; !ok {
				out = append(out, Issue{Error, "output." + k, fmt.Sprintf("unknown step ref %q", ref)})
			}
		}
	}
	return out
}

func walkRefs(steps []engine.Step, ids map[string]bool, out *[]Issue) {
	for _, s := range steps {
		for _, raw := range stringFields(s) {
			for _, ref := range extractStepRefs(raw) {
				if _, ok := ids[ref]; !ok {
					*out = append(*out, Issue{Error, s.ID, fmt.Sprintf("unknown step ref %q", ref)})
				}
			}
		}
		walkRefs(s.Then, ids, out)
		walkRefs(s.Else, ids, out)
	}
}

func stringFields(s engine.Step) []string {
	fields := []string{s.Source, s.Extract, s.Command, s.When, s.Field}
	for _, v := range s.Headers {
		fields = append(fields, v)
	}
	collectStrings(s.Input, &fields)
	return fields
}

func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]any:
		for _, val := range x {
			collectStrings(val, out)
		}
	case []any:
		for _, val := range x {
			collectStrings(val, out)
		}
	}
}

func extractStepRefs(s string) []string {
	var refs []string
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		path := strings.TrimSpace(m[1])
		parts := strings.Split(path, ".")
		if len(parts) >= 4 && parts[0] == "steps" && parts[2] == "output" {
			refs = append(refs, parts[1])
		}
	}
	return refs
}

func collectRefTargets(p *engine.Pipeline) map[string]bool {
	out := map[string]bool{}
	collectRefTargetsSteps(p.Steps, out)
	for _, expr := range p.Output {
		for _, r := range extractStepRefs(expr) {
			out[r] = true
		}
	}
	return out
}

func collectRefTargetsSteps(steps []engine.Step, out map[string]bool) {
	for _, s := range steps {
		for _, f := range stringFields(s) {
			for _, r := range extractStepRefs(f) {
				out[r] = true
			}
		}
		for _, r := range s.Over {
			out[r] = true
		}
		collectRefTargetsSteps(s.Then, out)
		collectRefTargetsSteps(s.Else, out)
	}
}

func collectIDs(steps []engine.Step) map[string]bool {
	out := map[string]bool{}
	var walk func([]engine.Step)
	walk = func(ss []engine.Step) {
		for _, s := range ss {
			if s.ID != "" {
				out[s.ID] = true
			}
			walk(s.Then)
			walk(s.Else)
		}
	}
	walk(steps)
	return out
}

func duplicatesPass(steps []engine.Step) []Issue {
	seen := map[string]bool{}
	reported := map[string]bool{}
	var out []Issue
	var walk func([]engine.Step)
	walk = func(ss []engine.Step) {
		for _, s := range ss {
			if s.ID != "" {
				if seen[s.ID] && !reported[s.ID] {
					out = append(out, Issue{Error, s.ID, "duplicate step id"})
					reported[s.ID] = true
				}
				seen[s.ID] = true
			}
			walk(s.Then)
			walk(s.Else)
		}
	}
	walk(steps)
	return out
}

func firstDup(xs []string) string {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			return x
		}
		seen[x] = true
	}
	return ""
}

// Sort orders issues: errors first, then by where/message for stable output.
func Sort(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Severity != issues[j].Severity {
			return issues[i].Severity < issues[j].Severity
		}
		if issues[i].Where != issues[j].Where {
			return issues[i].Where < issues[j].Where
		}
		return issues[i].Msg < issues[j].Msg
	})
}

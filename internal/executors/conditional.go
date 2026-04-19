package executors

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"shelby/internal/engine"
)

// Conditional evaluates Step.When (expr-lang syntax) against the run context.
// On true it dispatches Then steps; on false, Else. Nested outputs land in
// rc.Steps via the parent Engine.RunStep, so later refs can see them.
//
// When supports both native expr syntax (`steps.disk.output.PCT`) and the
// shelby ${...} wrapper (`${steps.disk.output.PCT} > 80`). The wrapper is
// stripped before compilation so the two styles are interchangeable.
//
// Type note: TextFSM values arrive as strings; use int()/float() builtins
// when comparing numerically, e.g. `int(steps.x.output.count) > 5`.
type Conditional struct{}

var whenRefRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func (Conditional) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	if s.When == "" {
		return engine.Output{OK: false, Error: "conditional: when required"}, errors.New("when required")
	}
	if rc.Engine == nil {
		return engine.Output{OK: false, Error: "conditional: no engine in context"}, errors.New("no engine")
	}

	matched, err := evalBool(s.When, rc)
	if err != nil {
		return engine.Output{OK: false, Error: fmt.Sprintf("when: %v", err)}, err
	}

	branch := s.Else
	if matched {
		branch = s.Then
	}
	executed := make([]string, 0, len(branch))
	for _, ns := range branch {
		out, err := rc.Engine.RunStep(ctx, ns, rc)
		rc.Steps[ns.ID] = out
		executed = append(executed, ns.ID)
		if err != nil {
			data := map[string]any{
				"matched":  matched,
				"executed": executed,
				"failed":   ns.ID,
			}
			return engine.Output{OK: false, Data: data, Error: fmt.Sprintf("branch step %s: %v", ns.ID, err)}, err
		}
	}

	return engine.Output{
		OK: true,
		Data: map[string]any{
			"matched":  matched,
			"executed": executed,
			"branch":   branchName(matched, len(s.Then), len(s.Else)),
		},
	}, nil
}

func evalBool(src string, rc *engine.RunContext) (bool, error) {
	clean := preprocessWhen(src)
	env := map[string]any{"steps": stepsEnv(rc)}
	program, err := expr.Compile(clean, expr.Env(env), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("compile %q: %w", clean, err)
	}
	res, err := vm.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("run %q: %w", clean, err)
	}
	b, ok := res.(bool)
	if !ok {
		return false, fmt.Errorf("expected bool, got %T", res)
	}
	return b, nil
}

// preprocessWhen strips ${...} wrappers and rewrites dot-indexed paths
// (a.0.b) into bracket form (a[0].b) so expr-lang accepts them.
// Transform is only applied inside ${...} to avoid mangling numeric
// literals like 0.5 in the surrounding expression.
func preprocessWhen(src string) string {
	return whenRefRe.ReplaceAllStringFunc(src, func(m string) string {
		inner := whenRefRe.FindStringSubmatch(m)[1]
		return dotIndexToBracket(strings.TrimSpace(inner))
	})
}

func dotIndexToBracket(s string) string {
	parts := strings.Split(s, ".")
	var b strings.Builder
	for i, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			fmt.Fprintf(&b, "[%s]", p)
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(p)
	}
	return b.String()
}

func stepsEnv(rc *engine.RunContext) map[string]any {
	out := make(map[string]any, len(rc.Steps))
	for id, o := range rc.Steps {
		out[id] = map[string]any{
			"ok":     o.OK,
			"output": map[string]any(o.Data),
			"error":  o.Error,
		}
	}
	return out
}

func branchName(matched bool, nThen, nElse int) string {
	if matched {
		if nThen == 0 {
			return "then(empty)"
		}
		return "then"
	}
	if nElse == 0 {
		return "else(empty)"
	}
	return "else"
}

package engine

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var refRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// Resolve replaces ${steps.X.output.Y...} refs in v using rc.
// Accepts string, map[string]any, []any. Single-ref strings preserve type.
func Resolve(v any, rc *RunContext) (any, error) {
	switch x := v.(type) {
	case string:
		return resolveString(x, rc)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			r, err := Resolve(val, rc)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			r, err := Resolve(val, rc)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

func resolveString(s string, rc *RunContext) (any, error) {
	// full-string single ref: keep raw typed value
	if m := refRe.FindStringSubmatch(s); m != nil && m[0] == s {
		return lookup(strings.TrimSpace(m[1]), rc)
	}
	// interpolate each match, stringify
	var firstErr error
	out := refRe.ReplaceAllStringFunc(s, func(match string) string {
		path := strings.TrimSpace(refRe.FindStringSubmatch(match)[1])
		v, err := lookup(path, rc)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return match
		}
		return fmt.Sprintf("%v", v)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func lookup(path string, rc *RunContext) (any, error) {
	parts := strings.Split(path, ".")
	// env.VAR — direct os environment lookup (missing = empty string)
	if len(parts) == 2 && parts[0] == "env" {
		return os.Getenv(parts[1]), nil
	}
	if len(parts) < 4 || parts[0] != "steps" || parts[2] != "output" {
		return nil, fmt.Errorf("invalid ref %q: expect steps.<id>.output.<field> or env.<VAR>", path)
	}
	id := parts[1]
	step, ok := rc.Steps[id]
	if !ok {
		return nil, fmt.Errorf("ref %q: step %s not in context", path, id)
	}
	var cur any = map[string]any(step.Data)
	for _, p := range parts[3:] {
		next, err := descend(cur, p, path)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

func descend(cur any, p, full string) (any, error) {
	switch x := cur.(type) {
	case map[string]any:
		v, ok := x[p]
		if !ok {
			return nil, fmt.Errorf("ref %q: key %s missing", full, p)
		}
		return v, nil
	case []any:
		idx, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("ref %q: %q not numeric index for array", full, p)
		}
		if idx < 0 || idx >= len(x) {
			return nil, fmt.Errorf("ref %q: index %d out of range (len=%d)", full, idx, len(x))
		}
		return x[idx], nil
	case []map[string]any:
		idx, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("ref %q: %q not numeric index for array", full, p)
		}
		if idx < 0 || idx >= len(x) {
			return nil, fmt.Errorf("ref %q: index %d out of range (len=%d)", full, idx, len(x))
		}
		return x[idx], nil
	default:
		return nil, fmt.Errorf("ref %q: cannot descend into %T at %q", full, cur, p)
	}
}

// FinalOutput resolves the pipeline's declared output map against rc.
func FinalOutput(p *Pipeline, rc *RunContext) (map[string]any, error) {
	out := make(map[string]any, len(p.Output))
	for k, expr := range p.Output {
		v, err := resolveString(expr, rc)
		if err != nil {
			return nil, fmt.Errorf("output %s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

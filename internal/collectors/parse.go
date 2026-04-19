package collectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sirikothe/gotextfsm"

	"shelby/internal/engine"
)

// parseOutput dispatches to the requested engine. Always returns a
// list of records ([]any of map[string]any) or an error.
func parseOutput(text string, p *engine.ParseConfig) ([]any, error) {
	var (
		records []any
		err     error
	)
	switch strings.ToLower(p.Engine) {
	case "textfsm":
		records, err = parseTextFSM(text, p.Template)
	case "json":
		records, err = parseJSON(text, p.Path)
	case "lines":
		records, err = parseLines(text, p.Skip), nil
	case "regex":
		records, err = parseRegex(text, p.Pattern)
	default:
		return nil, fmt.Errorf("unknown parse engine %q", p.Engine)
	}
	if err != nil {
		return nil, err
	}
	if len(p.Types) > 0 {
		if err := coerceTypes(records, p.Types); err != nil {
			return nil, err
		}
	}
	return records, nil
}

// coerceTypes mutates each record in place, converting the named fields
// per the types map ("int" | "float" | "bool"). Missing fields are skipped.
func coerceTypes(records []any, types map[string]string) error {
	for i, r := range records {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		for field, kind := range types {
			raw, present := row[field]
			if !present {
				continue
			}
			v, err := coerce(raw, kind)
			if err != nil {
				return fmt.Errorf("record[%d].%s: %w", i, field, err)
			}
			row[field] = v
		}
	}
	return nil
}

func coerce(v any, kind string) (any, error) {
	s := fmt.Sprint(v)
	switch strings.ToLower(kind) {
	case "int":
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("int(%q): %w", s, err)
		}
		return n, nil
	case "float":
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("float(%q): %w", s, err)
		}
		return n, nil
	case "bool":
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("bool(%q): %w", s, err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown type %q (want int|float|bool)", kind)
	}
}

func parseTextFSM(text, templatePath string) ([]any, error) {
	if templatePath == "" {
		return nil, errors.New("textfsm: template required")
	}
	tmpl, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	fsm := gotextfsm.TextFSM{}
	if err := fsm.ParseString(string(tmpl)); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	po := gotextfsm.ParserOutput{}
	if err := po.ParseTextString(text, fsm, true); err != nil {
		return nil, fmt.Errorf("apply template: %w", err)
	}
	out := make([]any, len(po.Dict))
	for i, row := range po.Dict {
		out[i] = map[string]any(row)
	}
	return out, nil
}

// parseJSON unmarshals text, then descends path. If the landed value is an
// array of objects it's returned as records; a single object becomes one row;
// a scalar or array of scalars becomes [{value: X}] rows.
func parseJSON(text, path string) ([]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("json: empty input")
	}
	var root any
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	cur := root
	if path != "" {
		for _, seg := range strings.Split(path, ".") {
			if seg == "" {
				continue
			}
			next, err := jsonDescend(cur, seg)
			if err != nil {
				return nil, err
			}
			cur = next
		}
	}
	switch x := cur.(type) {
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			switch ev := e.(type) {
			case map[string]any:
				out = append(out, ev)
			default:
				out = append(out, map[string]any{"value": ev})
			}
		}
		return out, nil
	case map[string]any:
		return []any{x}, nil
	default:
		return []any{map[string]any{"value": x}}, nil
	}
}

func jsonDescend(cur any, seg string) (any, error) {
	switch x := cur.(type) {
	case map[string]any:
		v, ok := x[seg]
		if !ok {
			return nil, fmt.Errorf("json: key %q missing", seg)
		}
		return v, nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return nil, fmt.Errorf("json: %q not numeric index", seg)
		}
		if idx < 0 || idx >= len(x) {
			return nil, fmt.Errorf("json: index %d out of range (len=%d)", idx, len(x))
		}
		return x[idx], nil
	default:
		return nil, fmt.Errorf("json: cannot descend %T at %q", cur, seg)
	}
}

// parseLines splits on \n, drops blank lines, returns records {line: "..."}.
// skip drops the first N non-blank lines (useful for column headers).
func parseLines(text string, skip int) []any {
	out := make([]any, 0)
	skipped := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if skipped < skip {
			skipped++
			continue
		}
		out = append(out, map[string]any{"line": line})
	}
	return out
}

// parseRegex applies pattern (must have named captures) to each line.
// Non-matching lines are skipped. Each match yields a record keyed by
// capture name.
func parseRegex(text, pattern string) ([]any, error) {
	if pattern == "" {
		return nil, errors.New("regex: pattern required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("regex: %w", err)
	}
	names := re.SubexpNames()
	hasNamed := false
	for _, n := range names {
		if n != "" {
			hasNamed = true
			break
		}
	}
	if !hasNamed {
		return nil, errors.New("regex: pattern must use named captures (?P<name>...)")
	}
	out := make([]any, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		row := make(map[string]any, len(names))
		for i, n := range names {
			if n == "" {
				continue
			}
			row[n] = m[i]
		}
		out = append(out, row)
	}
	return out, nil
}

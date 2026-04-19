package executors

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"shelby/internal/engine"
)

// Aggregator reduces previous step outputs to a single value.
//
// YAML:
//
//	- id: avg_cpu
//	  type: aggregator
//	  op: avg                 # sum | avg | min | max | count
//	  over:
//	    - ${steps.cpu.output.percent}
//	    - ${steps.df.output.records}   # array
//	  field: PCT              # optional: extract key from array-of-maps
//
// Output.Data: {op, value, count}. For op=count, "value" is the element count.
type Aggregator struct{}

func (Aggregator) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	op := strings.ToLower(strings.TrimSpace(s.Op))
	if op == "" {
		err := fmt.Errorf("aggregator: op required")
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	if len(s.Over) == 0 {
		err := fmt.Errorf("aggregator: over required")
		return engine.Output{OK: false, Error: err.Error()}, err
	}

	var nums []float64
	var count int
	for _, ref := range s.Over {
		v, err := engine.Resolve(ref, rc)
		if err != nil {
			return engine.Output{OK: false, Error: err.Error()}, err
		}
		if err := collect(v, s.Field, &nums, &count); err != nil {
			return engine.Output{OK: false, Error: err.Error()}, err
		}
	}

	data := map[string]any{"op": op, "count": count}
	switch op {
	case "count":
		data["value"] = count
	case "sum":
		data["value"] = sum(nums)
	case "avg", "average", "mean":
		if len(nums) == 0 {
			err := fmt.Errorf("aggregator: avg on empty input")
			return engine.Output{OK: false, Error: err.Error()}, err
		}
		data["value"] = sum(nums) / float64(len(nums))
	case "min":
		if len(nums) == 0 {
			err := fmt.Errorf("aggregator: min on empty input")
			return engine.Output{OK: false, Error: err.Error()}, err
		}
		m := math.Inf(1)
		for _, n := range nums {
			if n < m {
				m = n
			}
		}
		data["value"] = m
	case "max":
		if len(nums) == 0 {
			err := fmt.Errorf("aggregator: max on empty input")
			return engine.Output{OK: false, Error: err.Error()}, err
		}
		m := math.Inf(-1)
		for _, n := range nums {
			if n > m {
				m = n
			}
		}
		data["value"] = m
	default:
		err := fmt.Errorf("aggregator: unknown op %q (want sum|avg|min|max|count)", op)
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	return engine.Output{OK: true, Data: data}, nil
}

// collect walks v and appends numeric leaves to nums. field, if non-empty,
// extracts that key from each map element of an array. count tallies every
// non-nil leaf (including ones that fail numeric parse) for op=count.
func collect(v any, field string, nums *[]float64, count *int) error {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		for _, e := range x {
			if err := collect(e, field, nums, count); err != nil {
				return err
			}
		}
	case []map[string]any:
		for _, m := range x {
			if err := collect(map[string]any(m), field, nums, count); err != nil {
				return err
			}
		}
	case map[string]any:
		*count++
		if field == "" {
			return nil // count-only; no numeric contribution
		}
		inner, ok := x[field]
		if !ok {
			return fmt.Errorf("aggregator: field %q missing in map", field)
		}
		if n, ok := toFloat(inner); ok {
			*nums = append(*nums, n)
		}
	default:
		*count++
		if n, ok := toFloat(v); ok {
			*nums = append(*nums, n)
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func sum(ns []float64) float64 {
	var t float64
	for _, n := range ns {
		t += n
	}
	return t
}

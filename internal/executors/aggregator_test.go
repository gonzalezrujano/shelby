package executors

import (
	"context"
	"math"
	"testing"

	"shelby/internal/engine"
)

func rcWith(steps map[string]engine.Output) *engine.RunContext {
	return &engine.RunContext{
		Pipeline: &engine.Pipeline{Name: "t"},
		Steps:    steps,
		RunID:    "r1",
	}
}

func TestAggregatorSumScalars(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"v": 2.0}},
		"b": {OK: true, Data: map[string]any{"v": 3.5}},
	})
	s := engine.Step{ID: "agg", Type: engine.StepAggregator, Op: "sum",
		Over: []string{"${steps.a.output.v}", "${steps.b.output.v}"}}
	out, err := Aggregator{}.Execute(context.Background(), s, rc)
	if err != nil || !out.OK {
		t.Fatalf("unexpected: %v %+v", err, out)
	}
	if v, _ := out.Data["value"].(float64); v != 5.5 {
		t.Fatalf("sum=%v", out.Data["value"])
	}
	if c, _ := out.Data["count"].(int); c != 2 {
		t.Fatalf("count=%v", out.Data["count"])
	}
}

func TestAggregatorAvgArray(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"nums": []any{1.0, 2.0, 3.0, 4.0}}},
	})
	s := engine.Step{Op: "avg", Over: []string{"${steps.a.output.nums}"}}
	out, err := Aggregator{}.Execute(context.Background(), s, rc)
	if err != nil || !out.OK {
		t.Fatalf("unexpected: %v %+v", err, out)
	}
	if v := out.Data["value"].(float64); v != 2.5 {
		t.Fatalf("avg=%v", v)
	}
}

func TestAggregatorMinMax(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"nums": []any{9.0, 1.0, 7.0, 3.0}}},
	})
	sMin, _ := Aggregator{}.Execute(context.Background(), engine.Step{Op: "min", Over: []string{"${steps.a.output.nums}"}}, rc)
	sMax, _ := Aggregator{}.Execute(context.Background(), engine.Step{Op: "max", Over: []string{"${steps.a.output.nums}"}}, rc)
	if sMin.Data["value"].(float64) != 1.0 {
		t.Fatalf("min=%v", sMin.Data["value"])
	}
	if sMax.Data["value"].(float64) != 9.0 {
		t.Fatalf("max=%v", sMax.Data["value"])
	}
}

func TestAggregatorCount(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"rows": []any{"x", "y", "z"}}},
	})
	out, _ := Aggregator{}.Execute(context.Background(), engine.Step{Op: "count", Over: []string{"${steps.a.output.rows}"}}, rc)
	if out.Data["value"].(int) != 3 {
		t.Fatalf("count=%v", out.Data["value"])
	}
}

func TestAggregatorFieldFromRecords(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"df": {OK: true, Data: map[string]any{"records": []any{
			map[string]any{"PCT": "10", "MOUNT": "/"},
			map[string]any{"PCT": "20", "MOUNT": "/data"},
			map[string]any{"PCT": "70", "MOUNT": "/var"},
		}}},
	})
	out, err := Aggregator{}.Execute(context.Background(), engine.Step{
		Op: "max", Over: []string{"${steps.df.output.records}"}, Field: "PCT",
	}, rc)
	if err != nil || !out.OK {
		t.Fatalf("err=%v out=%+v", err, out)
	}
	if v := out.Data["value"].(float64); v != 70 {
		t.Fatalf("max=%v", v)
	}
}

func TestAggregatorStringNumericParse(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"v": "3.14"}},
	})
	out, _ := Aggregator{}.Execute(context.Background(), engine.Step{Op: "sum", Over: []string{"${steps.a.output.v}"}}, rc)
	if math.Abs(out.Data["value"].(float64)-3.14) > 1e-9 {
		t.Fatalf("sum=%v", out.Data["value"])
	}
}

func TestAggregatorEmptyInputErrs(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"nums": []any{}}},
	})
	out, err := Aggregator{}.Execute(context.Background(), engine.Step{Op: "avg", Over: []string{"${steps.a.output.nums}"}}, rc)
	if err == nil || out.OK {
		t.Fatalf("expected empty-input error, got %+v err=%v", out, err)
	}
}

func TestAggregatorUnknownOp(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"v": 1.0}},
	})
	_, err := Aggregator{}.Execute(context.Background(), engine.Step{Op: "stddev", Over: []string{"${steps.a.output.v}"}}, rc)
	if err == nil {
		t.Fatal("expected unknown-op error")
	}
}

func TestAggregatorMissingFieldErrs(t *testing.T) {
	rc := rcWith(map[string]engine.Output{
		"a": {OK: true, Data: map[string]any{"records": []any{
			map[string]any{"MOUNT": "/"},
		}}},
	})
	_, err := Aggregator{}.Execute(context.Background(), engine.Step{
		Op: "sum", Over: []string{"${steps.a.output.records}"}, Field: "PCT",
	}, rc)
	if err == nil {
		t.Fatal("expected missing-field error")
	}
}

func TestAggregatorMissingOp(t *testing.T) {
	_, err := Aggregator{}.Execute(context.Background(), engine.Step{Over: []string{"x"}}, rcWith(nil))
	if err == nil {
		t.Fatal("expected op-required error")
	}
}

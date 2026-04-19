package executors

import (
	"context"
	"reflect"
	"testing"

	"shelby/internal/engine"
)

// stubExecutor records the step IDs it has been asked to execute.
type stubExecutor struct {
	seen *[]string
}

func (s stubExecutor) Execute(ctx context.Context, st engine.Step, rc *engine.RunContext) (engine.Output, error) {
	*s.seen = append(*s.seen, st.ID)
	return engine.Output{OK: true, Data: map[string]any{"id": st.ID}}, nil
}

func buildRC(t *testing.T, priorSteps map[string]map[string]any) (*engine.RunContext, *[]string) {
	t.Helper()
	eng := engine.New()
	var seen []string
	eng.Register("stub", stubExecutor{seen: &seen})
	rc := &engine.RunContext{
		Pipeline: &engine.Pipeline{Name: "t"},
		Steps:    map[string]engine.Output{},
		RunID:    "r",
		Engine:   eng,
	}
	for id, data := range priorSteps {
		rc.Steps[id] = engine.Output{StepID: id, OK: true, Data: data}
	}
	return rc, &seen
}

func thenStep(id string) engine.Step {
	return engine.Step{ID: id, Type: "stub"}
}

func TestConditionalThenBranch(t *testing.T) {
	rc, seen := buildRC(t, map[string]map[string]any{
		"disk": {"PCT": 90.0},
	})
	step := engine.Step{
		ID:   "alert",
		Type: engine.StepConditional,
		When: "steps.disk.output.PCT > 80",
		Then: []engine.Step{thenStep("notify")},
		Else: []engine.Step{thenStep("noop")},
	}
	out, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Data["matched"] != true {
		t.Fatalf("want matched=true: %v", out)
	}
	if !reflect.DeepEqual(*seen, []string{"notify"}) {
		t.Fatalf("executed: %v", *seen)
	}
	if _, ok := rc.Steps["notify"]; !ok {
		t.Fatal("notify not in rc.Steps")
	}
}

func TestConditionalElseBranch(t *testing.T) {
	rc, seen := buildRC(t, map[string]map[string]any{
		"disk": {"PCT": 10.0},
	})
	step := engine.Step{
		Type: engine.StepConditional,
		When: "steps.disk.output.PCT > 80",
		Then: []engine.Step{thenStep("notify")},
		Else: []engine.Step{thenStep("noop")},
	}
	out, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if out.Data["matched"] != false {
		t.Fatalf("want matched=false: %v", out)
	}
	if !reflect.DeepEqual(*seen, []string{"noop"}) {
		t.Fatalf("executed: %v", *seen)
	}
}

func TestConditionalShelbyRefSyntax(t *testing.T) {
	rc, seen := buildRC(t, map[string]map[string]any{
		"disk": {"PCT": 90.0},
	})
	step := engine.Step{
		Type: engine.StepConditional,
		When: "${steps.disk.output.PCT} > 80",
		Then: []engine.Step{thenStep("notify")},
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*seen, []string{"notify"}) {
		t.Fatalf("executed: %v", *seen)
	}
}

func TestConditionalStringCoercion(t *testing.T) {
	// TextFSM values are strings; ensure int() coerces correctly.
	rc, seen := buildRC(t, map[string]map[string]any{
		"df": {"PCT": "85"},
	})
	step := engine.Step{
		Type: engine.StepConditional,
		When: `int(steps.df.output.PCT) > 80`,
		Then: []engine.Step{thenStep("notify")},
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("want notify, got %v", *seen)
	}
}

func TestConditionalLogicOperators(t *testing.T) {
	rc, seen := buildRC(t, map[string]map[string]any{
		"a": {"v": 10.0},
		"b": {"v": "yes"},
	})
	step := engine.Step{
		Type: engine.StepConditional,
		When: `steps.a.output.v > 5 && steps.b.output.v == "yes"`,
		Then: []engine.Step{thenStep("hit")},
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "hit" {
		t.Fatalf("executed: %v", *seen)
	}
}

func TestConditionalEmptyWhenErr(t *testing.T) {
	rc, _ := buildRC(t, nil)
	_, err := Conditional{}.Execute(context.Background(), engine.Step{Type: engine.StepConditional}, rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConditionalNonBoolExprErr(t *testing.T) {
	rc, _ := buildRC(t, map[string]map[string]any{"x": {"v": 1.0}})
	step := engine.Step{
		Type: engine.StepConditional,
		When: "steps.x.output.v + 1", // int, not bool
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConditionalInvalidExprErr(t *testing.T) {
	rc, _ := buildRC(t, nil)
	step := engine.Step{
		Type: engine.StepConditional,
		When: "?!not valid",
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPreprocessWhen(t *testing.T) {
	cases := map[string]string{
		"${steps.a.output.v} > 80":                      "steps.a.output.v > 80",
		"${steps.a.output.records.0.PCT} > 80":          "steps.a.output.records[0].PCT > 80",
		"int(${steps.a.output.records.2.count}) > 5":    "int(steps.a.output.records[2].count) > 5",
		"0.5 > 0":                                       "0.5 > 0", // literal untouched
		"${steps.a.output.list.0} == ${steps.b.output.list.1}": "steps.a.output.list[0] == steps.b.output.list[1]",
	}
	for in, want := range cases {
		got := preprocessWhen(in)
		if got != want {
			t.Errorf("in=%q\n  got  %q\n  want %q", in, got, want)
		}
	}
}

func TestConditionalDotIndexViaRefWrapper(t *testing.T) {
	rc, seen := buildRC(t, map[string]map[string]any{
		"df": {"records": []any{map[string]any{"PCT": "85"}}},
	})
	step := engine.Step{
		Type: engine.StepConditional,
		When: "int(${steps.df.output.records.0.PCT}) > 80",
		Then: []engine.Step{thenStep("hit")},
	}
	_, err := Conditional{}.Execute(context.Background(), step, rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("executed: %v", *seen)
	}
}

func TestConditionalBranchStepFailure(t *testing.T) {
	eng := engine.New()
	fail := failingExecutor{}
	eng.Register("fail", fail)
	rc := &engine.RunContext{
		Pipeline: &engine.Pipeline{Name: "t"},
		Steps:    map[string]engine.Output{"x": {OK: true, Data: map[string]any{"v": 1.0}}},
		RunID:    "r",
		Engine:   eng,
	}
	step := engine.Step{
		Type: engine.StepConditional,
		When: "steps.x.output.v > 0",
		Then: []engine.Step{{ID: "boom", Type: "fail"}},
	}
	out, err := Conditional{}.Execute(context.Background(), step, rc)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.Data["failed"] != "boom" {
		t.Fatalf("failed: %v", out.Data)
	}
}

type failingExecutor struct{}

func (failingExecutor) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	return engine.Output{OK: false, Error: "boom"}, &fakeErr{}
}

type fakeErr struct{}

func (*fakeErr) Error() string { return "boom" }

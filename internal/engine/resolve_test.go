package engine

import (
	"reflect"
	"testing"
)

func TestResolveEnvVar(t *testing.T) {
	t.Setenv("SHELBY_TEST_TOKEN", "secret123")
	rc := ctxWith(nil)
	got, err := Resolve("${env.SHELBY_TEST_TOKEN}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret123" {
		t.Fatalf("got %v", got)
	}
}

func TestResolveEnvInterpolated(t *testing.T) {
	t.Setenv("SHELBY_TEST_HOST", "api.example.com")
	rc := ctxWith(nil)
	got, err := Resolve("https://${env.SHELBY_TEST_HOST}/v1", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/v1" {
		t.Fatalf("got %v", got)
	}
}

func TestResolveEnvMissingIsEmpty(t *testing.T) {
	rc := ctxWith(nil)
	got, err := Resolve("${env.DEFINITELY_UNSET_SHELBY_VAR_XYZ}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %v", got)
	}
}

func ctxWith(data map[string]map[string]any) *RunContext {
	rc := &RunContext{Steps: map[string]Output{}}
	for id, d := range data {
		rc.Steps[id] = Output{StepID: id, OK: true, Data: d}
	}
	return rc
}

func TestResolveSingleRefPreservesType(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"health": {"response_time": 123.0},
	})
	got, err := Resolve("${steps.health.output.response_time}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != 123.0 {
		t.Fatalf("want 123.0, got %v (%T)", got, got)
	}
}

func TestResolveInterpolateStringifies(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"health": {"response_time": 123.0},
		"disk":   {"percentage_used": 47.5},
	})
	got, err := Resolve("lat=${steps.health.output.response_time} disk=${steps.disk.output.percentage_used}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "lat=123 disk=47.5" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveNestedPath(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"api": {"body": map[string]any{"user": map[string]any{"id": 7}}},
	})
	got, err := Resolve("${steps.api.output.body.user.id}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("want 7, got %v", got)
	}
}

func TestResolveMapRecurses(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"a": {"x": 1},
		"b": {"y": 2},
	})
	in := map[string]any{
		"alpha": "${steps.a.output.x}",
		"beta":  "${steps.b.output.y}",
	}
	got, err := Resolve(in, rc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"alpha": 1, "beta": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveMissingStepErr(t *testing.T) {
	rc := ctxWith(nil)
	_, err := Resolve("${steps.nope.output.x}", rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveInvalidRefErr(t *testing.T) {
	rc := ctxWith(nil)
	_, err := Resolve("${foo.bar}", rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveArrayIndex(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"rows": {
			"records": []any{
				map[string]any{"name": "alice"},
				map[string]any{"name": "bob"},
			},
		},
	})
	got, err := Resolve("${steps.rows.output.records.1.name}", rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "bob" {
		t.Fatalf("got %v", got)
	}
}

func TestResolveArrayOutOfRange(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"rows": {"records": []any{map[string]any{"name": "a"}}},
	})
	_, err := Resolve("${steps.rows.output.records.5.name}", rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveArrayBadIndex(t *testing.T) {
	rc := ctxWith(map[string]map[string]any{
		"rows": {"records": []any{map[string]any{"name": "a"}}},
	})
	_, err := Resolve("${steps.rows.output.records.foo.name}", rc)
	if err == nil {
		t.Fatal("expected error")
	}
}

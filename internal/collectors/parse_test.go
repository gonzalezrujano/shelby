package collectors

import (
	"reflect"
	"testing"

	"shelby/internal/engine"
)

func TestParseJSONArrayOfObjects(t *testing.T) {
	text := `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`
	got, err := parseOutput(text, &engine.ParseConfig{Engine: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	r0 := got[0].(map[string]any)
	if r0["a"].(float64) != 1 || r0["b"].(string) != "x" {
		t.Fatalf("row0=%+v", r0)
	}
}

func TestParseJSONPathDescent(t *testing.T) {
	text := `{"result":{"series":[{"t":1,"v":10},{"t":2,"v":20}]}}`
	got, err := parseOutput(text, &engine.ParseConfig{Engine: "json", Path: "result.series"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestParseJSONSingleObject(t *testing.T) {
	got, err := parseOutput(`{"a":1}`, &engine.ParseConfig{Engine: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestParseJSONScalarWrapsInValue(t *testing.T) {
	got, err := parseOutput(`42`, &engine.ParseConfig{Engine: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if v := got[0].(map[string]any)["value"].(float64); v != 42 {
		t.Fatalf("value=%v", v)
	}
}

func TestParseJSONInvalidErrs(t *testing.T) {
	_, err := parseOutput(`not json`, &engine.ParseConfig{Engine: "json"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJSONMissingPathErrs(t *testing.T) {
	_, err := parseOutput(`{"a":1}`, &engine.ParseConfig{Engine: "json", Path: "missing.key"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseLinesBasic(t *testing.T) {
	text := "alpha\nbeta\n\ngamma\n"
	got, err := parseOutput(text, &engine.ParseConfig{Engine: "lines"})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"line": "alpha"},
		map[string]any{"line": "beta"},
		map[string]any{"line": "gamma"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

func TestParseLinesSkipHeader(t *testing.T) {
	text := "HEADER\nval1\nval2\n"
	got, _ := parseOutput(text, &engine.ParseConfig{Engine: "lines", Skip: 1})
	if len(got) != 2 || got[0].(map[string]any)["line"] != "val1" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseRegexNamedCaptures(t *testing.T) {
	text := "ERROR 500 /api/users\nINFO  200 /api/health\nERROR 404 /api/missing\n"
	got, err := parseOutput(text, &engine.ParseConfig{
		Engine:  "regex",
		Pattern: `^(?P<level>\w+)\s+(?P<code>\d+)\s+(?P<path>\S+)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	r := got[0].(map[string]any)
	if r["level"] != "ERROR" || r["code"] != "500" || r["path"] != "/api/users" {
		t.Fatalf("row0=%+v", r)
	}
}

func TestParseRegexSkipsNonMatching(t *testing.T) {
	text := "ok\nERR: boom\njunk\nERR: fire\n"
	got, err := parseOutput(text, &engine.ParseConfig{
		Engine:  "regex",
		Pattern: `^ERR: (?P<msg>.*)$`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[1].(map[string]any)["msg"] != "fire" {
		t.Fatalf("row1=%+v", got[1])
	}
}

func TestParseRegexRequiresNamedCaptures(t *testing.T) {
	_, err := parseOutput("x", &engine.ParseConfig{Engine: "regex", Pattern: `\w+`})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRegexInvalidPattern(t *testing.T) {
	_, err := parseOutput("x", &engine.ParseConfig{Engine: "regex", Pattern: `(?P<a>[`})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTypesCoerceFromRegex(t *testing.T) {
	text := "GET /api 200 42\nPOST /login 401 120\n"
	got, err := parseOutput(text, &engine.ParseConfig{
		Engine:  "regex",
		Pattern: `^(?P<method>\w+)\s+(?P<path>\S+)\s+(?P<code>\d+)\s+(?P<ms>\d+)$`,
		Types:   map[string]string{"code": "int", "ms": "float"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r0 := got[0].(map[string]any)
	if v, _ := r0["code"].(int64); v != 200 {
		t.Fatalf("code=%v (%T)", r0["code"], r0["code"])
	}
	if v, _ := r0["ms"].(float64); v != 42 {
		t.Fatalf("ms=%v (%T)", r0["ms"], r0["ms"])
	}
	if r0["method"] != "GET" {
		t.Fatalf("method=%v", r0["method"])
	}
}

func TestParseTypesBool(t *testing.T) {
	got, err := parseOutput(`[{"enabled":"true"},{"enabled":"0"}]`, &engine.ParseConfig{
		Engine: "json",
		Types:  map[string]string{"enabled": "bool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].(map[string]any)["enabled"] != true {
		t.Fatalf("row0=%+v", got[0])
	}
	if got[1].(map[string]any)["enabled"] != false {
		t.Fatalf("row1=%+v", got[1])
	}
}

func TestParseTypesMissingFieldSkipped(t *testing.T) {
	got, err := parseOutput(`[{"a":"1"},{"b":"x"}]`, &engine.ParseConfig{
		Engine: "json",
		Types:  map[string]string{"a": "int"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := got[0].(map[string]any)["a"].(int64); v != 1 {
		t.Fatalf("a=%v", v)
	}
	// row1 has no "a", should pass through unchanged
	if got[1].(map[string]any)["b"] != "x" {
		t.Fatalf("b=%+v", got[1])
	}
}

func TestParseTypesBadValueErrs(t *testing.T) {
	_, err := parseOutput(`[{"n":"notnumber"}]`, &engine.ParseConfig{
		Engine: "json",
		Types:  map[string]string{"n": "int"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTypesUnknownKindErrs(t *testing.T) {
	_, err := parseOutput(`[{"x":"1"}]`, &engine.ParseConfig{
		Engine: "json",
		Types:  map[string]string{"x": "decimal"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseUnknownEngine(t *testing.T) {
	_, err := parseOutput("x", &engine.ParseConfig{Engine: "yaml"})
	if err == nil {
		t.Fatal("expected error")
	}
}

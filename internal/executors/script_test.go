package executors

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shelby/internal/engine"
)

func rc() *engine.RunContext {
	return &engine.RunContext{
		Pipeline: &engine.Pipeline{Name: "test"},
		Steps:    map[string]engine.Output{},
		RunID:    "r_test",
	}
}

func step(file string, input map[string]any) engine.Step {
	abs, _ := filepath.Abs(filepath.Join("testdata", file))
	return engine.Step{ID: "s", Type: engine.StepScript, Runtime: "bash", File: abs, Input: input}
}

func TestScriptEnvKeysForwarded(t *testing.T) {
	t.Setenv("SHELBY_TEST_API_KEY", "topsecret")
	s := step("echo_input.sh", nil)
	s.EnvKeys = []string{"SHELBY_TEST_API_KEY", "SHELBY_DEFINITELY_UNSET_XYZ"}
	out, err := Script{}.Execute(context.Background(), s, rc())
	if err != nil {
		t.Fatal(err)
	}
	recv, ok := out.Data["received"].(map[string]any)
	if !ok {
		t.Fatalf("no received map: %v", out.Data)
	}
	env, ok := recv["env"].(map[string]any)
	if !ok {
		t.Fatalf("no env map: %v", recv)
	}
	if env["SHELBY_TEST_API_KEY"] != "topsecret" {
		t.Fatalf("env=%+v", env)
	}
	if _, present := env["SHELBY_DEFINITELY_UNSET_XYZ"]; present {
		t.Fatalf("unset var leaked: %+v", env)
	}
}

func TestScriptOK(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("echo_ok.sh", nil), rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	if out.Data["hello"] != "world" {
		t.Fatalf("data: %v", out.Data)
	}
}

func TestScriptMarkerBlock(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("marker.sh", nil), rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	if out.Data["via"] != "marker" {
		t.Fatalf("data: %v", out.Data)
	}
}

func TestScriptReceivesInput(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("echo_input.sh", map[string]any{"x": 1}), rc())
	if err != nil {
		t.Fatal(err)
	}
	recv, ok := out.Data["received"].(map[string]any)
	if !ok {
		t.Fatalf("received not map: %v", out.Data)
	}
	if recv["step_id"] != "s" || recv["pipeline"] != "test" {
		t.Fatalf("missing stdin fields: %v", recv)
	}
	input, _ := recv["input"].(map[string]any)
	if v, _ := input["x"].(float64); v != 1 {
		t.Fatalf("input.x: %v", input)
	}
}

func TestScriptExitErr(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("fail_exit.sh", nil), rc())
	if err == nil {
		t.Fatal("expected error")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if !strings.Contains(out.Error, "boom in stderr") {
		t.Fatalf("stderr not captured: %q", out.Error)
	}
}

func TestScriptBadJSON(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("bad_json.sh", nil), rc())
	if err == nil {
		t.Fatal("expected error")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if !strings.Contains(out.Error, "invalid JSON") {
		t.Fatalf("error: %q", out.Error)
	}
}

func TestScriptOKFalse(t *testing.T) {
	out, err := Script{}.Execute(context.Background(), step("ok_false.sh", nil), rc())
	if err == nil {
		t.Fatal("expected error")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if out.Error != "deliberate" {
		t.Fatalf("error: %q", out.Error)
	}
}

func TestScriptTimeoutKills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := Script{}.Execute(ctx, step("slow.sh", nil), rc())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("did not kill promptly: %s", elapsed)
	}
}

func TestScriptUnknownRuntimeIsExecName(t *testing.T) {
	argv := buildArgv("deno", "x.ts")
	if len(argv) != 2 || argv[0] != "deno" || argv[1] != "x.ts" {
		t.Fatalf("argv: %v", argv)
	}
}

func TestScriptRuntimeMap(t *testing.T) {
	cases := map[string][]string{
		"python":  {"python3", "f.py"},
		"python3": {"python3", "f.py"},
		"node":    {"node", "f.py"},
		"bash":    {"bash", "f.py"},
		"":        {"f.py"},
		"rust":    {"f.py"},
	}
	for rt, want := range cases {
		got := buildArgv(rt, "f.py")
		if len(got) != len(want) {
			t.Fatalf("rt=%s got %v want %v", rt, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("rt=%s got %v want %v", rt, got, want)
			}
		}
	}
}

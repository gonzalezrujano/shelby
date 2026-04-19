package collectors

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shelby/internal/engine"
)

func TestShellBasicNoParse(t *testing.T) {
	out, err := Shell{}.Execute(context.Background(), engine.Step{Command: "echo hello"}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("want ok, got %v", out)
	}
	if s, _ := out.Data["stdout"].(string); !strings.Contains(s, "hello") {
		t.Fatalf("stdout: %q", s)
	}
	if out.Data["exit_code"] != 0 {
		t.Fatalf("exit_code: %v", out.Data["exit_code"])
	}
}

func TestShellExitNonZero(t *testing.T) {
	out, err := Shell{}.Execute(context.Background(), engine.Step{
		Command: "echo nope >&2; exit 3",
	}, rc())
	if err == nil {
		t.Fatal("expected error")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if out.Data["exit_code"] != 3 {
		t.Fatalf("exit_code: %v", out.Data["exit_code"])
	}
	if !strings.Contains(out.Error, "nope") {
		t.Fatalf("stderr not in error: %q", out.Error)
	}
}

func TestShellEmptyCommandErr(t *testing.T) {
	_, err := Shell{}.Execute(context.Background(), engine.Step{}, rc())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShellTextFSMParse(t *testing.T) {
	tmpl, _ := filepath.Abs(filepath.Join("testdata", "simple.textfsm"))
	step := engine.Step{
		Command: `printf 'alice 100 ok\nbob 200 err\ncarol 150 ok\n'`,
		Parse:   &engine.ParseConfig{Engine: "textfsm", Template: tmpl},
	}
	out, err := Shell{}.Execute(context.Background(), step, rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("want ok, got %v", out)
	}
	if out.Data["count"] != 3 {
		t.Fatalf("count: %v", out.Data["count"])
	}
	records, ok := out.Data["records"].([]any)
	if !ok || len(records) != 3 {
		t.Fatalf("records: %v", out.Data["records"])
	}
	first, _ := records[0].(map[string]any)
	if first["NAME"] != "alice" || first["COUNT"] != "100" || first["STATUS"] != "ok" {
		t.Fatalf("first: %v", first)
	}
	second, _ := records[1].(map[string]any)
	if second["NAME"] != "bob" || second["STATUS"] != "err" {
		t.Fatalf("second: %v", second)
	}
}

func TestShellTextFSMMissingTemplate(t *testing.T) {
	step := engine.Step{
		Command: "echo hi",
		Parse:   &engine.ParseConfig{Engine: "textfsm", Template: "testdata/nope.textfsm"},
	}
	_, err := Shell{}.Execute(context.Background(), step, rc())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShellUnknownParseEngine(t *testing.T) {
	step := engine.Step{
		Command: "echo hi",
		Parse:   &engine.ParseConfig{Engine: "xml"},
	}
	_, err := Shell{}.Execute(context.Background(), step, rc())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShellTimeoutKills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Shell{}.Execute(ctx, engine.Step{Command: "sleep 10"}, rc())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("did not kill promptly: %s", elapsed)
	}
}

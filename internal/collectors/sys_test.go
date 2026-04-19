package collectors

import (
	"context"
	"testing"

	"shelby/internal/engine"
)

func TestSysStatMemory(t *testing.T) {
	out, err := SysStat{}.Execute(context.Background(), engine.Step{Source: "memory"}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("want ok, got %v", out)
	}
	for _, k := range []string{"percent", "used", "free", "available", "total"} {
		if _, ok := out.Data[k]; !ok {
			t.Fatalf("missing %s: %v", k, out.Data)
		}
	}
}

func TestSysStatHost(t *testing.T) {
	out, err := SysStat{}.Execute(context.Background(), engine.Step{Source: "host"}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := out.Data["hostname"].(string); h == "" {
		t.Fatalf("no hostname: %v", out.Data)
	}
}

func TestSysStatDiskRoot(t *testing.T) {
	out, err := SysStat{}.Execute(context.Background(), engine.Step{Source: "/"}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Data["percentage_used"].(float64); !ok {
		t.Fatalf("no percentage_used: %v", out.Data)
	}
	if _, ok := out.Data["total"]; !ok {
		t.Fatalf("no total: %v", out.Data)
	}
}

func TestSysStatUnknownSource(t *testing.T) {
	_, err := SysStat{}.Execute(context.Background(), engine.Step{Source: "wat"}, rc())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSysStatCPU(t *testing.T) {
	if testing.Short() {
		t.Skip("cpu sampling takes 200ms")
	}
	out, err := SysStat{}.Execute(context.Background(), engine.Step{Source: "cpu"}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Data["percent"].(float64); !ok {
		t.Fatalf("no cpu percent: %v", out.Data)
	}
}

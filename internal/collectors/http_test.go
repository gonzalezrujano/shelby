package collectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"shelby/internal/engine"
)

func rc() *engine.RunContext {
	return &engine.RunContext{
		Pipeline: &engine.Pipeline{Name: "t"},
		Steps:    map[string]engine.Output{},
		RunID:    "r",
	}
}

func TestHTTPGetCustomHeadersAndEnvSource(t *testing.T) {
	t.Setenv("SHELBY_TEST_TOKEN", "Bearer abc123")
	var gotAuth, gotTrace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTrace = r.Header.Get("X-Trace")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	// interpolate env into Source via ${env.*} and Headers
	ctx := rc()
	ctx.Steps["prev"] = engine.Output{OK: true, Data: map[string]any{"trace": "t-42"}}
	out, err := HTTPGet{}.Execute(context.Background(), engine.Step{
		Source: srv.URL + "/v1",
		Headers: map[string]string{
			"Authorization": "${env.SHELBY_TEST_TOKEN}",
			"X-Trace":       "${steps.prev.output.trace}",
		},
	}, ctx)
	if err != nil || !out.OK {
		t.Fatalf("err=%v out=%+v", err, out)
	}
	if gotAuth != "Bearer abc123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotTrace != "t-42" {
		t.Fatalf("trace=%q", gotTrace)
	}
}

func TestHTTPGetOKJSONExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"user":{"id":7,"name":"alice"}}`)
	}))
	defer srv.Close()

	out, err := HTTPGet{}.Execute(context.Background(), engine.Step{
		Source: srv.URL, Extract: "user.id",
	}, rc())
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("want ok, got %v", out)
	}
	if out.Data["status_code"] != 200 {
		t.Fatalf("status: %v", out.Data["status_code"])
	}
	if _, ok := out.Data["response_time_ms"].(float64); !ok {
		t.Fatalf("no response_time_ms")
	}
	if v, _ := out.Data["user.id"].(float64); v != 7 {
		t.Fatalf("extract user.id: %v", out.Data)
	}
}

func TestHTTPGetResponseTimeAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	out, err := HTTPGet{}.Execute(context.Background(), engine.Step{
		Source: srv.URL, Extract: "response_time",
	}, rc())
	if err != nil {
		t.Fatal(err)
	}
	rt, ok := out.Data["response_time"].(float64)
	if !ok || rt <= 0 {
		t.Fatalf("response_time: %v", out.Data["response_time"])
	}
}

func TestHTTPGetNonJSONBodyTruncated(t *testing.T) {
	big := make([]byte, 10000)
	for i := range big {
		big[i] = 'x'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big)
	}))
	defer srv.Close()

	out, err := HTTPGet{}.Execute(context.Background(), engine.Step{Source: srv.URL}, rc())
	if err != nil {
		t.Fatal(err)
	}
	bs, _ := out.Data["body"].(string)
	if len(bs) == 0 || len(bs) > 5000 {
		t.Fatalf("body len=%d", len(bs))
	}
}

func TestHTTPGet500Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	out, err := HTTPGet{}.Execute(context.Background(), engine.Step{Source: srv.URL}, rc())
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if out.Data["status_code"] != 500 {
		t.Fatalf("status: %v", out.Data["status_code"])
	}
}

func TestHTTPGetEmptySourceErr(t *testing.T) {
	_, err := HTTPGet{}.Execute(context.Background(), engine.Step{}, rc())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDotLookup(t *testing.T) {
	v := map[string]any{"a": map[string]any{"b": map[string]any{"c": 9}}}
	got, ok := dotLookup(v, "a.b.c")
	if !ok || got != 9 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := dotLookup(v, "a.b.missing"); ok {
		t.Fatal("expected miss")
	}
	if _, ok := dotLookup(v, "a.b.c.d"); ok {
		t.Fatal("expected miss on non-map leaf")
	}
}

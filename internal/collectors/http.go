package collectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"context"

	"shelby/internal/engine"
)

// HTTPGet performs a GET on Step.Source.
// Data keys: status_code, response_time_ms, response_time (alias, ms float),
// body (parsed if JSON, truncated string otherwise).
// If Step.Extract is set and body is JSON, a dotted lookup populates that key.
// The special extract "response_time" is always satisfied by the ms alias.
type HTTPGet struct {
	Client *http.Client
}

var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

const httpMaxBody = 1 << 20 // 1MB

func (h HTTPGet) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	if s.Source == "" {
		err := errors.New("http_get: source required")
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	client := h.Client
	if client == nil {
		client = defaultHTTPClient
	}
	source, err := resolveStr(s.Source, rc)
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	for k, v := range s.Headers {
		rv, err := resolveStr(v, rc)
		if err != nil {
			return engine.Output{OK: false, Error: fmt.Sprintf("header %s: %v", k, err)}, err
		}
		req.Header.Set(k, rv)
	}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpMaxBody))
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	ms := float64(elapsed.Microseconds()) / 1000.0
	data := map[string]any{
		"status_code":      resp.StatusCode,
		"response_time_ms": ms,
		"response_time":    ms,
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "json") {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			data["body"] = parsed
			if s.Extract != "" && s.Extract != "response_time" {
				if v, ok := dotLookup(parsed, s.Extract); ok {
					data[s.Extract] = v
				}
			}
		} else {
			data["body"] = truncateStr(string(body), 4096)
		}
	} else {
		data["body"] = truncateStr(string(body), 4096)
	}

	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("http %d", resp.StatusCode)
		return engine.Output{OK: false, Data: data, Error: msg}, errors.New(msg)
	}
	return engine.Output{OK: true, Data: data}, nil
}

// resolveStr runs a string through engine.Resolve, stringifying the result.
func resolveStr(s string, rc *engine.RunContext) (string, error) {
	v, err := engine.Resolve(s, rc)
	if err != nil {
		return "", err
	}
	if str, ok := v.(string); ok {
		return str, nil
	}
	return fmt.Sprintf("%v", v), nil
}

func dotLookup(v any, path string) (any, bool) {
	cur := v
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

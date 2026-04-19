package executors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"shelby/internal/engine"
)

// Script spawns <runtime> <file>, pipes ScriptRequest JSON via stdin,
// reads ScriptResponse JSON from stdout. stderr captured as logs.
type Script struct{}

var runtimeCmd = map[string][]string{
	"node":    {"node"},
	"python":  {"python3"},
	"python3": {"python3"},
	"bash":    {"bash"},
	"sh":      {"sh"},
}

// Marker block: lets scripts mix logs on stdout with a delimited JSON payload.
//
//	<<<SHELBY_OUT
//	{ ... }
//	SHELBY_OUT>>>
var markerRe = regexp.MustCompile(`(?s)<<<SHELBY_OUT\s*\n(.*?)\n?SHELBY_OUT>>>`)

const shelbyVersion = "0.1.0"

func (Script) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	if s.File == "" {
		err := errors.New("script: file required")
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	argv := buildArgv(s.Runtime, s.File)
	if len(argv) == 0 {
		err := fmt.Errorf("script: unknown runtime %q", s.Runtime)
		return engine.Output{OK: false, Error: err.Error()}, err
	}

	env := map[string]string{"SHELBY_VERSION": shelbyVersion}
	for _, k := range s.EnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	req := engine.ScriptRequest{
		StepID:   s.ID,
		RunID:    rc.RunID,
		Pipeline: rc.Pipeline.Name,
		Input:    s.Input,
		Context:  engine.ScriptRequestContext{Steps: rc.Steps},
		Env:      env,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return engine.Output{OK: false, Error: err.Error()}, err
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// graceful: SIGTERM on cancel, SIGKILL 2s later
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second

	runErr := cmd.Run()
	stderrStr := truncate(stderr.String(), 2000)

	if runErr != nil {
		msg := runErr.Error()
		if stderrStr != "" {
			msg = msg + ": " + stderrStr
		}
		return engine.Output{OK: false, Error: msg, Data: map[string]any{"stderr": stderrStr}}, runErr
	}

	payload := extractPayload(stdout.String())
	if payload == "" {
		err := fmt.Errorf("script: empty stdout")
		return engine.Output{OK: false, Error: err.Error(), Data: map[string]any{"stderr": stderrStr}}, err
	}
	var resp engine.ScriptResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		werr := fmt.Errorf("invalid JSON stdout: %v: %s", err, truncate(payload, 500))
		return engine.Output{OK: false, Error: werr.Error(), Data: map[string]any{"stderr": stderrStr}}, werr
	}
	if !resp.OK {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "script returned ok=false"
		}
		return engine.Output{OK: false, Error: errMsg, Data: resp.Data}, fmt.Errorf("script fail: %s", errMsg)
	}
	return engine.Output{OK: true, Data: resp.Data}, nil
}

func buildArgv(runtime, file string) []string {
	if cmd, ok := runtimeCmd[runtime]; ok {
		return append(append([]string{}, cmd...), file)
	}
	switch runtime {
	case "", "bin", "rust":
		return []string{file}
	default:
		// treat runtime as executable name (e.g. "deno", "ruby")
		return []string{runtime, file}
	}
}

func extractPayload(stdout string) string {
	if m := markerRe.FindStringSubmatch(stdout); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(stdout)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}


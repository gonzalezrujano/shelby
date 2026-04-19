package collectors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"shelby/internal/engine"
)

// Shell runs Step.Command via a shell. If Step.Parse is set, stdout is parsed
// and a `records` list is added to the output.
//
// Data keys:
//   - stdout (string, truncated for storage)
//   - stderr (string, truncated)
//   - exit_code (int)
//   - records ([]any of map[string]any) if parsed
//   - count (int) number of records
type Shell struct {
	Executable string // default "bash"; set to "sh" or a full path to override
}

const shellStdoutCap = 64 * 1024

func (sh Shell) Execute(ctx context.Context, s engine.Step, rc *engine.RunContext) (engine.Output, error) {
	if s.Command == "" {
		err := errors.New("shell: command required")
		return engine.Output{OK: false, Error: err.Error()}, err
	}
	bin := sh.Executable
	if bin == "" {
		bin = "bash"
	}

	cmd := exec.CommandContext(ctx, bin, "-c", s.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second

	runErr := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	data := map[string]any{
		"stdout":    truncateStr(stdoutStr, shellStdoutCap),
		"stderr":    truncateStr(stderrStr, 4096),
		"exit_code": exitCode,
	}

	// binary missing / ctx cancel / io err (exit_code stays -1 or similar)
	if runErr != nil && exitCode <= 0 && stdoutStr == "" {
		return engine.Output{OK: false, Data: data, Error: runErr.Error()}, runErr
	}
	if exitCode != 0 {
		msg := fmt.Sprintf("exit %d: %s", exitCode, truncateStr(stderrStr, 500))
		return engine.Output{OK: false, Data: data, Error: msg}, errors.New(msg)
	}

	if s.Parse != nil && s.Parse.Engine != "" {
		records, err := parseOutput(stdoutStr, s.Parse)
		if err != nil {
			return engine.Output{OK: false, Data: data, Error: fmt.Sprintf("parse: %v", err)}, err
		}
		data["records"] = records
		data["count"] = len(records)
	}

	return engine.Output{OK: true, Data: data}, nil
}


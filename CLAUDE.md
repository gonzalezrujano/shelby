# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o shelby ./cmd/shelby

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/engine/...
go test ./internal/collectors/...

# Run a single test by name
go test ./internal/engine/ -run TestResolve

# Vet
go vet ./...

# Run a pipeline ad-hoc (no registration needed)
./shelby run disk.yaml

# Run with verbose step output
./shelby run -v disk.yaml

# Run with full debug (resolved inputs + data dumps)
./shelby run --debug disk.yaml

# Validate pipeline YAML (structural errors)
./shelby validate disk.yaml

# Lint pipeline YAML (style warnings)
./shelby lint disk.yaml

# Daemon + web UI at :8080
./shelby serve

# viz dashboard in sandbox mode (no Shelby server needed)
./shelby viz sandbox ./my-dashboard --watch

# viz dashboard against live Shelby server
./shelby viz serve ./my-dashboard --shelby http://localhost:8080 --watch
```

## Architecture

Shelby is a read-only YAML-driven metrics collection engine. The core data flow is:

**Pipeline YAML → `internal/config` → `engine.Pipeline` → `internal/runner` → `engine.Engine` → collectors/executors → `engine.RunContext` → `internal/store`**

### Key packages

**`internal/engine`** — Core types and orchestration. `Engine` holds a registry of `Executor` implementations keyed by `StepType`. `Engine.Run` iterates steps sequentially, calling `RunStep` for each. `RunStep` resolves `${steps.<id>.output.<field>}` references in `Step.Input` via `Resolve()` before dispatch. A `StepObserver` hook fires after every step (used by `run -v`).

**`internal/runner`** — Wires all executors together. `NewEngine()` registers all built-in collectors and executors. `ExecuteWithObserver` is the primary entry point: builds an engine, runs the pipeline, evaluates the `output:` map via `engine.FinalOutput`, and optionally persists to the store.

**`internal/collectors`** — Implements `http_get`, `sys_stat`, and `shell` step types. The `shell` collector feeds stdout through one of four parsers (`textfsm`, `json`, `lines`, `regex`) configured in `Step.ParseConfig`. The `types:` field on `ParseConfig` coerces string captures to `int`, `float`, or `bool` after parsing.

**`internal/executors`** — Implements `script`, `conditional`, and `aggregator` step types. `Script` spawns a subprocess using `runtime` + `file`, pipes a `ScriptRequest` JSON to stdin, and reads `ScriptResponse` from stdout (or between `<<<SHELBY_OUT / SHELBY_OUT>>>` markers). `Conditional` calls back into `engine.Engine.RunStep` to execute nested `then`/`else` branches. `Aggregator` operates over arrays of prior step outputs.

**`internal/engine/resolve.go`** — Reference resolution. `${steps.<id>.output.<field>}` paths support nested dot-paths (e.g., `records.0.MOUNT`). Full-string refs preserve type; interpolated refs stringify. `${env.VAR}` reads from the host environment.

**`internal/store`** — File-based persistence at `$SHELBY_HOME` (default `~/.shelby`). Registrations are stored as `pipelines/<slug>.json` (pointers to the source YAML path — edits to the YAML are picked up on next run). Run history is stored as `runs/<slug>/<ts>_<runid>.json`. Slugs are derived from pipeline names via `Slugify()`.

**`internal/server`** — `shelby serve` daemon. `Scheduler` polls registered pipelines and fires `runner.Execute` when their interval elapses. `Server` exposes an HTML dashboard and a JSON API (`/api/pipelines`, `/api/pipelines/<slug>/runs`, `/api/pipelines/<slug>/series`) consumed by `shelby viz`.

**`internal/lint`** — Two-tier static analysis: `Validate` returns hard errors (missing required fields, unknown step refs, duplicate IDs, missing script files), `Lint` returns warnings (missing timeouts, unreferenced outputs, non-snake_case IDs).

**`internal/viz`** — Dashboard visualization engine (`shelby viz`). Renders pongo2 (Jinja2-compatible) `.html.j2` templates against live Shelby API data or mock data from `sandbox.yml`. Templates are resolved via a `combinedLoader` that checks the user's dashboard directory first, then falls back to embedded builtin templates (`builtins/widgets/`). Built-in widget types: `counter`, `gauge`, `status`, `table`, `timeseries`. Custom widgets use `type: custom` with an explicit `template:` path. Widget `binding:` fields support `series:` (time-series from a pipeline field), `runs:` (run history), and `value:` (static). Live reload is delivered via SSE (`/api/livereload`).

### Step execution contract

Every `Executor.Execute` call returns `(Output, error)`. `Output.Data` is a `map[string]any` whose keys become accessible via `${steps.<id>.output.<key>}` in subsequent steps. `OK: false` on the output does not stop the pipeline — only a returned non-nil error does.

### Script subprocess contract

Shelby writes a `ScriptRequest` JSON to stdin and expects `ScriptResponse` JSON on stdout. Exit non-zero or invalid JSON = failure. SIGTERM is sent on cancel/timeout, SIGKILL 2 seconds later. stderr is captured into `output.stderr`.

### viz dashboard layout

A dashboard project is a directory containing:
- `dashboard.yml` — declares widgets with `col`/`row`/`width`/`height` grid positions and `binding:` data sources
- `widgets/` — custom `.html.j2` templates (override built-ins by shadowing names)
- `sandbox.yml` — mock data for `shelby viz sandbox` (format: `pipelines: <slug>: <field>: [values...]`)

Templates receive `data` (resolved bindings), `config`, and `meta` (title, id) contexts. `SeriesData` methods accessible in templates: `.points`, `.last`, `.previous`, `.trend`, `.max`, `.min`, `.avg`, `.reversed`.

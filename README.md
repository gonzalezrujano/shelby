# Shelby

> A YAML-driven observation & metrics engine. Read-only by design — it never deploys,
> writes back, or talks to production databases. Describe what to collect, Shelby runs it.

Shelby is a single Go binary that turns short YAML pipelines into repeatable,
schedulable, inspectable data collection jobs. Each pipeline is a list of
**steps**; each step produces a typed `Output` that later steps can reference
with `${steps.<id>.output.<field>}`.

## Features

- **Native collectors:** `http_get`, `sys_stat` (CPU/RAM/disk/host via gopsutil), `shell`.
- **External scripts:** run Node, Python, Rust, or any executable — JSON contract over stdin/stdout.
- **Control flow:** `conditional` steps with `when`/`then`/`else` powered by expr-lang.
- **Reductions:** `aggregator` step with `sum | avg | min | max | count` over prior outputs.
- **Shell parsers:** `textfsm`, `regex`, `json`, `lines` — structure any command's stdout into records.
- **Type coercion:** declare `types: { pct: int, ok: bool }` on any parser to drop string-casting in expressions.
- **Secrets & env:** `${env.API_TOKEN}` anywhere; HTTP `headers:`; per-script `env_keys:` whitelist.
- **Daemon + Web UI:** `shelby serve` schedules pipelines and exposes a local HTML dashboard.
- **TUI:** `shelby tui` for a terminal dashboard (bubbletea).
- **Run history:** per-pipeline JSON log in `~/.shelby/runs/<slug>/`.

## Install

Requires Go 1.26+.

```bash
git clone https://github.com/<you>/shelby.git
cd shelby
go build -o shelby ./cmd/shelby
```

## Quickstart

```bash
# 1. Write a pipeline
cat > disk.yaml <<'YAML'
name: "Disk Watch"
interval: 30s
steps:
  - id: df
    type: shell
    command: "df -Pk /"
    parse:
      engine: textfsm
      template: ./examples/templates/df.textfsm
      types: { BLOCKS: int, USED: int, AVAIL: int, PCT: int }

  - id: max_pct
    type: aggregator
    op: max
    over: ["${steps.df.output.records}"]
    field: PCT

output:
  mount:   ${steps.df.output.records.0.MOUNT}
  pct:     ${steps.max_pct.output.value}
YAML

# 2. Run it ad-hoc
shelby run disk.yaml

# 3. Or register + schedule it
shelby add disk.yaml
shelby list
shelby serve          # web UI at http://localhost:8080
```

## CLI

```
shelby add <file.yaml>       register pipeline (path pointer; edits live)
shelby list                  table of registered pipelines with last-run status
shelby show <name|slug>      YAML + last-run summary
shelby rm   <name|slug>      unregister (drops run history)
shelby run  <name|file.yaml> run ad-hoc (no registration needed for files)
shelby logs <name|slug>      recent run history
shelby tui                   interactive terminal dashboard
shelby serve [-addr :8080]   scheduler daemon + web UI
```

Store location: `$SHELBY_HOME` or `~/.shelby`.

## Pipeline reference

```yaml
name: "My Pipeline"              # required
description: "..."
interval: 60s                    # Go duration (s/m/h); 0 = manual only

steps:                           # ordered; each Output is keyed by step id
  - id: ping
    type: http_get
    source: "https://${env.API_HOST}/health"
    headers:
      Authorization: "Bearer ${env.API_TOKEN}"
    extract: status              # dot-path into parsed JSON body

  - id: top
    type: shell
    command: "ps -eo pid,comm | head -5"
    parse:
      engine: lines
      skip: 1                    # drop header

  - id: cpu
    type: sys_stat
    source: cpu                  # cpu | memory | host | /some/path

  - id: score
    type: script
    runtime: python              # node | python | bash | sh | <bin name>
    file: ./enrich.py
    timeout: 5s
    env_keys: [API_TOKEN]        # whitelist of host env to forward
    input:
      latency: ${steps.ping.output.response_time}
      cpu:     ${steps.cpu.output.percent}

  - id: alert
    type: conditional
    when: "${steps.score.output.value} > 80"
    then:
      - id: notify
        type: shell
        command: "curl -X POST ${env.SLACK_WEBHOOK} -d 'high score'"

  - id: avg_latency
    type: aggregator
    op: avg                      # sum | avg | min | max | count
    over: ["${steps.top.output.records}"]
    field: latency_ms            # extract key from array-of-maps

output:                          # final pipeline output map (recorded in run history)
  latency: ${steps.ping.output.response_time}
  score:   ${steps.score.output.value}
```

### Reference syntax

- `${steps.<id>.output.<field>}` — prior step data (nested: `records.0.MOUNT`).
- `${env.VAR}` — host environment variable (missing = empty string).
- Full-string refs preserve type; interpolated refs stringify.

### Shell parsers

| Engine    | Required        | Notes                                           |
|-----------|-----------------|-------------------------------------------------|
| `textfsm` | `template:`     | External TextFSM file; values become strings.   |
| `regex`   | `pattern:`      | Must use `(?P<name>...)` named captures.        |
| `json`    | —               | `path:` descends into parsed JSON (dot-path).   |
| `lines`   | —               | Splits on `\n`; `skip:` drops first N lines.    |

Add `types: { field: int|float|bool }` to coerce string captures.

### Script contract

Shelby spawns `<runtime> <file>`, pipes a `ScriptRequest` JSON via stdin,
expects a `ScriptResponse` JSON on stdout:

```json
// stdin
{"step_id":"score","run_id":"r_abc","pipeline":"My Pipeline",
 "input":{"latency":123},"context":{"steps":{...}},"env":{"API_TOKEN":"..."}}

// stdout (whole body, or between <<<SHELBY_OUT / SHELBY_OUT>>> markers)
{"ok":true,"data":{"value":72.4,"note":"healthy"}}
```

Exit non-zero or invalid JSON = failure. Shelby sends SIGTERM on cancel/timeout,
SIGKILL 2s later. stderr is captured into `output.stderr`.

## Examples

See `examples/`:

- `monitor_alpha.yaml` — HTTP + sys_stat + Python script + conditional.
- `shell_demo.yaml` — shell commands with TextFSM + aggregator + types.
- `parsers_demo.yaml` — all four parse engines chained through an aggregator.

## Development

```bash
go test ./...
go vet ./...
go build ./cmd/shelby
```

Layout:

```
cmd/shelby/          main entrypoint
internal/engine      pipeline types + runner + ref resolver
internal/collectors  http_get, sys_stat, shell + parsers
internal/executors   script, conditional, aggregator
internal/runner      executor registry + run orchestration
internal/config      YAML loader
internal/store       registration + run history on disk
internal/cli         subcommand dispatch
internal/tui         bubbletea dashboard
internal/server      scheduler + HTTP web UI
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

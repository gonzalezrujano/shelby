# Shelby

Shelby is two engines in one binary: a YAML pipeline engine that collects
metrics from anything — HTTP, shell, system stats, scripts — without ever
touching production, and a dashboard engine (`shelby viz`) that turns that
data into a live, self-hosted HTML dashboard with no JS build step. Either
one is useful alone. Together — and read by a coding agent through one
machine-readable [`llms.txt`](./llms.txt) — they let you go from "I want
visibility into X" to a running, branded dashboard in minutes.

![Mission Control dashboard](./examples/mission-control/screenshot.png)

The dashboard above — three pipelines, two Python scripts, three custom
widgets — was built end-to-end by Claude Code from a plain-English request,
using `llms.txt` as its only specification. The full, runnable source is in
[`examples/mission-control`](./examples/mission-control).

## Features

**Pipelines**

- **Native collectors:** `http_get`, `sys_stat` (CPU/RAM/disk/host via gopsutil), `shell`.
- **External scripts:** run Node, Python, Rust, or any executable — JSON contract over stdin/stdout.
- **Control flow:** `conditional` steps with `when`/`then`/`else` powered by expr-lang.
- **Reductions:** `aggregator` step with `sum | avg | min | max | count` over prior outputs.
- **Shell parsers:** `textfsm`, `regex`, `json`, `lines` — structure any command's stdout into records.
- **Type coercion:** declare `types: { pct: int, ok: bool }` on any parser to drop string-casting in expressions.
- **Secrets & env:** `${env.API_TOKEN}` anywhere; HTTP `headers:`; per-script `env_keys:` whitelist.
- **Daemon + scheduler:** `shelby serve` polls registered pipelines on their `interval:` and records every run.
- **Run history:** per-pipeline JSON log in `~/.shelby/runs/<slug>/`.

**Dashboards (`shelby viz`)**

- **Built-in widgets:** `counter`, `gauge`, `status`, `table`, `timeseries` — no CSS or JS required.
- **Custom widgets:** drop a `.html.j2` file in `widgets/` for anything the built-ins don't cover.
- **Grid layout:** `col` / `row` / `width` / `height` positioning in `dashboard.yml`.
- **Three binding kinds:** `series:` (time-series from a pipeline field), `runs:` (run history), `value:` (static).
- **Sandbox mode:** `shelby viz sandbox` renders the full dashboard against mock data — no Shelby server needed.
- **Live reload:** `shelby viz serve --watch` re-renders templates on every request over SSE.
- **Works standalone:** point it at any Shelby-compatible API; it doesn't require running pipelines locally.

Already have Grafana or Datadog? Use just the pipelines and pipe their output
elsewhere. Already have metrics in another format? Point `shelby viz` at any
Shelby-compatible API and skip the pipeline engine entirely. Nothing here is
coupled by force.

## Install

Requires Go 1.26+.

```bash
git clone https://github.com/<you>/shelby.git
cd shelby
go build -o shelby ./cmd/shelby
```

## Quickstart

End to end: write a pipeline, run it, then visualize it — all with the same binary.

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

# 2. Run it ad-hoc, or register + schedule it
shelby run disk.yaml
shelby add disk.yaml
shelby serve                     # daemon + web UI at http://localhost:8080

# 3. Visualize it — no dashboard project needed to start exploring
shelby viz serve ./examples/mission-control --shelby http://localhost:8080 --watch
# or, with no server running at all:
shelby viz sandbox ./examples/mission-control --watch
# open http://localhost:5000
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
shelby viz sandbox <dir>     render a dashboard against mock data
shelby viz serve <dir>       render a dashboard against a live Shelby server
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

## Dashboard reference

A dashboard project is a directory:

```
my-dashboard/
  dashboard.yml     # widgets: grid position (col/row/width/height) + binding
  widgets/          # custom .html.j2 templates (override built-ins by shadowing names)
  sandbox.yml       # mock data for `shelby viz sandbox`
```

Templates receive `data` (resolved bindings), `config`, and `meta` (title, id).
`SeriesData` methods available in templates: `.points`, `.last`, `.previous`,
`.trend`, `.max`, `.min`, `.avg`, `.reversed`.

```yaml
name: "My Dashboard"
refresh: 30s
layout:
  columns: 12

widgets:
  - id: cpu-gauge
    title: CPU Usage
    type: gauge                  # counter | gauge | status | table | timeseries | custom
    col: 1
    row: 1
    width: 3
    height: 3
    binding:
      value:
        pipeline: system-health
        series: cpu.percent
        limit: 2
    config:
      min: 0
      max: 100
      unit: "%"
```

## Examples

See `examples/`:

- [`mission-control/`](./examples/mission-control) — a full pipelines-to-dashboard project (service health, Redis-backed queue, user growth). Built by Claude Code from `llms.txt`; runs against mock data out of the box.
- [`system-health/`](./examples/system-health) — a single `sys_stat` pipeline visualized with built-in and custom widgets.
- `monitor_alpha.yaml` — HTTP + sys_stat + Python script + conditional.
- `shell_demo.yaml` — shell commands with TextFSM + aggregator + types.
- `parsers_demo.yaml` — all four parse engines chained through an aggregator.

## Docker

### Build locally

```bash
docker build -t shelby:latest .
```

Multi-platform (to push to Docker Hub):

```bash
docker buildx create --use   # first time only
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-user/shelby:latest \
  --push \
  .
```

### Run with docker-compose

The repo includes a `docker-compose.yml` with two services:

| Service | Command | Port |
|---|---|---|
| `shelby` | `shelby serve` — daemon + web UI | 8080 |
| `shelby-viz` | `shelby viz serve` — visual dashboard | 5000 |

```bash
# 1. Copy and edit environment variables
cp .env.example .env

# 2. Create folders for your resources
mkdir -p pipelines dashboards/my-dashboard

# 3. Bring up the stack
docker compose up -d

# 4. Register a pipeline (once per pipeline)
docker compose exec shelby shelby add /pipelines/my-pipeline.yaml
```

Expected layout in the working directory:

```
├── docker-compose.yml
├── .env
├── pipelines/          # pipeline .yaml files
└── dashboards/
    └── my-dashboard/   # dashboard.yml + widgets/ + sandbox.yml
```

Relevant environment variables (see `.env.example`):

| Variable | Default | Description |
|---|---|---|
| `SHELBY_IMAGE` | `your-user/shelby:latest` | Image to use |
| `SHELBY_PORT` | `8080` | Daemon port |
| `VIZ_PORT` | `5000` | Viz port |
| `VIZ_DASHBOARD` | `my-dashboard` | Active dashboard folder |
| `TZ` | `UTC` | Timezone |

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
internal/viz         dashboard rendering engine (shelby viz)
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

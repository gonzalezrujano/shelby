---
sidebar_position: 4
---

# Pipelines & Script Contract

The core of Shelby relies on YAML-driven pipelines composed of steps, expressions, and configuration.

## Pipeline Reference

```yaml
name: "Monitor Salud Alpha"      # required
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

## Reference Syntax

- `${steps.<id>.output.<field>}`: Retrieve output from previous steps in nested dot notation (e.g., `records.0.MOUNT`).
- `${env.VAR}`: Expand to system environment variables (missing maps to empty strings).
- Type matching: A full-string reference preserves type while interpolated refs convert back to strings.

## Shell Parsers

Use `parse` under `shell` steps to structure outputs:

| Engine | Required | Notes |
|---|---|---|
| `textfsm` | `template:` | External TextFSM file; values become strings. |
| `regex` | `pattern:` | Must use `(?P<name>...)` named captures. |
| `json` | — | `path:` descends into parsed JSON (dot-path). |
| `lines` | — | Splits on `\n`; `skip:` drops first N lines. |

> Tip: Use `types: { field: int|float|bool }` to coerce string captures.

## Script Contract

Shelby spawns the process using `<runtime> <file>`, writes a **JSON `ScriptRequest`** into standard input (`stdin`) and parses the **JSON `ScriptResponse`** from standard output (`stdout`).

Exit non-zero or supply invalid JSON, and execution is marked as a failure. Shelby sends `SIGTERM` if execution times out (with `SIGKILL` 2 seconds later). `stderr` goes directly into the step's `output.stderr`.

### Stdin (ScriptRequest)
```json
{
  "step_id": "score",
  "run_id": "r_abc",
  "pipeline": "My Pipeline",
  "input": { "latency": 123 },
  "context": {
    "steps": {
      "ping": { "ok": true, "data": { "response_time": 123 } }
    }
  },
  "env": { "API_TOKEN": "..." }
}
```

### Stdout (ScriptResponse)

Provide output in one line or enclose it securely between `<<<SHELBY_OUT` and `SHELBY_OUT>>>` markers to allow custom `console.log` logging throughout the script.

```json
{"ok": true, "data": {"value": 72.4, "note": "healthy"}}
```

---
sidebar_position: 1
slug: /
---

# Introduction

> A YAML-driven observation & metrics engine. Read-only by design — it never deploys, writes back, or talks to production databases. Describe what to collect, Shelby runs it.

Shelby is a single Go binary that turns short YAML pipelines into repeatable, schedulable, inspectable data collection jobs. Each pipeline is a list of **steps**; each step produces a typed `Output` that later steps can reference with `${steps.<id>.output.<field>}`.

## Features

- **Native collectors:** `http_get`, `sys_stat` (CPU/RAM/disk/host via gopsutil), `shell`, `log_read`.
- **External scripts:** run Node, Python, Rust, or any executable — JSON contract over stdin/stdout.
- **Control flow:** `conditional` steps with `when`/`then`/`else` powered by expr-lang.
- **Reductions:** `aggregator` step with `sum | avg | min | max | count` over prior outputs.
- **Shell parsers:** `textfsm`, `regex`, `json`, `lines` — structure any command's stdout into records.
- **Type coercion:** declare `types: { pct: int, ok: bool }` on any parser to drop string-casting in expressions.
- **Secrets & env:** `${env.API_TOKEN}` anywhere; HTTP `headers:`; per-script `env_keys:` whitelist.
- **Daemon + Web UI:** `shelby serve` schedules pipelines and exposes a local HTML dashboard.
- **TUI:** `shelby tui` for a terminal dashboard (bubbletea).
- **Run history:** per-pipeline JSON log in `~/.shelby/runs/<slug>/`.

## Architecture 🛠️

Shelby consists of several internal modules designed for maximum concurrency and non-blocking execution:

1. **Collectors (Read-only):** Native Go implementations like `http_get` and `sys_stat`.
2. **Pipeline Engine:** Executes steps and maintains strongly-typed `Output` references. 
3. **Step Types:** Includes scripts, conditional branches and aggregators.
4. **Exporters:** Expose data via `net/http` locally or push to an optional cloud endpoint.
5. **Concurrency:** A dedicated goroutine runs each step independently.

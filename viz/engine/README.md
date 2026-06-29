# shelby-viz engine

Jinja2-based dashboard renderer for [Shelby](../../README.md) pipelines.

## Install

```bash
pip install -e .
# or from PyPI once published:
pip install shelby-viz
```

## Quick start

```bash
# 1. Start the Shelby API
shelby serve

# 2. Serve a dashboard
shelby-viz serve examples/system-health --shelby http://localhost:8080
# → open http://localhost:5000

# 3. Build a static HTML snapshot
shelby-viz build examples/system-health --out dist/

# 4. Scaffold a new dashboard project
shelby-viz init my-dashboard

# 5. Validate dashboard.yml + templates
shelby-viz validate my-dashboard
```

## Dashboard project layout

Any folder with a `dashboard.yml` is a dashboard project:

```
my-dashboard/
  dashboard.yml          ← widget grid definition (YAML)
  README.md
  widgets/               ← custom Jinja2 widget templates (.html.j2)
  assets/                ← static CSS / JS referenced by custom widgets
  vars/                  ← variable overrides per environment
```

## dashboard.yml reference

```yaml
name: My Dashboard
description: Optional subtitle
refresh: 30s             # polling interval (Ns | Nm | Nh)

layout:
  columns: 12            # CSS grid columns

vars:                    # available inside templates as {{ vars.key }}
  shelby_url: http://localhost:8080

widgets:
  - id: cpu              # unique within the dashboard
    title: CPU Usage
    type: gauge           # gauge | timeseries | counter | table | status | custom
    col: 1
    row: 1
    width: 4             # columns to span
    height: 2            # rows to span
    binding:
      value:             # binding key → available as data.value in the template
        pipeline: system-health
        series: cpu.percent
        limit: 2         # last N runs
    config:              # free-form; forwarded verbatim to the template as config.*
      min: 0
      max: 100
      unit: "%"
```

### Binding shapes

| Shape | Keys | Returns |
|-------|------|---------|
| Series | `pipeline`, `series`, `limit?`, `unit?` | `SeriesData` |
| Runs list | `pipeline`, `runs` (limit) | `dict` from `/api/pipelines/:slug/runs` |
| Static | `value` | the literal value |
| Expression | `"{{ pipelines['slug'].series('field') }}"` | evaluated at render time |

### SeriesData helpers (available in widget templates)

```
data.key.last       → float  (most recent value)
data.key.previous   → float
data.key.trend      → float  (last - previous)
data.key.max        → float
data.key.min        → float
data.key.avg        → float
data.key.points     → list[{t, v, run_id}]
```

## Built-in widgets

| type | template |
|------|----------|
| `gauge` | `widgets/gauge.html.j2` |
| `timeseries` | `widgets/timeseries.html.j2` |
| `counter` | `widgets/counter.html.j2` |
| `table` | `widgets/table.html.j2` |
| `status` | `widgets/status.html.j2` |

## Custom widgets

Set `type: custom` and point `template:` at any `.html.j2` file inside your
dashboard project folder. The template receives:

```jinja2
{{ data }}      {# resolved bindings — data.key is whatever the binding returned #}
{{ config }}    {# free-form dict from dashboard.yml #}
{{ meta }}      {# { title, id } #}
```

Output is inserted into a Shadow DOM cell, so your CSS cannot leak into or
out of the widget.

## Architecture

```
shelby serve            ← Go binary (unchanged)
  /api/pipelines/:slug/series
  /api/pipelines/:slug/runs
         │
         │  HTTP
         ▼
shelby-viz engine (Python / Jinja2)
  loader.py   ← parses dashboard.yml → DashboardDef
  data.py     ← PipelinesContext, PipelineProxy, SeriesData
  renderer.py ← resolves bindings + renders .html.j2 templates
  server.py   ← Flask dev server
  cli.py      ← Click CLI
         │
         ▼
browser
  Shadow DOM grid (CSS Grid host)
  runtime.js  ← polls /api/widget/:id, swaps shadow root on refresh
```

The Go engine is a pure data source. `shelby-viz` never touches `internal/`.

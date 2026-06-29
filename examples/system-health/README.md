# system-health dashboard

Example shelby-viz dashboard that visualises the `system-health` Shelby pipeline.

## Prerequisites

1. Register the pipeline and collect some history (run from repo root):

   ```bash
   shelby add viz/examples/system-health/pipeline.yml

   # Run a handful of times to build history
   for i in $(seq 1 10); do shelby run system-health; sleep 2; done

   # Start the Shelby API (keeps running + schedules pipeline every 15s)
   shelby serve
   ```

2. Install `shelby-viz`:

   ```bash
   cd viz/engine
   python3 -m venv .venv && .venv/bin/pip install -e .
   ```

## Run

```bash
# From viz/engine/
.venv/bin/shelby-viz serve ../examples/system-health --shelby http://localhost:8080
# open http://localhost:5000
```

## Widgets

| id | type | description |
|----|------|-------------|
| `cpu-gauge` | built-in `gauge` | Current CPU % with arc fill |
| `cpu-history` | built-in `timeseries` | Bar chart of last 60 readings |
| `mem-counter` | built-in `counter` | Memory used in GB with trend arrow |
| `mem-donut` | **custom** `widgets/memory-donut.html.j2` | SVG donut, shows used/total ratio |
| `pipeline-ok` | built-in `status` | Green/red based on whether pipeline ran |
| `run-log` | built-in `table` | Last 20 CPU readings with timestamps |

## Custom widget

`widgets/memory-donut.html.j2` is a self-contained Jinja2 template that produces
HTML + CSS inside a Shadow DOM cell. It receives:

- `data.used` — a `SeriesData` object from the `memory.used_gb` series
- `config.total` — total GB configured in `dashboard.yml`
- `meta.title` — widget title

Edit the template freely; `shelby-viz serve` re-renders on every request so
changes are visible on the next browser refresh.

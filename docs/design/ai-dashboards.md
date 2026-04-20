# AI-Generated Dashboards — Design Notes

Status: draft / brainstorm. Not implemented. Target is a **separate repository** (working name `shelby-viz`) that consumes Shelby pipeline outputs and renders dashboards whose widgets are generated on demand by an LLM.

## Vision

Shelby today extracts metrics and persists `RunRecord` history. Phase 2: a visualization layer where the user describes a widget in plain language ("show p95 latency over the last 24h for the API pipeline") and an LLM emits a self-contained HTML/CSS/JS widget wired to live Shelby data. Dashboards are grids of these widgets; pre-generated widgets seed a marketplace.

## Non-goals

- Not a Grafana replacement for ops teams — this is for the "Metrics as Code" user who already owns their pipelines.
- No static dashboard editor. Widgets are born from prompts, then pinned/tweaked.
- Not bundled inside the Go binary. Shelby stays a pure extraction engine.

## Contract with Shelby

The visualization layer only talks to Shelby over HTTP. `shelby serve` already exists; extend it with a read-only metrics API:

```
GET /api/pipelines                             → list of registrations + last-run status
GET /api/pipelines/:slug/runs?limit=N          → run history (lightweight)
GET /api/pipelines/:slug/runs/:run_id          → full RunRecord
GET /api/pipelines/:slug/series?field=<path>&range=24h&agg=none|avg|p95
                                               → normalized time series projected
                                                 from RunRecord.Output across runs
```

The `series` endpoint is the hot path. It walks recent runs, pulls `Output[field]` by dotted path, and returns a normalized payload. **Backend aggregates; client only renders** — rationale:

- We already have the records on disk. Projection is O(N) trivial in Go.
- Avoids shipping hundreds of raw `RunRecord` JSON blobs per widget.
- Isolates the AI-generated widget from parsing bugs and schema drift.

Derived metrics the backend doesn't provide (rolling windows, diffs, simple math) are computed client-side over the already-served series — cheap in JS at <5k points.

### Widget data contract

Every widget receives a payload matching a fixed schema. This is what gets injected into the LLM's system prompt as the "contract":

```json
{
  "kind": "timeseries | gauge | scalar | table | categorical",
  "unit": "ms | count | percent | bytes | ...",
  "points": [{ "t": "2026-04-20T10:00:00Z", "v": 123 }],
  "meta": {
    "pipeline": "api-latency",
    "field": "http.p95_ms",
    "range": "24h",
    "generated_at": "..."
  }
}
```

Strict schema = no hallucinated fields, no invented HTTP calls from the widget code.

## Layout survival

AI-generated HTML/CSS/JS must not break the host dashboard. Two hard rules:

1. **Shadow DOM per widget.** Each widget is a Web Component. Its CSS cannot leak; the parent's CSS cannot bleed in. A malformed `<div>` stays contained in its shadow root.
2. **The grid owns layout; the widget owns pixels inside its cell.** Dashboard parent is a strict CSS grid. The widget is told "fill 100% of your container" and nothing else about the outer world.

The LLM is never allowed to emit `<script src=...>` that fetches; all data arrives via a prop/attribute set by the host. Network is not in the widget's capability surface.

## Tech stack (to evaluate)

### Renderer / dashboard shell
- **SvelteKit or Next.js** — Next is the safe bet (ecosystem, deploy options, server actions for the LLM call). Svelte produces smaller runtime but smaller talent pool.
- **Tailwind v4** for the shell — the user already called this out. Pairs cleanly with Shadow-DOM widgets because Tailwind lives only on the shell, not inside the shadow root.
- **Web Components** via Lit or plain Custom Elements. Lit gives templating + reactivity with a ~5kb runtime; plain CE works but is verbose for anything interactive.

### Charting inside the widget
- Leave it to the LLM. Give it access to a small whitelisted set: `<canvas>`, SVG, and optionally a pinned version of **Chart.js** or **uPlot** loaded once by the host and exposed to the shadow root via `window.__shelby_chart`. uPlot is ~50x lighter if we lean into timeseries-heavy dashboards.
- Avoid D3 as default. Too easy for the LLM to hallucinate APIs; too big for a widget.

### LLM layer
- **Claude (Sonnet 4.6 or 4.7)** via Anthropic SDK for widget generation. Structured output + tool use keeps the emitted HTML inside a schema.
- Prompt caching on the system prompt (contract + safety rules + examples) is a big win — every widget generation reuses the same preamble.
- Widget source cached by `(prompt_hash, contract_hash)` so regenerating a pinned widget is free.

### Backend additions in Shelby
- Small: expose the endpoints above in `internal/server`. The `series` projection is new code; everything else is thin wrappers over `store.Runs` / `store.Run`.
- Auth: out of scope for v0 (local-first), but put the API behind a simple bearer token so remote mode is a flag-flip later.

### Persistence for the viz app
- Dashboards + widget definitions stored as JSON. Local-first by default (SQLite or flat files), with the same `~/.shelby/` style home so `shelby-viz` can coexist.

## Repo shape (proposed)

```
shelby-viz/
  apps/web/           # SvelteKit or Next shell
  packages/contract/  # shared TS types for the widget payload schema
  packages/widgets/   # seed widget components (Web Components) for the marketplace
  packages/llm/       # prompt templates + Claude client + caching layer
```

The `packages/contract` is the keystone: both Shelby's Go server and the TS frontend derive from it. Go side can generate from the JSON schema; TS imports directly.

## Open questions

- **Where does widget code execute first-time?** Generated in the server action (Next) so the client receives a pre-rendered string? Or streamed directly to the browser? Server-side gives us a chance to AST-validate before it ever hits the DOM.
- **Validation pipeline.** HTML parser + CSP-style allowlist before insertion. Reject widgets that reference `fetch`, `eval`, `new Function`, external URLs, etc.
- **Marketplace submission.** Community widgets still need the contract check. Same validator as generation.
- **Historical join semantics.** When a widget spans a pipeline YAML change (see `pipeline-versioning.md`), what happens to the series? Probably: breaks in series rendered as explicit gaps, tagged with the version boundary.

## Sequencing

1. Add `/api/pipelines/:slug/series` to `shelby serve`. Define the contract JSON schema in this repo.
2. Bootstrap `shelby-viz` separate repo. Static dashboard + hand-written Web Component widgets against the contract. No AI yet.
3. Layer in the LLM generator. Start with one widget kind (timeseries) and expand.
4. Marketplace + sharing.

## Reference

Companion doc: `pipeline-versioning.md` — the versioning work is a prerequisite for stable historical widgets.

from __future__ import annotations

from pathlib import Path
from typing import Any

import jinja2

from .data import PipelinesContext, MockPipelinesContext, SeriesData
from .loader import DashboardDef, WidgetDef

BUILTINS_DIR = Path(__file__).parent / "builtins"

BUILTIN_TYPES = {"gauge", "timeseries", "counter", "table", "status"}


def _resolve_binding(cfg: Any, pipelines: PipelinesContext) -> Any:
    """Convert one binding config value into live data.

    Binding config shapes supported in dashboard.yml:

        # Structured (recommended) — pull a time series
        pipeline: my-slug
        series: field.path
        limit: 50          # optional
        unit: ms           # optional

        # Structured — pull run list
        pipeline: my-slug
        runs: 20           # limit

        # Static scalar
        value: 42

        # Jinja2 expression string  "{{ pipelines['slug'].series('field') }}"
        # Evaluated with eval() — power-user shorthand, runs locally so acceptable.
    """
    if isinstance(cfg, dict):
        if "series" in cfg:
            return pipelines[cfg["pipeline"]].series(
                cfg["series"],
                limit=cfg.get("limit", 50),
                unit=cfg.get("unit", ""),
            )
        if "runs" in cfg:
            return pipelines[cfg["pipeline"]].runs(limit=cfg.get("runs", 20))
        if "value" in cfg:
            return cfg["value"]
        return cfg

    if isinstance(cfg, str) and cfg.strip().startswith("{{"):
        # Strip {{ }} and eval the inner expression
        inner = cfg.strip()[2:-2].strip()
        try:
            return eval(inner, {"__builtins__": {}}, {"pipelines": pipelines})  # noqa: S307
        except Exception as exc:
            return SeriesData(kind="timeseries", unit="", points=[], meta={"error": str(exc)})

    return cfg


def _filesizeformat(value: float, binary: bool = False) -> str:
    base = 1024 if binary else 1000
    value = float(value)
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if abs(value) < base:
            return f"{value:.1f} {unit}"
        value /= base
    return f"{value:.1f} PB"


class Renderer:
    """Resolves bindings from the Shelby API and renders Jinja2 widget templates."""

    def __init__(
        self,
        dashboard_path: Path,
        shelby_url: str,
        pipelines: PipelinesContext | MockPipelinesContext | None = None,
    ) -> None:
        self.dashboard_path = Path(dashboard_path).resolve()
        self.shelby_url = shelby_url
        self._pipelines = pipelines
        self._env = self._build_env()

    def _build_env(self) -> jinja2.Environment:
        loaders = [
            # User's dashboard project directory (custom widgets, vars, assets)
            jinja2.FileSystemLoader(str(self.dashboard_path)),
            # Built-in widgets and shell shipped with shelby-viz
            jinja2.FileSystemLoader(str(BUILTINS_DIR)),
        ]
        env = jinja2.Environment(
            loader=jinja2.ChoiceLoader(loaders),
            autoescape=jinja2.select_autoescape(["html"]),
        )
        env.filters["filesizeformat"] = _filesizeformat
        env.filters["ratio"] = lambda v, total: v / total if total else 0
        env.filters["abs"] = abs
        return env

    def render_widget(self, widget: WidgetDef) -> str:
        """Fetch live data and render one widget to an HTML string."""
        pipelines = self._pipelines if self._pipelines is not None else PipelinesContext(self.shelby_url)

        data: dict[str, Any] = {}
        for key, binding_cfg in widget.binding.items():
            data[key] = _resolve_binding(binding_cfg, pipelines)

        if widget.type == "custom":
            template_path = widget.template
        else:
            template_path = f"widgets/{widget.type}.html.j2"

        tmpl = self._env.get_template(template_path)
        return tmpl.render(
            data=data,
            config=widget.config,
            meta={"title": widget.title, "id": widget.id},
        )

    def render_dashboard(self, dashboard: DashboardDef) -> str:
        """Render the full dashboard page with all widgets inlined."""
        initial: dict[str, str] = {}
        for widget in dashboard.widgets:
            try:
                initial[widget.id] = self.render_widget(widget)
            except Exception as exc:
                initial[widget.id] = (
                    f'<style>:host{{display:flex;align-items:center;justify-content:center;height:100%}}'
                    f'.err{{color:#f44336;font-size:.75rem;padding:8px;text-align:center}}</style>'
                    f'<div class="err">⚠ {exc}</div>'
                )

        tmpl = self._env.get_template("shell/base.html")
        return tmpl.render(
            dashboard=dashboard,
            initial_renders=initial,
            shelby_url=self.shelby_url,
        )

from __future__ import annotations

import yaml
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass
class WidgetDef:
    id: str
    title: str
    type: str        # gauge | timeseries | counter | table | status | custom
    col: int
    row: int
    width: int
    height: int
    binding: dict[str, Any] = field(default_factory=dict)
    config: dict[str, Any] = field(default_factory=dict)
    template: str | None = None  # required when type == "custom"


@dataclass
class DashboardDef:
    name: str
    description: str
    refresh: int     # seconds
    columns: int
    widgets: list[WidgetDef]
    vars: dict[str, Any] = field(default_factory=dict)


def load_dashboard(path: str | Path) -> DashboardDef:
    path = Path(path)
    if path.is_dir():
        path = path / "dashboard.yml"

    raw = yaml.safe_load(path.read_text())

    widgets: list[WidgetDef] = []
    for w in raw.get("widgets", []):
        wtype = w.get("type", "custom")
        if wtype == "custom" and "template" not in w:
            raise ValueError(f"widget '{w.get('id')}' has type=custom but no template field")
        widgets.append(
            WidgetDef(
                id=w["id"],
                title=w.get("title", ""),
                type=wtype,
                col=w.get("col", 1),
                row=w.get("row", 1),
                width=w.get("width", 4),
                height=w.get("height", 2),
                binding=w.get("binding", {}),
                config=w.get("config", {}),
                template=w.get("template"),
            )
        )

    return DashboardDef(
        name=raw.get("name", "Dashboard"),
        description=raw.get("description", ""),
        refresh=_parse_duration(raw.get("refresh", "60s")),
        columns=raw.get("layout", {}).get("columns", 12),
        widgets=widgets,
        vars=raw.get("vars", {}),
    )


def _parse_duration(s: str | int) -> int:
    s = str(s).strip()
    if s.endswith("s"):
        return int(s[:-1])
    if s.endswith("m"):
        return int(s[:-1]) * 60
    if s.endswith("h"):
        return int(s[:-1]) * 3600
    return int(s)

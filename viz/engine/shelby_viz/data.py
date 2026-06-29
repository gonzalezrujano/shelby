from __future__ import annotations

from dataclasses import dataclass

import requests


@dataclass
class SeriesData:
    """Wraps a /api/pipelines/:slug/series payload with helper properties."""

    kind: str
    unit: str
    points: list[dict]
    meta: dict

    @property
    def last(self) -> float:
        return self.points[-1]["v"] if self.points else 0.0

    @property
    def previous(self) -> float:
        return self.points[-2]["v"] if len(self.points) >= 2 else 0.0

    @property
    def trend(self) -> float:
        return self.last - self.previous

    @property
    def max(self) -> float:
        return max((p["v"] for p in self.points), default=0.0)

    @property
    def min(self) -> float:
        return min((p["v"] for p in self.points), default=0.0)

    @property
    def avg(self) -> float:
        if not self.points:
            return 0.0
        return sum(p["v"] for p in self.points) / len(self.points)

    def __iter__(self):
        return iter(self.points)

    def __len__(self) -> int:
        return len(self.points)

    def __bool__(self) -> bool:
        return bool(self.points)


class PipelineProxy:
    def __init__(self, slug: str, base_url: str) -> None:
        self.slug = slug
        self._base = base_url.rstrip("/")

    def series(self, field: str, limit: int = 50, unit: str = "") -> SeriesData:
        url = f"{self._base}/api/pipelines/{self.slug}/series"
        params: dict = {"field": field, "limit": limit}
        if unit:
            params["unit"] = unit
        try:
            resp = requests.get(url, params=params, timeout=10)
            resp.raise_for_status()
            payload = resp.json()
        except Exception as exc:
            return SeriesData(kind="timeseries", unit=unit, points=[], meta={"error": str(exc)})
        return SeriesData(
            kind=payload.get("kind", "timeseries"),
            unit=payload.get("unit", unit),
            points=payload.get("points", []),
            meta=payload.get("meta", {}),
        )

    def runs(self, limit: int = 20) -> dict:
        url = f"{self._base}/api/pipelines/{self.slug}/runs"
        try:
            resp = requests.get(url, params={"limit": limit}, timeout=10)
            resp.raise_for_status()
            return resp.json()
        except Exception as exc:
            return {"runs": [], "error": str(exc)}


class PipelinesContext:
    """Proxy that resolves pipeline slugs to PipelineProxy instances.

    In binding expressions and widget templates use:
        pipelines['my-slug'].series('field.path')
    """

    def __init__(self, base_url: str) -> None:
        self._base = base_url

    def __getitem__(self, slug: str) -> PipelineProxy:
        return PipelineProxy(slug, self._base)

    # Allow pipelines('slug') call syntax as well
    def __call__(self, slug: str) -> PipelineProxy:
        return PipelineProxy(slug, self._base)

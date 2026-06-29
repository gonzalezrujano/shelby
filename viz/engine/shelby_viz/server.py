from __future__ import annotations

from pathlib import Path

from flask import Flask, Response, jsonify

from .loader import load_dashboard
from .renderer import Renderer

BUILTINS_DIR = Path(__file__).parent / "builtins"


def serve(dashboard_path: str, shelby_url: str, port: int = 5000, debug: bool = False) -> None:
    app = Flask(
        __name__,
        static_folder=str(BUILTINS_DIR / "shell"),
        static_url_path="/static",
    )

    _path = Path(dashboard_path).resolve()

    def _renderer() -> Renderer:
        return Renderer(_path, shelby_url)

    @app.route("/")
    def index():
        dashboard = load_dashboard(_path)
        html = _renderer().render_dashboard(dashboard)
        return Response(html, mimetype="text/html")

    @app.route("/api/widget/<widget_id>")
    def widget_api(widget_id: str):
        dashboard = load_dashboard(_path)
        widget = next((w for w in dashboard.widgets if w.id == widget_id), None)
        if widget is None:
            return jsonify({"error": "widget not found"}), 404
        try:
            html = _renderer().render_widget(widget)
        except Exception as exc:
            html = (
                f'<style>:host{{display:flex;align-items:center;justify-content:center;height:100%}}'
                f'.err{{color:#f44336;font-size:.75rem;padding:8px}}</style>'
                f'<div class="err">⚠ {exc}</div>'
            )
        return jsonify({"html": html})

    app.run(host="0.0.0.0", port=port, debug=debug, use_reloader=debug)

from __future__ import annotations

import threading
from pathlib import Path

from flask import Flask, Response, jsonify

from .data import MockPipelinesContext
from .loader import load_dashboard, load_sandbox
from .renderer import Renderer

BUILTINS_DIR = Path(__file__).parent / "builtins"

# --- live-reload broadcast ---------------------------------------------------

_reload_clients: list[threading.Event] = []
_reload_lock = threading.Lock()

_WATCH_EXTS = {".j2", ".html", ".yml", ".yaml", ".css", ".js"}


def _broadcast_reload() -> None:
    with _reload_lock:
        for ev in _reload_clients:
            ev.set()


def _start_watcher(watch_path: Path) -> None:
    from watchdog.events import FileSystemEventHandler
    from watchdog.observers import Observer

    class _Handler(FileSystemEventHandler):
        def _handle(self, path: str) -> None:
            if Path(path).suffix in _WATCH_EXTS:
                _broadcast_reload()

        def on_modified(self, event):
            if not event.is_directory:
                self._handle(event.src_path)

        def on_created(self, event):
            if not event.is_directory:
                self._handle(event.src_path)

        def on_moved(self, event):
            if not event.is_directory:
                self._handle(event.dest_path)

    observer = Observer()
    observer.schedule(_Handler(), str(watch_path), recursive=True)
    observer.daemon = True
    observer.start()


# --- SSE endpoint (shared by both serve functions) ---------------------------

def _add_livereload_route(app: Flask) -> None:
    @app.route("/api/livereload")
    def livereload():
        def stream():
            ev = threading.Event()
            with _reload_lock:
                _reload_clients.append(ev)
            try:
                yield "data: connected\n\n"
                while True:
                    triggered = ev.wait(timeout=25)
                    if triggered:
                        ev.clear()
                        yield "data: reload\n\n"
                    else:
                        yield ": heartbeat\n\n"
            finally:
                with _reload_lock:
                    _reload_clients.remove(ev)

        return Response(
            stream(),
            mimetype="text/event-stream",
            headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
        )


# --- public serve functions --------------------------------------------------

def serve(
    dashboard_path: str,
    shelby_url: str,
    port: int = 5000,
    debug: bool = False,
    live_reload: bool = False,
) -> None:
    app = Flask(
        __name__,
        static_folder=str(BUILTINS_DIR / "shell"),
        static_url_path="/static",
    )

    _path = Path(dashboard_path).resolve()

    if live_reload:
        _start_watcher(_path)
        _add_livereload_route(app)

    def _renderer() -> Renderer:
        return Renderer(_path, shelby_url)

    @app.route("/")
    def index():
        dashboard = load_dashboard(_path)
        html = _renderer().render_dashboard(dashboard, live_reload=live_reload)
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


def serve_sandbox(
    dashboard_path: str,
    port: int = 5000,
    debug: bool = False,
    live_reload: bool = False,
) -> None:
    app = Flask(
        __name__,
        static_folder=str(BUILTINS_DIR / "shell"),
        static_url_path="/static",
    )

    _path = Path(dashboard_path).resolve()
    mock_data = load_sandbox(_path)
    pipelines = MockPipelinesContext(mock_data)

    if live_reload:
        _start_watcher(_path)
        _add_livereload_route(app)

    def _renderer() -> Renderer:
        return Renderer(_path, "", pipelines=pipelines)

    @app.route("/")
    def index():
        dashboard = load_dashboard(_path)
        html = _renderer().render_dashboard(dashboard, live_reload=live_reload)
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

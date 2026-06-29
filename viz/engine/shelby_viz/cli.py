from __future__ import annotations

import sys
from pathlib import Path

import click


@click.group()
@click.version_option()
def cli() -> None:
    """shelby-viz — dashboard renderer for Shelby pipelines."""


@cli.command()
@click.argument("dashboard", type=click.Path(exists=True))
@click.option("--shelby", default="http://localhost:8080", show_default=True, help="Shelby server URL")
@click.option("--port", default=5000, show_default=True, help="Port to serve on")
@click.option("--debug", is_flag=True, help="Enable Flask debug mode + auto-reload")
def serve(dashboard: str, shelby: str, port: int, debug: bool) -> None:
    """Start a live dashboard server backed by a running Shelby instance."""
    from .server import serve as _serve

    click.echo(f"  shelby-viz · serving {dashboard}")
    click.echo(f"  shelby API → {shelby}")
    click.echo(f"  open       → http://localhost:{port}")
    _serve(dashboard, shelby, port, debug)


@cli.command()
@click.argument("dashboard", type=click.Path(exists=True))
@click.option("--shelby", default="http://localhost:8080", show_default=True, help="Shelby server URL")
@click.option("--out", default="dist", show_default=True, help="Output directory")
def build(dashboard: str, shelby: str, out: str) -> None:
    """Render a static HTML bundle from a dashboard (data snapshot at build time)."""
    from .loader import load_dashboard
    from .renderer import Renderer

    out_path = Path(out)
    out_path.mkdir(parents=True, exist_ok=True)

    dashboard_def = load_dashboard(dashboard)
    renderer = Renderer(Path(dashboard), shelby)
    html = renderer.render_dashboard(dashboard_def)

    dest = out_path / "index.html"
    dest.write_text(html)
    click.echo(f"  built → {dest}")


@cli.command()
@click.argument("name")
@click.option("--out", default=".", show_default=True, help="Parent directory for the new project")
def init(name: str, out: str) -> None:
    """Scaffold a new dashboard project folder."""
    target = Path(out) / name

    if target.exists():
        click.echo(f"  error: {target} already exists", err=True)
        sys.exit(1)

    for subdir in ("widgets", "assets", "vars"):
        (target / subdir).mkdir(parents=True)

    (target / "dashboard.yml").write_text(
        f"name: {name}\n"
        "description: ''\n"
        "refresh: 30s\n\n"
        "layout:\n"
        "  columns: 12\n\n"
        "vars:\n"
        "  shelby_url: http://localhost:8080\n\n"
        "widgets: []\n"
    )
    (target / "README.md").write_text(f"# {name}\n\nA shelby-viz dashboard.\n")

    click.echo(f"  created {target}/")
    click.echo(f"  → edit dashboard.yml to add widgets")
    click.echo(f"  → shelby-viz serve {target} --shelby http://localhost:8080")


@cli.command()
@click.argument("dashboard", type=click.Path(exists=True))
def validate(dashboard: str) -> None:
    """Validate dashboard.yml and all referenced widget templates."""
    from .loader import load_dashboard
    from .renderer import BUILTINS_DIR, Renderer

    import jinja2

    errors: list[str] = []

    try:
        d = load_dashboard(dashboard)
        click.echo(f"  ✓ dashboard.yml — {len(d.widgets)} widget(s)")
    except Exception as exc:
        click.echo(f"  ✗ dashboard.yml: {exc}", err=True)
        sys.exit(1)

    loader = jinja2.ChoiceLoader([
        jinja2.FileSystemLoader(str(Path(dashboard).resolve())),
        jinja2.FileSystemLoader(str(BUILTINS_DIR)),
    ])
    env = jinja2.Environment(loader=loader)

    for w in d.widgets:
        tpath = w.template if w.type == "custom" else f"widgets/{w.type}.html.j2"
        try:
            env.get_template(tpath)
            click.echo(f"  ✓ {w.id} ({w.type}) → {tpath}")
        except jinja2.TemplateNotFound:
            errors.append(f"  ✗ {w.id}: template not found: {tpath}")

    if errors:
        for e in errors:
            click.echo(e, err=True)
        sys.exit(1)

    click.echo("  ✓ validation passed")


if __name__ == "__main__":
    cli()

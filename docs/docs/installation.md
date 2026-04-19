---
sidebar_position: 2
---

# Installation & CLI

Requires **Go 1.26+**.

## Install From Source

```bash
git clone https://github.com/gonzalezrujano/shelby.git
cd shelby
go build -o shelby ./cmd/shelby
```

You can place the `shelby` binary in your `$PATH`.

## Run with Docker

You can build and run Shelby as a Docker container. The Dockerfile exposes the web UI on port `8080` and stores data in `/data` (configured by `SHELBY_HOME`).

```bash
# Build the image
docker build -t shelby .

# Run the serve daemon
docker run -d -p 8080:8080 -v shelby_data:/data shelby serve

# Run an ad-hoc command
docker run --rm -v $(pwd):/workspace -w /workspace shelby run ./disk.yaml
```

## CLI Usage

The `shelby` binary provides the following commands:

```text
shelby add <file.yaml>       register pipeline (path pointer; edits live)
shelby list                  table of registered pipelines with last-run status
shelby show <name|slug>      YAML + last-run summary
shelby rm   <name|slug>      unregister (drops run history)
shelby run  <name|file.yaml> run ad-hoc (no registration needed for files)
shelby logs <name|slug>      recent run history
shelby tui                   interactive terminal dashboard
shelby serve [-addr :8080]   scheduler daemon + web UI
```

**Store location:** `$SHELBY_HOME` or `~/.shelby`.

## Development Layout

If you want to poke around the source code:

```text
cmd/shelby/          main entrypoint
internal/engine      pipeline types + runner + ref resolver
internal/collectors  http_get, sys_stat, shell + parsers
internal/executors   script, conditional, aggregator
internal/runner      executor registry + run orchestration
internal/config      YAML loader
internal/store       registration + run history on disk
internal/cli         subcommand dispatch
internal/tui         bubbletea dashboard
internal/server      scheduler + HTTP web UI
```

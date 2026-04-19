---
sidebar_position: 5
---

# Contributing

Thanks for your interest in improving Shelby! This document describes how to propose changes and what to expect from the review process.

## Ways to contribute

- **Report bugs.** Open an issue with reproduction steps, expected vs. actual behaviour, Shelby version (`shelby --help`), and OS.
- **Propose features.** Open an issue first to discuss scope before sending a PR for anything larger than a small fix.
- **Improve docs.** README, inline docs, or example pipelines are always welcome.
- **Add examples.** Real-world YAML pipelines help other users; drop them under `examples/` with a short comment block explaining intent.

## Development setup

Requires **Go 1.26+**.

```bash
git clone https://github.com/gonzalezrujano/shelby.git
cd shelby
go build ./cmd/shelby
go test ./...
```

Project layout is documented in the documentation home under **Development Layout**.

## Making changes

1. **Fork** the repository and create a feature branch from `main`:
   ```bash
   git checkout -b fix/short-description
   ```
2. **Keep changes focused.** One logical change per PR. Unrelated refactors belong in separate commits or PRs.
3. **Write tests.** New executors, parsers, collectors, and bug fixes must ship with unit tests.
4. **Run the full checks** before pushing:
   ```bash
   go test ./...
   go vet ./...
   gofmt -l .     # must print nothing
   ```
5. **Update docs.** If you add a step type, parser, CLI flag, or YAML field, update the docs accordingly and add at least one example.

## Coding guidelines

- **Style:** standard `gofmt`. Package names lowercase, no underscores.
- **Errors:** return errors up; don't `log.Fatal` from library code. Wrap with `fmt.Errorf("context: %w", err)` so callers can `errors.Is` / `errors.As`.
- **Comments:** exported identifiers need a doc comment starting with their name.
- **No new dependencies** without discussion. Prefer the standard library.
- **Context propagation:** every long-running function takes `context.Context` as its first argument. Subprocesses go through `exec.CommandContext` with `cmd.Cancel` (SIGTERM) and a `WaitDelay` (SIGKILL fallback).
- **Security:** Shelby is read-only. Do not add steps that mutate external state without an explicit opt-in design and test coverage.

## Pull requests

- **Title:** imperative mood, e.g. `add json parser type coercion`.
- **Description:** explain the problem, the approach, and any tradeoffs. Link the issue it closes.
- **Size:** aim for < 400 lines of diff. Larger PRs should be split.
- **Checklist** before requesting review:
  - `go test ./...` passes
  - `go vet ./...` clean
  - `gofmt -l .` empty
  - Docs updated
  - New tests added for new behaviour

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0 that covers the project.

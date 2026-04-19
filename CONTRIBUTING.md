# Contributing to Shelby

Thanks for your interest in improving Shelby! This document describes how to
propose changes and what to expect from the review process.

## Code of Conduct

Be kind, assume good faith, and keep discussion technical. Harassment, personal
attacks, or sustained disruption of collaboration are not tolerated. Maintainers
may remove comments, commits, or contributors that violate these norms.

## Ways to contribute

- **Report bugs.** Open an issue with reproduction steps, expected vs. actual
  behaviour, Shelby version (`shelby --help` shows build info), and OS.
- **Propose features.** Open an issue first to discuss scope before sending a PR
  for anything larger than a small fix.
- **Improve docs.** README, inline docs, or example pipelines are always welcome.
- **Add examples.** Real-world YAML pipelines help other users; drop them under
  `examples/` with a short comment block explaining intent.

## Development setup

Requires Go 1.26+.

```bash
git clone https://github.com/<you>/shelby.git
cd shelby
go build ./cmd/shelby
go test ./...
```

Project layout is documented in the `README.md` under **Development**.

## Making changes

1. **Fork** the repository and create a feature branch from `main`:
   ```bash
   git checkout -b fix/short-description
   ```
2. **Keep changes focused.** One logical change per PR. Unrelated refactors
   belong in separate commits or PRs.
3. **Write tests.** New executors, parsers, collectors, and bug fixes must ship
   with unit tests. The existing test files under each `internal/<pkg>/` are
   good templates.
4. **Run the full checks** before pushing:
   ```bash
   go test ./...
   go vet ./...
   gofmt -l .     # must print nothing
   ```
5. **Update docs.** If you add a step type, parser, CLI flag, or YAML field,
   update `README.md` accordingly and add at least one example.

## Coding guidelines

- **Style:** standard `gofmt`. Package names lowercase, no underscores.
- **Errors:** return errors up; don't `log.Fatal` from library code. Wrap with
  `fmt.Errorf("context: %w", err)` so callers can `errors.Is` / `errors.As`.
- **Comments:** exported identifiers need a doc comment starting with their
  name. Keep unexported comments only when the *why* is non-obvious — not a
  restatement of what the code does.
- **No new dependencies** without discussion. Prefer the standard library.
- **Context propagation:** every long-running function takes `context.Context`
  as its first argument. Subprocesses go through `exec.CommandContext` with
  `cmd.Cancel` (SIGTERM) and a `WaitDelay` (SIGKILL fallback).
- **Security:** Shelby is read-only. Do not add steps that mutate external
  state without an explicit opt-in design and test coverage.

## Tests

- Unit tests live next to the code they cover (`foo.go` + `foo_test.go`).
- Integration fixtures (scripts, textfsm templates) live under
  `internal/<pkg>/testdata/`.
- Prefer deterministic tests. If a test touches time, the filesystem, or the
  network, use `t.TempDir()`, `httptest.NewServer`, or inject a fake clock.
- Keep tests fast. Anything over a couple seconds should be tagged
  `testing.Short()` friendly or guarded behind a build tag.

## Pull requests

- **Title:** imperative mood, e.g. `add json parser type coercion`.
- **Description:** explain the problem, the approach, and any tradeoffs.
  Link the issue it closes.
- **Size:** aim for < 400 lines of diff. Larger PRs should be split.
- **Checklist** before requesting review:
  - [ ] `go test ./...` passes
  - [ ] `go vet ./...` clean
  - [ ] `gofmt -l .` empty
  - [ ] Docs updated
  - [ ] New tests added for new behaviour

A maintainer will review within a reasonable timeframe. Expect iteration — we
may ask for renames, test additions, or scope changes. That's normal.

## Commit messages

Short imperative subject (<= 72 chars), optional blank line + body explaining
*why* the change is being made. Example:

```
aggregator: allow count op on arrays of maps

Previously count required `field:` even though it only needs cardinality.
collect() now increments the counter for any map element without needing to
extract a numeric field. Adds coverage for the no-field case.
```

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](LICENSE) that covers the project.

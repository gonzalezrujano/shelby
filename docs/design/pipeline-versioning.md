# Pipeline Versioning — Design Notes

Status: draft / brainstorm. Not implemented. Companion to the simple `update` command shipped in CLI.

## Problem

Today registrations are a pointer to an absolute YAML path. Edits to the source file take effect on the next run, and run history (`runs/<slug>/...`) only stores execution outcomes — not the YAML that produced them. Consequences:

- A run that failed two weeks ago can't be reproduced if the YAML was edited since.
- No diff between "what ran on Monday" and "what runs today."
- No safe rollback path. You'd have to revert the YAML by hand from git (assuming the file is even in git).
- `shelby update` swaps the path with no audit trail.

## Goal

Every recorded run is reproducible bit-for-bit. Users can list previous versions of a pipeline, diff them, and pin/rollback to one.

## Proposed model

### Snapshot store

New layout under `$SHELBY_HOME`:

```
pipelines/<slug>.json            # registration (current pointer + active version)
versions/<slug>/<hash>.yaml      # immutable YAML snapshots, content-addressed
runs/<slug>/<ts>_<runid>.json    # run record now embeds version_hash
```

- `<hash>` = sha256 of the canonicalized YAML bytes (trim trailing whitespace, normalize line endings — keep it simple, don't re-emit).
- Snapshots are write-once. Same content → same hash → no duplicate file.
- `Registration` gains: `ActiveVersion string`, `Versions []VersionMeta` (hash, captured_at, source_path_at_capture, optional note).

### When snapshots are taken

- `shelby add <file>` → captures v1.
- `shelby update <name> <file>` → if content hash differs from active, capture new version and bump `ActiveVersion`.
- `shelby run <name>` (registered runs) → before executing, compare on-disk YAML hash to `ActiveVersion`. If different (user edited file in place), capture a new version and update active. Run record references the version that actually ran.
- Scheduler tick → same as run.

This keeps "edits live" semantics from today, but every distinct content hash gets stamped.

### Run record changes

`RunRecord` adds `VersionHash string`. `logs` displays it; `show` shows the hash for the last run.

### Rollback

`shelby rollback <name|slug> <version|hash-prefix>`:
- Resolves to a stored snapshot.
- Writes that snapshot back to the registered Path (the user's working file). Confirmation prompt — this overwrites their working copy.
- Sets `ActiveVersion` to the rolled-back hash.
- Optional `--detach` flag to set `ActiveVersion` without touching the user's file (scheduler/run-by-name uses snapshot directly, ignores edits to working file until next `update` or explicit re-attach).

### History / diff

- `shelby history <name|slug>` → table: hash | captured_at | runs_using | note.
- `shelby diff <name|slug> <hashA> [<hashB>]` → unified diff between two snapshots (default B = active).
- `shelby show <name|slug> --version <hash>` → render that snapshot instead of the live file.

## Open questions

- **Pruning.** Snapshots accumulate forever. Need retention policy: keep last N, keep any referenced by an unpruned run record, allow `shelby gc`.
- **Detached vs. attached mode.** Does the scheduler always follow the working file, or does pinning a version freeze it? Lean: default attached (current behavior), `--pin` flag on update/rollback to detach.
- **External edits during a run.** Snapshot at run start; if file changes mid-execution, the run still references the start-of-run hash. No re-snapshot mid-run.
- **Rename.** Versioning doesn't fix the rename problem from `update`. Possible: `shelby rename <old-slug> <new-slug>` that moves `pipelines/`, `versions/`, and `runs/` directories atomically.
- **Notes / tags.** Worth tagging versions ("pre-prod", "v1.2") for human reference? Probably yes — cheap to add `Note string` to `VersionMeta`.
- **Cross-tool integration.** If the YAML file is already in git, are we duplicating history? Argument for: Shelby's snapshots are tied to actual runs (provenance), git history isn't. Argument against: noise. Recommend: snapshot regardless, document the difference.

## Non-goals (for v1 of versioning)

- Branching / merge of pipeline definitions.
- Multi-tenancy or per-user version ownership.
- Editing snapshots in place. Snapshots are immutable.
- Surfacing versions in the TUI/web dashboard. CLI-first; UI later.

## Migration

- Existing registrations have no `ActiveVersion`. On first interaction (any of: `run`, `show`, `update`, scheduler tick), capture current YAML as v1 retroactively. No flag day, no schema break — `Registration` fields are additive.
- Existing run records have no `VersionHash`. Leave blank; `logs` shows `-` for legacy entries.

## Cut lines

If we want to ship in stages:

1. Snapshot on add/update/run + record `VersionHash` on runs. Read-only history. No rollback yet. (Smallest useful slice.)
2. `shelby history` + `shelby diff`.
3. `shelby rollback` (attached mode only).
4. Pin / detach mode + `gc` + `rename`.

Each stage is a usable product on its own.

# `awg bootstrap` — production repo initialization

`awg bootstrap` is the **production** path for initializing Sensei on an
existing repository. It scaffolds Sensei if missing, runs deterministic architecture
extraction, optionally mines history candidates, then validates and builds the
graph — and prints a report.

```bash
awg bootstrap --repo <path>
```

It is **not** `awg cold-bootstrap`. `cold-bootstrap` is a *history-signal miner*
(it learns from a repo's scars — reverts, regression commits, PR review
comments). `awg bootstrap` initializes architectural awareness for a repo and
may call the coldsource miner as **one optional candidate stage**. Keep those
roles separate.

## What it does

1. **Scaffold if missing** — if `docs/awareness/` does not exist, runs the same
   scaffold as `awg init` (reusing `scaffoldProject`).
2. **Deterministic extraction → `docs/awareness/generated/`** (every fact is
   `assertion: inferred` — derived from code, not hand-authored):
   - **proto/API** → `contracts.yaml` (one Contract per gRPC service
     `uml.kind: Interface`, one per RPC `uml.kind: Operation`; read/write
     inferred from the method name, `unknown` when uncertain). `.proto` files are
     discovered automatically, excluding vendor/generated/build dirs.
   - **components** → `components.yaml` (one Component per package/unit from the
     repo layout; `service` if it has an entrypoint, else `module`).
   - **code symbols** → `source_symbols.yaml` / `source_edges.yaml` — only when a
     `docs/awareness/namespaces.yaml` registry exists (the annotation scanner
     needs one); otherwise reported as skipped.
   - **tests** → `tests.yaml` (Go `TestXxx` functions, plus file-level entries
     for other languages).
3. **Candidate extraction → `docs/awareness/candidates/`** (`status: candidate`,
   never auto-promoted; move under `architecture/` to accept):
   - **history candidates** via the coldsource miner — fully offline (echo
     drafter, local git only); no LLM or GitHub access required. Skipped with
     `--skip-history` or when the repo has no git history.
   - **pattern candidates** (minimum, grounded in the proto API shape): an
     all-read service suggests a `ReadOnlyProjection`; a service with write RPCs
     suggests a `GuardedMutationFlow`.
   - **misuse candidates** (minimum): files importing a storage driver directly
     suggest a `direct_storage_read` misuse — verify the access goes through the
     owner's port rather than stealing authority.
   - boundary candidates are honestly reported as *not implemented yet*.
4. **Gates** — `awg validate` (read-only) and an in-process `awg build` (compiles
   the graph; no running store required). Findings are reported, not fatal.
5. **Report** — components / contracts / operations / tests / source anchors /
   candidate patterns / candidate misuses / history candidates / validation
   findings / build status / next recommended actions.

## Flags

| flag | effect |
|---|---|
| `--repo <path>` | repository to bootstrap (default `.`) |
| `--skip-history` | do not run coldsource/history mining |
| `--skip-build` | run extraction + validate, but do not build |
| `--check` | compare generated output to committed files; exit non-zero if stale |
| `--dry-run` | print the report without writing generated/candidate files |

## Safety

`awg bootstrap` writes only under `docs/awareness/` (scaffold, `generated/`,
`candidates/`) and `.awg/config.yaml`. It never auto-promotes candidates, never
mutates hand-authored canonical files (beyond the initial scaffold), and makes no
edits outside the awareness/bootstrap paths.

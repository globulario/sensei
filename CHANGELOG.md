# Changelog

All notable changes to Sensei are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v1.0.0 — Multi-platform binaries

The first `1.0` release. Sensei is stable enough to build on: the CLI surface,
the awareness YAML schema, the gRPC/MCP tools, and the CI gate are committed
interfaces going forward.

### Highlights

- **Prebuilt binaries for every desktop platform.** Each release now ships
  standalone `sensei`, `awareness-graph`, and `awareness-mcp` binaries for
  `linux` and `darwin` × `amd64` and `arm64` — no Go toolchain required. The
  release workflow builds each target on a **native runner** (the tree-sitter
  parsers are cgo bindings and cannot be cross-compiled from one host).
- **macOS + arm64 fast path in CI.** `globulario/sensei-action` downloads the
  matching prebuilt binary for the runner's OS/arch (linux/darwin, amd64/arm64),
  including a platform-aware Oxigraph fetch, and falls back to a source build for
  any ref without prebuilt artifacts.
- **Windows** binaries are intentionally deferred: the enforcement hooks are
  bash and the end-to-end workflow is not yet validated there. The supervised
  Linux tarball bundles remain `linux-amd64`; the raw per-platform binaries cover
  macOS and arm64.

Since `0.2.x`: `sensei gate --sarif` (findings surface in GitHub code scanning),
the `--mode advisory|enforce|dry-run` alias, and `sensei demo`.

## v0.1.0 — Initial public release

First public, open-source release of the Sensei runtime under Apache-2.0.

Sensei gives an AI coding agent the architectural knowledge that normally lives only
in senior engineers' heads — invariants, failure modes, forbidden fixes, and
intent — as a queryable graph the agent consults **before** it edits and a CI
gate enforces **after**.

### Highlights

- **`sensei` CLI.** The command is `sensei` (formerly `awg`, an acronym of the
  old "Awareness Graph" name). The `awg` binary is still installed as a
  deprecated alias for one release — it prints a deprecation notice and forwards.
  Local state now lives in `.sensei/` (a pre-existing `.awg/` is still honored).
- **Local-first, you own the graph.** Your project's rules are YAML in your repo,
  compiled into a local Oxigraph store. No SaaS, no account, no source upload.
- **Consult before edit.** `sensei briefing`/`preflight` surface the invariants,
  contracts, and forbidden fixes that govern a file — in ~2 ms.
- **Enforce, not just inform.** `sensei gate --enforce` fails a CI check on a
  contract/forbidden-fix violation, with rule id + provenance; `--completeness`
  flags sibling call-sites a diff missed; per-repo `warn`/`block` policy.
- **Tool-agnostic.** A CLI + local gRPC server + MCP bridge (structured tools:
  briefing, impact, preflight, edit-check, resolve, query, metadata, propose).
  Drive it from Claude Code, Codex, Cursor, CI, or a plain shell.
- **Self-maintaining corpus.** `sensei onboard` proposes a starter graph from your
  repo for review; `sensei propose` writes typed feedback back into the graph.
- **133 universal meta-principles** across 8 categories, distilled from real
  production incidents, ship as a starting vocabulary.
- **Standalone build.** `scripts/build-awareness-graph-self.sh` builds the
  embedded seed from this repo alone — no external dependencies.

### Install

```bash
git clone https://github.com/globulario/sensei.git
cd sensei && ./scripts/install.sh
export PATH="$PWD/bin:$PATH"
```

Linux and macOS are validated paths; Go1.25+ required for source builds. A
prebuilt Linux amd64 local runtime bundle is attached to this release.

### Notes

Sensei was extracted from the [Globular](https://github.com/globulario) platform,
where its principles were validated against real production incidents. It now
runs standalone for any codebase.

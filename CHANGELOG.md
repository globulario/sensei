# Changelog

All notable changes to Sensei are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v1.1.0 — Windows binaries

- **`windows-amd64` prebuilt tarball.** Releases now include
  `sensei-windows-amd64.tar.gz` (`sensei.exe`, `awareness-graph.exe`,
  `awareness-mcp.exe`, `oxigraph.exe` + `setup.sh`), built natively on a
  `windows-latest` runner. Oxigraph's official Windows build ships in the bundle.
- **The CI Action runs on Windows.** `globulario/sensei-action` detects a Windows
  runner (Git Bash) and installs the Windows bundle; the gate runs under
  `shell: bash`.
- **Binary lookup is `.exe`-aware.** `sensei serve` finds `awareness-graph.exe` /
  `oxigraph.exe` next to itself on Windows.
- **Caveat — local enforcement needs a POSIX shell.** The pre-edit enforcement
  hooks and `setup.sh` are bash; on Windows run them under **Git Bash** or WSL.
  The compiled `sensei.exe` (`serve`, `build`, `gate`, queries) works natively.

## v1.0.0 — Multi-platform binaries

The first `1.0` release. Sensei is stable enough to build on: the CLI surface,
the awareness YAML schema, the gRPC/MCP tools, and the CI gate are committed
interfaces going forward.

### Highlights

- **One self-contained tarball per platform, Oxigraph included.** Each release
  ships a single `sensei-<platform>.tar.gz` for `linux-amd64`, `linux-arm64`, and
  `darwin-arm64` (Apple Silicon), containing every binary Sensei needs
  (`sensei`, `awareness-graph`, `awareness-mcp`, `oxigraph`, and the `awg` alias)
  in one `bin/` directory plus a platform-independent `setup.sh` that puts them
  on your `PATH`. No Go toolchain, no separate store download; bundling Oxigraph
  also makes the release immune to upstream rate-limits. The workflow builds each
  target on a **native runner** (the tree-sitter parsers are cgo bindings and
  cannot be cross-compiled from one host).
- **arm64 fast path in CI.** `globulario/sensei-action` downloads and unpacks the
  matching per-platform tarball for the runner's OS/arch — Oxigraph included —
  and falls back to a source build for any ref/platform without a prebuilt
  tarball.
- **macOS is Apple Silicon (arm64) only.** The Intel (`macos-13`) runner pool has
  long, unreliable queue times and current Mac dev machines are overwhelmingly
  arm64; Intel-mac users get the Action's source-build fallback.
- **Windows** binaries are intentionally deferred: the enforcement hooks are
  bash and the end-to-end workflow is not yet validated there.
- The machine-readable Globular service bundle
  (`awareness-graph_<version>_linux_amd64.tgz`, consumed by the Globular
  installer) is still published for `linux-amd64`. The previous loose per-binary
  release assets and the `awg-local` tarball are superseded by the per-platform
  tarball above.

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

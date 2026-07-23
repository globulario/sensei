# Changelog

All notable changes to Sensei are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v1.4.0 — transactional graph publication + GitHub App incubation

- **`sensei build --repo` no longer risks corrupting the live graph.** The
  scoped repository-refresh path previously concatenated compiled N-Triples
  directly into a SPARQL `INSERT DATA` block — a corpus valid for RDF ingestion
  could still be rejected by the SPARQL parser, and a delete-then-append
  window meant a mid-failure could leave the served store stale or corrupted.
  It now derives and validates the complete candidate generation offline,
  stages the replacement RDF through the Graph Store Protocol into an isolated
  named graph, and promotes it with one control-only SPARQL transaction
  (`ADD GRAPH ... TO DEFAULT; DROP GRAPH ...`) — raw RDF is never embedded in
  SPARQL text again. A cross-platform publication lock serializes concurrent
  local publishers, an ambiguous post-promotion response is resolved against
  the live whole-generation marker instead of guessed at, and a first
  publication into an empty store is now supported. Proven against a real
  Oxigraph instance (`-tags integration`), not just mocks. See
  `docs/architecture/repository-authority-and-graph-publication.md` for the
  full authority/publication model.
- **Governance self-audit repair.** Wired `required_tests` for 26 critical/high
  invariants that had a real proving test but no graph linkage, backfilled
  severity on 9 invariants that had none, and fixed `sensei repo-eval` to
  compare a self-only build's freshness against a self-only regeneration
  instead of a broader combined one (which was reading as false drift).
- **`sensei bootstrap` extracts Go library API candidates** — boundary and
  contract candidates inferred from exported declarations, written to
  `docs/awareness/generated/` alongside the existing candidate extractors.
- **Sensei GitHub App (first slice, incubating).** A standalone `github-app/`
  service: verifies webhook signatures, exchanges a signed JWT for
  installation tokens, handles `pull_request` open/reopen/synchronize events,
  posts a deterministic mechanical briefing as a sticky PR comment, and
  creates/updates one Check Run per head revision. Static-only inspection —
  no repository code execution. Governed architectural reasoning (invariants,
  contracts, proof obligations) is deferred to the next slice; see issue #111.
- **Phase 10 architectural-investigation groundwork** — deterministic HOW/WHY
  extraction (compiler-bound data-shape capture, Git-history-bound evidence)
  and the underlying investigation contract/validation model. Internal
  reasoning infrastructure for now; no new CLI surface yet.

## v1.3.0 — proportional rigor

- **`sensei rigor`** — reports the proportional-rigor class (and proof
  obligations owed) for a proposed change, classifying by the **governed
  surfaces** it touches rather than by filename. Surfaces are declared in
  `docs/rigor_classes.yaml` and bound to code through owned package prefixes.
  Advisory only: it names the obligations existing guards/CI enforce, it
  enforces nothing itself.
  - Classes: **A** semantic owner/authority/certification/identity · **B**
    evidence ingestion/admission/binding · **C** projection/transport/rendering
    · **D** cosmetic/explanatory local UI.
  - Fail-closed laws: effective rigor is the **strictest** class among every
    governed surface touched; a file owned by no surface is unclassified →
    **Class A**; an unknown class fails closed to A; a `--declared` class can
    only *raise* strictness, never downgrade contact with an A/B surface; and
    Class D still owes every repository-integrity gate (ownership,
    determinism, licensing, generated-artifact, build) — it only lightens
    *semantic* proof.
- **Control-panel polish** — actionable Unknown (owner-projected explanation,
  stable Kind), distinct non-positive states, and honest coverage (owned
  tallies only, no denominator → no fabricated percentage). Ships documented
  in the VS Code extension's own changelog.

## v1.2.1 — init wires every agent tool

- **`sensei init` sets up all your agent surfaces, not just Claude Code.** It now
  writes **AGENTS.md** (the cross-tool convention Codex/Cursor/others read), a
  **Cursor rule** (`.cursor/rules/sensei.mdc`), and — with `--mcp` — writes or
  merges the `sensei` server into **`.mcp.json`** (resolving the `awareness-mcp`
  path; never clobbering other servers). All additive and idempotent: existing
  rules are preserved and re-running never duplicates. Flags `--agents-md` /
  `--cursor` (default on), `--mcp` (opt-in).
- **General behavioral guidelines in the init snippets.** The CLAUDE.md /
  AGENTS.md / Cursor files also carry four general coding-discipline rules (Think
  before coding · Simplicity first · Surgical changes · Goal-driven — paraphrased
  and credited to Andrej Karpathy's observations), so `sensei init` gives you
  general agent discipline *and* your repo's architectural memory in one command.
- Domain-scoped `Metadata`/`Query` and the `edit-brief` push hook (from v1.2.0)
  are included.

## v1.2.0 — edit-brief push hook + domain scoping

- **`sensei edit-brief`** — a Claude Code PreToolUse *push* hook: it hands the
  agent the invariants, forbidden fixes, and failure modes that govern the file
  it's about to edit as `additionalContext`, so the agent gets the awareness
  unprompted and can't forget to consult Sensei. Completes the `edit-check`
  (advisory) / `edit-guard` (block) / `edit-brief` (push) triad; `sensei init`
  installs the `push-briefing.sh` hook and the quickstart recommends it alongside
  `edit-check-guard`.
- **Domain-scoped `Metadata` + `Query`.** `MetadataRequest`/`QueryRequest` accept
  a `domain`; per-class counts and by-class lists scope to a single repo/domain
  (reusing the pure, tested `InScope` core — with a no-cross-domain-leak test),
  and `MetadataResponse.available_domains` enumerates the selectable domains.
  This powers the VS Code extension's dashboard domain filter (scope the whole
  cockpit to one project).
- **Homebrew tap.** `brew install globulario/tap/sensei` installs the CLI,
  server, MCP bridge, and bundled Oxigraph on macOS (Apple Silicon) and Linux
  (amd64/arm64), with `brew upgrade` for updates. The tap
  ([globulario/homebrew-tap](https://github.com/globulario/homebrew-tap)) pins
  each release's per-platform tarball + SHA256 and CI-tests `brew install` on
  macOS and Linux.
- **One-line installers.** `install.sh` (Linux/macOS,
  `curl -fsSL …/install.sh | sh`) and `install.ps1` (Windows,
  `irm …/install.ps1 | iex`) detect the platform, download and checksum-verify
  the matching release tarball (via GitHub's `latest` redirect — no API, no rate
  limit), install the binaries onto PATH, and print the MCP config.
  `SENSEI_VERSION` / `SENSEI_PREFIX` override the release and target dir. A CI
  job (`installer-test.yml`) smoke-tests both across linux-amd64/arm64,
  darwin-arm64, and windows-amd64.

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

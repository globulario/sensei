# Changelog

All notable changes to Sensei are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v0.1.0 — Initial public release

First public, open-source release of the Sensei runtime under Apache-2.0.

Sensei gives an AI coding agent the architectural knowledge that normally lives only
in senior engineers' heads — invariants, failure modes, forbidden fixes, and
intent — as a queryable graph the agent consults **before** it edits and a CI
gate enforces **after**.

### Highlights

- **Local-first, you own the graph.** Your project's rules are YAML in your repo,
  compiled into a local Oxigraph store. No SaaS, no account, no source upload.
- **Consult before edit.** `awg briefing`/`preflight` surface the invariants,
  contracts, and forbidden fixes that govern a file — in ~2 ms.
- **Enforce, not just inform.** `awg gate --enforce` fails a CI check on a
  contract/forbidden-fix violation, with rule id + provenance; `--completeness`
  flags sibling call-sites a diff missed; per-repo `warn`/`block` policy.
- **Tool-agnostic.** A CLI + local gRPC server + MCP bridge (structured tools:
  briefing, impact, preflight, edit-check, resolve, query, metadata, propose).
  Drive it from Claude Code, Codex, Cursor, CI, or a plain shell.
- **Self-maintaining corpus.** `awg onboard` proposes a starter graph from your
  repo for review; `awg propose` writes typed feedback back into the graph.
- **133 universal meta-principles** across 8 categories, distilled from real
  production incidents, ship as a starting vocabulary.
- **Standalone build.** `scripts/build-awareness-graph-self.sh` builds the
  embedded seed from this repo alone — no external dependencies.

### Install

```bash
git clone https://github.com/globulario/sensei.git
cd awareness-graph && ./scripts/install.sh
export PATH="$PWD/bin:$PATH"
```

Linux and macOS are validated paths; Go 1.23+ required for source builds. A
prebuilt Linux amd64 local runtime bundle is attached to this release.

### Notes

Sensei was extracted from the [Globular](https://github.com/globulario) platform,
where its principles were validated against real production incidents. It now
runs standalone for any codebase.

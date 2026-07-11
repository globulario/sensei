# Changelog

## 0.1.1

- **Point users at the Sensei CLI.** The extension is a *client* of the `sensei`
  CLI — the dashboard now has a footer linking the project with one-line install
  commands (Homebrew / winget / curl), and the "server unreachable" message in
  the This File view explains how to install and start `sensei serve`.
- **Fix a confusing dashboard status after Reload/Rebuild.** When the graph is
  fresh but served by a locally-built (dev) server, the status no longer reads
  the self-contradictory "graph is current — authority disabled". It now states
  the actual reason (e.g. "✓ Reloaded — Dev build — provenance unstamped") and
  treats a current-but-unstamped graph as an advisory, not a red failure — the
  reload succeeded and the graph is usable; only the release-provenance stamp
  that governance/trust contexts need is absent.

## 0.1.0 — First public release

- **This File** view (activity bar): the invariants, failure modes, intent, risk
  class, forbidden fixes, and required tests that govern the file you're editing
  — read from your project's awareness graph in a single Preflight query, with
  explicit "visible absence" when nothing anchors to a file.
- **Project dashboard** (`Sensei: Open Project Dashboard`): an architect's
  cockpit — control banner with per-class totals and a trust signal, aspect
  navigation (invariants / failure modes / intents / patterns / files), and
  clickable detail via Resolve.
- **Candidate review & promotion** and **project review / architecture
  proposals**, with optional, opt-in local operations (rebuild/promote) that run
  in your working tree and surface a git diff — they never auto-commit.
- First-class **gRPC client** of the `sensei serve` backend (the same
  `AwarenessGraph` service the CLI uses); the contract is vendored and
  CI-checked against the canonical proto.

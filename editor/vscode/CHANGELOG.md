# Changelog

## 0.1.7

- **Ontology-aligned architectural control panel (Phase 9.5).** A read-only
  control panel — descriptor-driven navigation, snapshot-driven posture strip,
  index-driven attention queue, and an owner-honest artifact inspector — renders
  the awareness owner's projections verbatim and never classifies closure,
  severity, lifecycle, or applicability client-side.
- **One guarded mutation family.** Architect answers can be recorded and
  accepted/rejected through an explicit prepare → confirm → commit-once → refresh
  flow; refusals write nothing and are shown verbatim; the displayed lifecycle
  comes only from the refreshed owner, never optimistically. Governed promotion
  stays deferred and visibly unavailable.
- **Accessibility & layout closure.** Full keyboard operation (Enter/Space on
  every row, focus moves to the inspector on selection, an `aria-live` region
  announces guarded results); color is never the sole state carrier; the panel
  collapses to a single readable column at constrained widths.
- **Packaging.** AGPL-3.0-only throughout; the extension is validated + packaged
  on Ubuntu and Windows.

## 0.1.6

- **Adds Phase 2: Closure & Control.** The project dashboard now separates
  Project Awareness from architectural closure, dialogue, Evidence,
  convergence, and admission state.
- **Surfaces closure/control artifacts honestly.** Closure assessments,
  convergence sessions, admission decisions, and verification receipts can be
  selected from the workspace and rendered without treating missing artifacts as
  safety.
- **Adds Phase 2 graph browsing.** Architecture claims, open questions,
  architect answers, and Evidence probes are visible through explicit graph
  queries with non-authority / non-probative warnings.
- **Keeps local operations guarded.** Read-only status commands are allowlisted;
  no probe execution, test execution, automatic answers, automatic adjudication,
  automatic convergence loops, graph mutation, or source mutation is added.

## 0.1.5

- **Rebuild follows the selected domain.** The dashboard rebuild action now
  rebuilds the selected workspace domain as a single-repo graph, keeps combined
  rebuilds for "All domains", and blocks selected foreign domains instead of
  rebuilding the wrong graph.
- **Rebuild finds the local Sensei binary from nested workspaces.** Opening
  `editor/vscode` directly now still resolves the repo-root `bin/sensei`.
- **Rebuild failures show the actual command error.** Non-zero CLI exits surface
  stdout/stderr in the dashboard instead of only saying to check the log.

## 0.1.4

- **Counts stay scoped after Rebuild/Promote.** The post-operation banner refresh
  (and the before/after count summary) now query the same project domain as the
  main banner, so a Rebuild or candidate Promote no longer flashes the counts
  back to the whole multi-repo graph — they stay scoped to this repo.
- **Rebuild rebuilds THIS repo's view, not a flattened graph.** The dashboard's
  Rebuild (combined mode) now passes `--tag-by-repo`, so a multi-repo rebuild
  tags each repo's nodes with its own domain (from its git remote) and the graph
  stays filterable per repo — previously a Rebuild collapsed everything into one
  home domain and lost the per-repo view.

## 0.1.3

- **Dashboard defaults to the current project by default.** The banner counts,
  triple count, aspect lists, and the architecture score now scope to *this*
  repo's domain (derived from the git remote) whenever the graph carries it —
  re-evaluated on every load, so it self-corrects after the graph is reseeded
  (previously a one-time check could lock the view to the whole multi-repo
  graph). Pick "All domains" in the filter for the graph-wide view.

## 0.1.2

- **Robust domain default.** The dashboard no longer scopes the banner/lists to a
  git-remote-derived domain the graph doesn't actually key on (which showed
  near-empty counts and looked like Reload was broken) — it self-heals to the
  graph-wide view when the derived project domain isn't among the graph's
  domains; you can still pick any domain from the filter.
- **Domain filtering — scope the whole dashboard to a project.** On a graph that
  hosts more than one repo/domain, the "This File" view and node detail resolve
  against *this* project's domain (derived from the workspace git remote;
  `sensei.domain` overrides), and the **Project dashboard gains a domain filter**
  next to Reload/Rebuild — the control-banner per-class totals and the aspect
  lists scope to the selected project (defaulting to the current one), with an
  "All domains" option for the graph-wide view. Backed by domain-scoped
  `Metadata`/`Query` on the server; `triple_count` stays the raw store size.
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

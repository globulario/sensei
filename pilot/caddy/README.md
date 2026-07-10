# Pilot: repo-scoped graph for `github.com/caddyserver/caddy`

This directory is the **first repo-scoped/domain-scoped pilot** for Sensei. It
proves the end-to-end path for hosting a *foreign* repo's awareness in the same
Sensei instance as Globular's own, without cross-domain leakage:

```
cold-source candidate
  → human-reviewed / promoted rule (preserving provenance)
  → repo-scoped graph  (domain = github.com/caddyserver/caddy)
  → served by Sensei
  → briefing for a real Caddy file surfaces the rule
  → briefing for a Globular file NEVER surfaces it
```

## Why this is a *separate* graph

Pilot knowledge is **not** compiled into the embedded `awareness.nt` shipped
inside Globular. The embedded seed (`awg rebuild`) only scans
`docs/awareness/` + `docs/intent/` in the two product repos. This `pilot/` tree
lives outside those paths on purpose, so foreign-repo rules never ride inside
Globular's binary. The pilot demo builds a **separate authoritative artifact**
that combines the home graph with `pilot/caddy`, loads that artifact into an
isolated Oxigraph, and points Sensei at the matching runtime graph marker and
runtime transaction certification.

Isolation is enforced regardless, by `golang/server/scope.go`: a briefing scoped
to one domain returns that domain's nodes plus `shared` meta-principles, and
**never** another repo's rules. See the scope tests
(`golang/server/scope_test.go`, `golang/server/impact_scope_test.go`).

## Layout

```
pilot/caddy/
  candidates/         reviewed-but-not-promoted candidates (status: candidate)
  invariants.yaml     promoted, domain-tagged canonical rules (written by promote)
  README.md
  demo.sh             end-to-end demo (promote → build authoritative pilot graph → brief Caddy → brief Globular)
```

`candidates/` is skipped by the importer (by directory name), exactly like the
product repos' candidate queue — a candidate never influences the graph until it
is explicitly promoted.

## Provenance contract

Every promoted pilot rule MUST carry the receipt of how it was earned:

| Field | Meaning |
|-------|---------|
| `repo` | the foreign repo domain, e.g. `github.com/caddyserver/caddy` |
| `domain` | `repo` (foreign) — `shared` is reserved for portable meta-principles |
| `source_set` | namespace within the domain, e.g. `pilot/caddy` |
| `origin` | `coldsource` — drafted from existing repo signals |
| `provenance.bundle_id` | the cold-source bundle the candidate came from |
| `provenance.commit_range` | the git window that bundle scanned |
| `provenance.citations` | the PR review comments / commits supporting the rule |
| `provenance.review_label` | the human label at promotion (`load-bearing`, …) |

These compile to `aw:repo` / `aw:domain` / `aw:sourceSet` (read by the scope
filter) plus `aw:provenance*` literals (the audit trail — they grant no
authority, they only let any foreign rule be traced back to its evidence).

## Scope guardrails (do not change as part of running the pilot)

- Promote only **1–2** reviewed candidates. No bulk promotion, no auto-promotion.
- No new extractors. Candidates come from the existing cold-source fixtures.
- No active-graph mutation outside the explicit `awg promote --repo` path.
- Caddy rules MUST NOT appear in Globular briefings; Globular rules MUST NOT
  appear in Caddy briefings — unless they are `shared` meta-principles.

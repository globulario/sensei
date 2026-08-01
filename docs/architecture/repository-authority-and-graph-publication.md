# Repository Authority and Graph Publication

Status: Proposed implementation contract

## Purpose

Sensei derives a queryable architecture graph from repository-owned source. The graph is operationally important, but it is not the owner of architectural truth. This document defines the publication model used by local agents, GitHub Actions, the GitHub App, and future Globular-hosted workers.

## Authority model

The durable source world is the repository at an exact revision or working tree:

```text
source code and annotations
repository-owned awareness files
tests, decisions, contracts and policies
selected governance-pack digest
```

GitHub may version and distribute these files, but the files themselves are the governed source. Git history, issues, pull requests and incidents are evidence. They do not become current architectural truth without the repository's promotion path.

Oxigraph contains a compiled projection of that source world. It may be discarded and rebuilt. A triple's presence does not make its proposition authoritative.

## Generation identity

Every published graph is one complete generation bound to:

```text
repository or workspace identity
source revision or working-tree digest
source-set digest
compiler identity
governance-policy digest
graph semantic digest
triple count
```

The whole-generation marker and publication receipt identify the exact graph being served. A successful command does not merely mean that an HTTP mutation returned success. The live store must independently verify against the expected marker.

## Local publication protocol

A scoped repository refresh preserves other loaded domains while replacing one repository's solely owned slice.

```text
acquire the store publication lock
        ↓
read and count the committed default graph
        ↓
remove the target slice and old marker offline
        ↓
merge, canonicalize and validate the replacement generation
        ↓
PUT replacement RDF into an isolated named staging graph
        ↓
one SPARQL control transaction:
  delete target slice and old marker
  add staging graph to the default graph
  drop staging graph
        ↓
verify the live whole-generation marker
        ↓
publish marker and transaction receipts
```

RDF payload bytes are loaded through Graph Store Protocol as `application/n-triples`. They are never concatenated into `INSERT DATA` text. SPARQL carries control operations only.

The publication lock is keyed by normalized store endpoint, so separate local repository checkouts using the same Oxigraph service cannot concurrently derive generations from the same stale base.

## Failure semantics

Before promotion, the served default graph is untouched. A staging upload failure therefore preserves the active generation.

A rejected promotion transaction preserves the active generation and the staging graph is cleaned up. When the HTTP response is lost after the server may have committed, Sensei resolves the outcome from the live marker rather than blindly retrying an ambiguous mutation.

A build is successful only when:

1. the complete candidate N-Triples validates;
2. promotion completes or the live marker proves it completed;
3. the live graph matches the candidate marker and triple count;
4. local marker and transaction receipts are published.

## Runtime topologies

### Local agent

A developer may run a shared local Oxigraph service. Store-scoped locking serializes publishers. Agents call Sensei preflight, briefing and edit-check surfaces. They do not receive direct Oxigraph mutation access.

### GitHub Actions

Every verification run starts from repository files at exact base and head revisions and uses an isolated store. No CI run updates a developer's long-lived store.

### GitHub App

The App coordinates repository identity, workflow execution and check presentation. It does not own hidden architecture records and does not maintain a mutable architectural truth separate from the repository.

### Globular-hosted Sensei

Globular resolves an exact repository revision, builds or loads an immutable graph artifact, and serves it from an isolated worker. Graph artifacts may be cached by digest. Oxigraph processes remain replaceable.

## Explicit workspace composition

Cross-repository reasoning uses a declared workspace manifest containing exact repository identities and revisions. Sensei composes those immutable repository generations with the selected shared governance pack. It does not grow a permanent multi-repository graph through unbounded incremental mutation.

## Client discovery

The graph's `aw:repo` values describe domains present in the active generation. A later operational registry may retain configured repository visibility and degraded build status for editor clients. Such a registry is an operator index only and cannot establish graph or architectural authority.

## Non-negotiable boundaries

- Repository files remain the durable governed source.
- Candidate and historical evidence remain distinct from promoted truth.
- Oxigraph is a compiled serving projection.
- Agents never mutate Oxigraph directly.
- Raw RDF is never embedded into SPARQL update text.
- No publication is reported complete before live marker verification.
- CI and hosted workers reconstruct from exact repository revisions.
- Cross-repository composition is explicit and revision-bound.

The compact rule is:

> The repository remembers. The graph explains. Oxigraph serves. Sensei governs publication.

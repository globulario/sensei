# Docs Index

## Reference

- [CLI Reference](./cli-reference.md) — every `sensei` command and flag
- [API Reference](./api-reference.md) — the gRPC service + the MCP bridge tools
- [Meta-Principles](./meta-principles.md) — the 134-principle framework

## Agent-facing docs

- [Agent Usage](./agent-usage.md) — the pre-edit workflow + the write path
- [MCP Configuration](./mcp-config.md)
- [Agent Prompt Snippet](./agent-prompt-snippet.md)

## Design & rationale

- [Memory scope: the three bands](./design/memory-scope-bands.md) — what Sensei covers (durable, system-specific judgment) and why **agent + Sensei** covers the surface by construction
- [Memory correctness trade-off](./design/memory-correctness-tradeoff.md) — write-time vs read-time correctness, and why behavioral memory is forced to the write-time pole
- [Contract-first resolution](./design/contract-first-resolution.md) — why a contract must be explicit before a repair is judged legitimate
- [Contract Spine v1](./contract-spine-v1.md) — the band-2 model (Contracts, Invariants, Evidence) + the "Evidence is overloaded" modeling note
- [Architectural Closure v1](./design/architectural-closure-v1.md) — the Phase Two protocol, 10-dimensional task closure model (identity, scope, direction, authority, mutation, protection, epistemic state, proof, freshness, completion), and append-only ledger
- [Phase 8 Discovery](./design/phase8-discovery.md) — terminal task completion and owner-controlled closure slices

## Main operational docs

- [Repository README](../README.md)
- [Onboard an existing repo](./onboard-existing-repo.md) — repeatable recipe (+ `scripts/awg-init-repo.sh`) to stand up an awareness graph on a repo you didn't author it for: structural bootstrap, history mining, and the judgment passes
- [Initialize](./initialize.md) — local bootstrap, load, reload, and troubleshooting
- [Skill Ingestion](./skill-ingestion.md) — generate review-only Sensei candidates from external `SKILL.md` agent skill packs
- [Sensei Architect Skill](./sensei-architect-skill.md) — built-in project skill installed by `sensei init`
- [Local User Services](./initialize-user-services.md) — supervised local `systemd --user` runtime for Sensei and Oxigraph
- [Agent Admission Skill](../.agents/skills/sensei-admission/SKILL.md) — skill for exact change admission and working-tree scope verification
- [Agent Closure Skill](../.agents/skills/sensei-closure/SKILL.md) — skill for closing bounded architectural knowledge gaps, recording architect answers, and manual convergence advancement
- Operational smoke targets: `make oxigraph-health`, `make smoke-local`
- Integration tests: `go test -tags=integration ./...`

## Packaging

- [Globular Packaging](./globular-packaging.md)
- [Release Runbook](./release-runbook.md) — version bump + deploy + seed activation steps; read before every release

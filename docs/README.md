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

## Main operational docs

- [Repository README](../README.md)
- [Onboard an existing repo](./onboard-existing-repo.md) — repeatable recipe (+ `scripts/awg-init-repo.sh`) to stand up an awareness graph on a repo you didn't author it for: structural bootstrap, history mining, and the judgment passes
- [Initialize](./initialize.md) — local bootstrap, load, reload, and troubleshooting
- [Skill Ingestion](./skill-ingestion.md) — generate review-only Sensei candidates from external `SKILL.md` agent skill packs
- [Local User Services](./initialize-user-services.md) — supervised local `systemd --user` runtime for Sensei and Oxigraph
- Operational smoke targets: `make oxigraph-health`, `make smoke-local`
- Integration tests: `go test -tags=integration ./...`

## Packaging

- [Globular Packaging](./globular-packaging.md)
- [Release Runbook](./release-runbook.md) — version bump + deploy + seed activation steps; read before every release

# Docs Index

## Reference

- [CLI Reference](./cli-reference.md) — every `awg` command and flag
- [API Reference](./api-reference.md) — the gRPC service + the MCP bridge tools
- [Meta-Principles](./meta-principles.md) — the 133-principle framework

## Agent-facing docs

- [Agent Usage](./agent-usage.md) — the pre-edit workflow + the write path
- [MCP Configuration](./mcp-config.md)
- [Agent Prompt Snippet](./agent-prompt-snippet.md)

## Design & rationale

- [Contract Spine v1](./contract-spine-v1.md) — the band-2 model (Contracts, Invariants, Evidence) + the "Evidence is overloaded" modeling note

## Main operational docs

- [Repository README](../README.md)
- [Initialize](./initialize.md) — local bootstrap, load, reload, and troubleshooting
- [Skill Ingestion](./skill-ingestion.md) — generate review-only AWG candidates from external `SKILL.md` agent skill packs
- [Local User Services](./initialize-user-services.md) — supervised local `systemd --user` runtime for AWG and Oxigraph
- Operational smoke targets: `make oxigraph-health`, `make smoke-local`
- Integration tests: `go test -tags=integration ./...`

## Packaging

- [Globular Packaging](./globular-packaging.md)
- [Release Runbook](./release-runbook.md) — version bump + deploy + seed activation steps; read before every release

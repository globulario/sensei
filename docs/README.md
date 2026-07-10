# Docs Index

## Reference

- [CLI Reference](./cli-reference.md) — every `awg` command and flag
- [API Reference](./api-reference.md) — the gRPC service + the MCP bridge tools
- [Meta-Principles](./meta-principles.md) — the 98-principle framework

## Agent-facing docs

- [Agent Usage](./agent-usage.md) — the pre-edit workflow + the write path
- [MCP Configuration](./mcp-config.md)
- [Agent Prompt Snippet](./agent-prompt-snippet.md)

## Design & rationale

- [Memory scope: the three bands](./design/memory-scope-bands.md) — what AWG covers (durable, system-specific judgment) and why **agent + AWG** covers the surface by construction
- [Memory correctness trade-off](./design/memory-correctness-tradeoff.md) — write-time vs read-time correctness, and why behavioral memory is forced to the write-time pole
- [AWG Product Distribution Strategy](./design/product-distribution-strategy.md) — the working product memo: local runtime, managed governance distribution, Git integration boundary, packaging path, SKUs, and the concrete gaps to close
- [AWG Product Moat And Protection Strategy](./design/product-moat-and-protection-strategy.md) — where the advantage should live once the local runtime idea is known: corpus quality, trust/distribution, repair legitimacy credibility, and workflow fit
- [AWG Control Plane On Globular](./design/awg-control-plane-on-globular.md) — the concrete split between local AWG runtime and a Globular-backed control-plane micro-service for governance distribution, trust, channels, and entitlement
- [AWG Governed Repair Platform](./design/governed-repair-platform.md) — the minimum sellable product shape: managed governance, local project truth, and agent-independent governed repair
- [AWG Repair Guard: Minimum Sellable Product Spec](./design/awg-repair-guard-minimum-sellable-product-spec.md) — the first-edition operational spec: deployment model, boundaries, repair states, CI/editor behavior, and a 90-day slice
- [Local Truth, Managed Governance Architecture](./design/local-truth-managed-governance-architecture.md) — canonical governance on the vendor side, project truth on the client side, and local execution of governed repair queries
- [Signed Governance Pack Format And Activation](./design/signed-governance-pack-format-and-activation.md) — the first managed-governance interface: manifest, signature, local activation, combined-graph verification, and CLI flow
- [Trusted Publisher Bootstrap And Rotation](./design/trusted-publisher-bootstrap-and-rotation.md) — the client-side trust-root contract for managed governance: local trust store, bootstrap, key states, and rotation rules
- [Globular-Backed Governance Pack Publication](./design/globular-backed-governance-pack-publication.md) — the vendor-side control-plane contract: canonical pack build, signing, immutable publication, compatibility lookup, and explicit local fetch/activation
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

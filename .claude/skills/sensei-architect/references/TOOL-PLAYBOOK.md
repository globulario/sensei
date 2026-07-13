# Sensei Tool Playbook

Use the tool that answers the architectural question. Do not use briefing as a universal hammer.

## MCP Tools

Prefer MCP when available. Current standalone Sensei MCP tools:

- `awareness_metadata`
- `awareness_preflight`
- `awareness_briefing`
- `awareness_impact`
- `awareness_resolve`
- `awareness_query`
- `awareness_edit_check`
- `awareness_propose`

MCP tool names may be exposed by the host as `mcp__sensei.awareness_*` or a similar namespace. Use the host's available tool list.

## CLI Fallbacks

Use the `sensei` CLI when MCP is unavailable.

| Question | MCP | CLI fallback |
|---|---|---|
| Session health, authority, coverage, freshness | `awareness_metadata(domain=...)` | `sensei metadata --domain <repo-domain>` |
| Pre-edit risk and required actions | `awareness_preflight` | `sensei preflight --task "..." --file <path> --domain <repo-domain> --mode standard` |
| File-level compact context | `awareness_briefing` | `sensei briefing --file <path> --task "..." --depth agent_compact --domain <repo-domain>` |
| Typed nodes for one file | `awareness_impact` | `sensei impact --file <path> --domain <repo-domain> --json` |
| Resolve one node and provenance | `awareness_resolve` | `sensei resolve <class> <id> --domain <repo-domain>` |
| Typed browsing by file, id, class, or relation | `awareness_query` | `sensei query --mode by_class --class contract --limit 50 --domain <repo-domain>` |
| Proposed edit content warning | `awareness_edit_check` | `sensei edit-check --file <path> --content-file <file> --domain <repo-domain>` |
| Final diff gate | none in standalone MCP | `sensei gate --diff HEAD --domain <repo-domain> --enforce` |
| Corpus quality | none in standalone MCP | `sensei audit --check` |
| Repository readiness | none in standalone MCP | `sensei repo-eval --repo .` |
| Pattern recipes | none in standalone MCP | `sensei pattern-check <file>...` |
| Structural source rules | none in standalone MCP | `sensei source-check --source <dir> --patterns <patterns.yaml>` |
| Durable feedback | `awareness_propose` | `sensei propose --kind <kind> ...` |

## Tool Strategy

At session start:

1. Run metadata.
2. Determine repo/domain scope.
3. Report stale, thin, unknown, or unavailable awareness.

Before planning:

1. Run preflight with task and likely files.
2. Read `status`, `risk_class`, `confidence`, `coverage`, and `blind_spots`.
3. Branch depth by risk.

Before editing each file:

1. Run briefing.
2. Read `status`, `referenced_ids`, patterns, forbidden fixes, and tests.
3. Resolve high or critical referenced nodes.
4. Use impact when you need typed lists rather than prose.

For architecture browsing:

1. Query classes such as `contract`, `component`, `boundary`, `decision`, `evidence`, `design_pattern`, `implementation_pattern`, `pattern_misuse`, `meta_principle`, `invariant`, `failure_mode`, `forbidden_fix`, and `test`.
2. Use `related` on class-qualified ids to walk edges.
3. Resolve nodes before treating a short row as governing.

For proposed content:

1. Run edit-check on architecture-sensitive edited regions or full files.
2. Treat a clean result as "no detector fired", not proof of correctness.

At the end:

1. Run required tests and repository gates.
2. Run final Sensei gate when configured.
3. Propose durable lessons as candidates, never as active authority.

## Fallback When Awareness Is Sparse or Degraded

State the degraded condition. Then inspect:

- local `docs/awareness/` YAML
- source annotations
- tests and generated proof artifacts
- ADRs and intent documents
- git/PR history
- runtime evidence when relevant

For high-risk work, ask for user approval before mutation if the governing contract or authority path remains unverified.

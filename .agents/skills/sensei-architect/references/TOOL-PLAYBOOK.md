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
- `task_status`
- `advance_task`
- `task_briefing`

MCP tool names may be exposed by the host as `mcp__sensei.awareness_*` or a similar namespace. Use the host's available tool list.

## CLI Fallbacks

Use the `sensei` CLI when MCP is unavailable.

Resolve `<repo-domain>` from the active repository before using Sensei as decision support. In multi-domain graphs, pass the same domain to metadata, preflight, briefing, impact, query, resolve, edit-check, audit, and gate. Missing scope, unknown scope, zero scoped rows or triples, `EMPTY`, `DEGRADED`, backend errors, and `CANNOT VERIFY` are degraded awareness, not permission to proceed.

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
| Corpus quality | none in standalone MCP | `sensei audit --check --domain <repo-domain>` |
| Repository readiness | none in standalone MCP | `sensei repo-eval --repo .` |
| Bounded task awareness and admission | none in standalone MCP | `sensei prepare-change --repo . --repo-domain <repo-domain> --description "..." --mode modify --task-class <class> --risk-class <risk> --direction <direction> --graph-nt .sensei/project/graph.nt --file modify:<path>` |
| Compact task permission and next action | `task_status` | `sensei task-status --repo . --active --compact` |
| Task-aware file context | `task_briefing` | `sensei task-briefing --repo . --active --file <path>` |
| One bounded automatic Evidence step | `advance_task` | `sensei advance-task --repo . --active` |
| Pattern recipes | none in standalone MCP | `sensei pattern-check <file>...` |
| Structural source rules | none in standalone MCP | `sensei source-check --source <dir> --patterns <patterns.yaml>` |
| Durable feedback | `awareness_propose` | `sensei propose --kind <kind> ...` |

## Tool Strategy

At session start:

1. Run metadata.
2. Determine repo/domain scope.
3. Report stale, thin, unknown, unavailable, or unscoped awareness.

Before planning:

1. Run preflight with task and likely files.
2. Read `status`, `risk_class`, `confidence`, `coverage`, and `blind_spots`.
3. Branch depth by risk.

Before editing each file:

1. For an architecture-sensitive mutation with a project reconstruction bundle,
   run `prepare-change` for the exact task and files before mutation.
2. Run `task_briefing` for every planned file, then `task_status`.
3. Run `advance_task` while the one primary action is `run_static_evidence`.
4. Ask only the primary architect question when task status selects it.
5. Stop mutation on `waiting`, `refused`, stale, or `uncertifiable`.
6. Request mutation admission and edit only the admitted scope.
7. Verify admission after the edit.

For architecture browsing:

1. Query classes such as `contract`, `component`, `boundary`, `decision`, `evidence`, `design_pattern`, `implementation_pattern`, `pattern_misuse`, `architecture_claim`, `open_question`, `architect_answer`, `meta_principle`, `invariant`, `failure_mode`, `forbidden_fix`, and `test`.
2. Use `related` on class-qualified ids to walk edges.
3. Resolve nodes before treating a short row as governing.
4. Treat `architecture_claim` as explicit-query-only, non-authoritative hypothesis/proposition. Inspect its `epistemic_status`, evidence, dependencies, and invalidation condition before using it as context, and never present it as governed knowledge.
5. Treat `open_question` as explicit-query-only uncertainty, not truth or closure. Inspect the blocked claims and Evidence before summarizing it.
6. Treat `architect_answer` as an exact typed human statement, not observed behavior, Intent, Decision, Contract, or Invariant. `accepted_for_question` only resolves the question artifact; it does not promote architecture. Evidence pointers are unverified literals until converted to Evidence.

For bounded task awareness:

1. Use repository-level claims and adopted knowledge as the input, not as a
   declaration that the task is closed.
2. Let `prepare-change` generate OpenQuestions only for the exact task scope.
3. Preserve every generated question in the task bundle. Prioritize what you
   show the architect, but do not silently discard lower-priority unknowns.
4. An open question is an honest boundary of current knowledge. It blocks
   mutation only through the explicit admission policy for that task and risk.

For proposed content:

1. Run edit-check on architecture-sensitive edited regions or full files.
2. Treat a clean result as "no detector fired", not proof of correctness.

At the end:

1. Run required tests and repository gates.
2. Run final Sensei gate with `--domain <repo-domain>` when configured.
3. Propose durable lessons as candidates, never as active authority.

## Fallback When Awareness Is Sparse, Unscoped, or Degraded

State the degraded condition. Then inspect:

- local `docs/awareness/` YAML
- source annotations
- tests and generated proof artifacts
- ADRs and intent documents
- git/PR history
- runtime evidence when relevant

For high-risk work, ask for user approval before mutation if the governing contract or authority path remains unverified.

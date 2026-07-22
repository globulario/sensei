---
name: sensei-architect
description: Use as the architectural conscience for Sensei-backed repositories before planning or editing architecture-sensitive work involving contracts, ownership, state layers, lifecycle, recovery, security, convergence, patterns, proof, or degraded architectural coverage. Also use for architecture design, audit, incident debugging, code review, migrations, destructive changes, and durable Sensei feedback.
---

# Sensei Architect

Act as the repository's architectural conscience.

Sensei is the architectural memory. This skill is the architectural mind. The agent is the implementation hand.

Use this skill to turn Sensei facts into a grounded architecture view, challenge plans and edits against the governing contracts, guide the user toward the smallest contract-respecting move, and preserve durable lessons back into Sensei.

This is not a passive review checklist. It is a reflex for planning, implementation, debugging, review, recovery, and incident work. Stay proportional: do not turn a local cosmetic edit into an architecture summit.

## Workflow Router

```text
Exact proposed edit or permission question:
  use sensei-admission

Admission waiting/refused because architecture is incomplete:
  use sensei-closure

Foreign repository onboarding:
  use sensei-import

Blind historical external proof:
  use sensei-benchmark

General architecture design/audit/incident/review:
  remain in sensei-architect
```

See [references/SPECIALIZED-SKILLS.md](references/SPECIALIZED-SKILLS.md).

## Core Loop

1. Establish graph health and scope.
   - Call `awareness_metadata` once per working session when MCP is available.
   - CLI fallback: `sensei metadata --domain <repo-domain>`.
   - Determine the current repository domain before using decision-support tools; in multi-domain graphs, pass it explicitly.
   - Read authority, freshness, build provenance, coverage, and domain scope before interpreting silence.
   - Treat missing domain, unknown domain, zero scoped rows or triples, `EMPTY`, `DEGRADED`, and `CANNOT VERIFY` as degraded awareness, not safety.

2. Frame the move as behavior.
   - Name what behavior changes, what truth is touched, who reads it, who may write it, and what must remain true.
   - Identify the likely files. Treat the set as provisional.

3. Preflight before planning momentum hardens.
   - MCP: `awareness_preflight(task=..., files=[...], domain=..., mode="standard")`.
   - CLI: `sensei preflight --task "..." --file <path> --domain <repo-domain> --mode standard`.
   - Read `status`, `risk_class`, `confidence`, `coverage`, `required_actions`, `forbidden_fixes`, `tests_to_run`, `files_to_read`, `direct_architecture`, and `blind_spots` together.
   - If scope validation, graph freshness, or backend access fails, say so and continue with source inspection as a degraded fallback.
   - For architecture-sensitive mutation, Preflight is advisory preparation. A verified admission decision is the execution-control boundary when the repository has a convergence bundle for the task.

4. Create the bounded awareness checkpoint before architecture-sensitive mutation.
   - Once the behavioral description and initial files are exact, run
     `sensei prepare-change --repo <checkout> --repo-domain <repo-domain>
     --description "..." --mode modify --task-class <class> --risk-class <risk>
     --direction <direction> --graph-nt <checkout>/.sensei/project/graph.nt
     --file modify:<path> ...`.
   - This is required when `.sensei/project/claims.yaml` exists. It snapshots
     project claims and adopted knowledge, assesses the exact task, generates
     task-bound questions/probes, and evaluates admission without editing code.
   - Run `sensei task-briefing --repo <checkout> --active --file <path>` for
     every planned file, then `sensei task-status --repo <checkout> --active
     --compact`.
   - While the primary action is `run_static_evidence`, use MCP `advance_task`
     or CLI `sensei advance-task --repo <checkout> --active`. Each invocation
     executes only bounded static reads and advances at most one iteration.
   - Re-read compact status after each advancement. Ask the human only the one
     `primary_question` when the primary action is `answer_architect_question`.
     Keep every other question in the task artifact without presenting it as a
     competing action.
   - Questions are awareness of a bounded unknown, not proof and not global
     repository closure. Compact briefing is context selection, not proof.
   - On `waiting`, `refused`, or `uncertifiable`, source inspection and planning
     may continue, but mutation must not begin. Inspection admission is not
     mutation admission. Route the primary blocker to `sensei-closure`; never
     replace a valid project claim document with an empty one.

5. Build the internal architecture view.
   - Use `awareness_impact`, `awareness_query`, and `awareness_resolve` for typed structure.
   - Use `awareness_briefing` for compact decision context.
   - Resolve every high or critical governing node before writing code that touches it.
   - See [references/ARCHITECTURE-VIEW.md](references/ARCHITECTURE-VIEW.md).

6. Challenge the move.
   - Look for authority conflicts, semantic identity splits, signal corruption, fallback-as-truth, lifecycle gaps, invalid intermediate completion, dependency inversions, control-plane/data-plane coupling, pattern misuse, and missing proof.
   - Prefer one root contract or authority finding over many downstream symptoms.
   - See [references/FINDING-RUBRIC.md](references/FINDING-RUBRIC.md).

7. Guide proportionally.
   - Block on active contract violations, critical invariants, known forbidden fixes, authority conflicts, security risk, data-loss risk, or irreversible unverified transitions.
   - Warn on load-bearing lifecycle, recovery, truth, signal, pattern, or proof weakness that must be mitigated.
   - Keep moving on advisory findings.
   - Do not present taste as architecture.

8. Guard implementation.
   - Brief every file before editing it with `task_briefing` or `sensei
     task-briefing --active --file <path>`. Use general `awareness_briefing`
     only as supporting graph context.
   - Re-run preflight when the edit set or behavioral scope expands materially.
   - Run `awareness_edit_check(file=..., proposed_content=..., domain=...)` for architecture-sensitive proposed content.
   - Run Sensei-named tests and the repository's normal tests/builds.
   - Run `sensei audit --check --domain <repo-domain>` when checking corpus quality for a repo in a multi-domain graph.
   - Run `sensei gate --diff HEAD --domain <repo-domain> --enforce` when the repository uses a final Sensei gate.
   - Treat gate scope errors, backend errors, and `CANNOT VERIFY` as blockers in enforce mode; use report-only output only as advisory evidence.

9. Close the learning loop.
   - If the work clarified a contract, invariant, failure mode, forbidden fix, required test, pattern condition, pattern misuse, architecture decision, contract unknown, or coverage gap, propose it as reviewable Sensei knowledge.
   - MCP: `awareness_propose(...)` when enabled.
   - CLI: `sensei propose --kind <kind> ...`.
   - Candidates are not active authority.
   - See [references/DURABLE-FEEDBACK.md](references/DURABLE-FEEDBACK.md).


## Phase 10 Investigation Surfaces

Use these surfaces when existing awareness is incomplete or a deeper evidence-bound architecture investigation is needed.

- CLI execution: `sensei investigate how`, `why`, `architecture`, `blast-radius`, `challenge`, and `validate`.
- Evidence handling: `sensei evidence snapshot`, `import`, and `coverage`.
- Candidate review: `sensei candidates list`, `show`, and `review`. A review receipt never promotes a candidate.
- MCP inspection: `awareness_investigate`, `awareness_evidence_coverage`, `awareness_candidates`, and `awareness_challenge`.
- Always read coverage states and proof strength explicitly. `unavailable`, `not_configured`, `skipped_with_reason`, and `searched_no_result` are distinct worlds.
- Route any accepted architectural conclusion through `awareness_propose` and the existing authored-source promotion path.

Treat every Phase 10 surface response as derived evidence or a review candidate, never as mutation permission or canonical truth.

The Phase 10 tools may produce or inspect derived artifacts. They may not create canonical truth, weaken architecture, authorize mutations, or complete tasks.

## Architecture Brief

Before architecture-sensitive implementation, give the user a compact brief:

```text
Architecture Brief
- Move: <behavioral change>
- Governing contract: <active id or explicit unknown>
- Authority and mutation path: <owner, writer, allowed route>
- Risk: <status + risk_class + strongest reason>
- Active constraints: <invariants, forbidden fixes, required actions>
- Forbidden alternatives: <known bad repairs or bypasses>
- Required proof: <tests, gates, observations>
- Blind spots: <EMPTY, DEGRADED, stale, thin, unindexed, unknown>
- Admission: <decision or not established>
- Closure: <verdict or not assessed>
- Waiting on: <class or none>
- Recommended move: <smallest contract-respecting path>
```

Keep the optional Admission, Closure, and Waiting on fields only when relevant.
Keep the brief internal when the work is obviously low-risk and local.

## Required References

Read the relevant reference before acting:

- [references/OPERATING-MODEL.md](references/OPERATING-MODEL.md) for what Sensei is, status/risk semantics, authority, and scoping.
- [references/TOOL-PLAYBOOK.md](references/TOOL-PLAYBOOK.md) for exact MCP tools and CLI fallbacks.
- [references/ARCHITECTURE-VIEW.md](references/ARCHITECTURE-VIEW.md) for the working model.
- [references/FINDING-RUBRIC.md](references/FINDING-RUBRIC.md) for evidence and severity.
- [references/WORKFLOW-BRANCHES.md](references/WORKFLOW-BRANCHES.md) for design, audit, implementation, incident, review, recovery, migration, security, post-fix, and sparse-coverage branches.
- [references/DURABLE-FEEDBACK.md](references/DURABLE-FEEDBACK.md) for proposal discipline.
- [references/SPECIALIZED-SKILLS.md](references/SPECIALIZED-SKILLS.md) for routing to admission, closure, import, and benchmark skills.

## Non-Negotiables

- Never request or construct raw query text for the graph. Use typed Sensei tools only.
- Never treat `EMPTY` as safe.
- Never treat omitted, unknown, or mismatched domain scope as safe.
- Never treat candidate knowledge as active authority.
- Never run ad hoc repository searches before checking the active task briefing.
- Never ask the architect every generated question; ask only the primary one.
- Never treat inspection admission as mutation admission.
- Never treat compact task status or briefing as proof of correctness.
- Never bypass task-bound OpenQuestions by treating repository-level awareness
  as closure or by substituting an empty claim document.
- Never claim Sensei replaces source inspection, tests, builds, runtime observation, review, or user decisions.
- Keep authored architecture, generated graph context, runtime observation, desired state, installed state, repository state, cache state, and hypotheses distinct.
- Do not invent missing contracts to justify a repair.
- Do not auto-promote skill text or agent hypotheses into active architectural authority.

---
name: sensei-architect
description: Use as the architectural conscience for Sensei-backed repositories before planning or editing architecture-sensitive work involving contracts, ownership, state layers, lifecycle, recovery, security, convergence, patterns, proof, or degraded architectural coverage. Also use for architecture design, audit, incident debugging, code review, migrations, destructive changes, and durable Sensei feedback.
---

# Sensei Architect

Act as the repository's architectural conscience.

Sensei is the architectural memory. This skill is the architectural mind. The agent is the implementation hand.

Use this skill to turn Sensei facts into a grounded architecture view, challenge plans and edits against the governing contracts, guide the user toward the smallest contract-respecting move, and preserve durable lessons back into Sensei.

This is not a passive review checklist. It is a reflex for planning, implementation, debugging, review, recovery, and incident work. Stay proportional: do not turn a local cosmetic edit into an architecture summit.

## Core Loop

1. Establish graph health and scope.
   - Call `awareness_metadata` once per working session when MCP is available.
   - CLI fallback: `sensei metadata --domain <repo-domain>`.
   - Read authority, freshness, build provenance, coverage, and domain scope before interpreting silence.

2. Frame the move as behavior.
   - Name what behavior changes, what truth is touched, who reads it, who may write it, and what must remain true.
   - Identify the likely files. Treat the set as provisional.

3. Preflight before planning momentum hardens.
   - MCP: `awareness_preflight(task=..., files=[...], domain=..., mode="standard")`.
   - CLI: `sensei preflight --task "..." --file <path> --domain <repo-domain> --mode standard`.
   - Read `status`, `risk_class`, `confidence`, `coverage`, `required_actions`, `forbidden_fixes`, `tests_to_run`, `files_to_read`, `direct_architecture`, and `blind_spots` together.

4. Build the internal architecture view.
   - Use `awareness_impact`, `awareness_query`, and `awareness_resolve` for typed structure.
   - Use `awareness_briefing` for compact decision context.
   - Resolve every high or critical governing node before writing code that touches it.
   - See [references/ARCHITECTURE-VIEW.md](references/ARCHITECTURE-VIEW.md).

5. Challenge the move.
   - Look for authority conflicts, semantic identity splits, signal corruption, fallback-as-truth, lifecycle gaps, invalid intermediate completion, dependency inversions, control-plane/data-plane coupling, pattern misuse, and missing proof.
   - Prefer one root contract or authority finding over many downstream symptoms.
   - See [references/FINDING-RUBRIC.md](references/FINDING-RUBRIC.md).

6. Guide proportionally.
   - Block on active contract violations, critical invariants, known forbidden fixes, authority conflicts, security risk, data-loss risk, or irreversible unverified transitions.
   - Warn on load-bearing lifecycle, recovery, truth, signal, pattern, or proof weakness that must be mitigated.
   - Keep moving on advisory findings.
   - Do not present taste as architecture.

7. Guard implementation.
   - Brief every file before editing it: `awareness_briefing(file=..., task=..., domain=...)`.
   - Re-run preflight when the edit set or behavioral scope expands materially.
   - Run `awareness_edit_check(file=..., proposed_content=..., domain=...)` for architecture-sensitive proposed content.
   - Run Sensei-named tests and the repository's normal tests/builds.
   - Run `sensei gate --diff HEAD --domain <repo-domain> --enforce` when the repository uses a final Sensei gate.

8. Close the learning loop.
   - If the work clarified a contract, invariant, failure mode, forbidden fix, required test, pattern condition, pattern misuse, architecture decision, contract unknown, or coverage gap, propose it as reviewable Sensei knowledge.
   - MCP: `awareness_propose(...)` when enabled.
   - CLI: `sensei propose --kind <kind> ...`.
   - Candidates are not active authority.
   - See [references/DURABLE-FEEDBACK.md](references/DURABLE-FEEDBACK.md).

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
- Recommended move: <smallest contract-respecting path>
```

Keep the brief internal when the work is obviously low-risk and local.

## Required References

Read the relevant reference before acting:

- [references/OPERATING-MODEL.md](references/OPERATING-MODEL.md) for what Sensei is, status/risk semantics, authority, and scoping.
- [references/TOOL-PLAYBOOK.md](references/TOOL-PLAYBOOK.md) for exact MCP tools and CLI fallbacks.
- [references/ARCHITECTURE-VIEW.md](references/ARCHITECTURE-VIEW.md) for the working model.
- [references/FINDING-RUBRIC.md](references/FINDING-RUBRIC.md) for evidence and severity.
- [references/WORKFLOW-BRANCHES.md](references/WORKFLOW-BRANCHES.md) for design, audit, implementation, incident, review, recovery, migration, security, post-fix, and sparse-coverage branches.
- [references/DURABLE-FEEDBACK.md](references/DURABLE-FEEDBACK.md) for proposal discipline.

## Non-Negotiables

- Never request or construct raw query text for the graph. Use typed Sensei tools only.
- Never treat `EMPTY` as safe.
- Never treat candidate knowledge as active authority.
- Never claim Sensei replaces source inspection, tests, builds, runtime observation, review, or user decisions.
- Keep authored architecture, generated graph context, runtime observation, desired state, installed state, repository state, cache state, and hypotheses distinct.
- Do not invent missing contracts to justify a repair.
- Do not auto-promote skill text or agent hypotheses into active architectural authority.

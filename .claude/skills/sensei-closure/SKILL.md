---
name: sensei-closure
description: Use when a task says "close the architecture", "why is this blocked", "closure open", "answer the question", "record architect answer", "plan Evidence", "record probe result", "advance convergence", "stalled", "oscillating", "waiting on architect", or "waiting on Evidence". This skill closes bounded architectural knowledge gaps; it does not perform code mutation.
---

# Sensei Closure

Use this skill when admission or architectural review is blocked because the
bounded task is not architecturally closed.

Closure moves from a blocker to the smallest justified next action. It does not
turn conversation into proof automatically, mutate source, or promote knowledge.
The task-control executor may perform only registered static reads.

## When To Use

- Admission returns `waiting` because architecture is incomplete.
- A closure report has open blockers.
- The work needs an exact architect answer recorded.
- The work needs an EvidenceProbe planned or an externally reported result
  recorded.
- A convergence session needs exactly one deterministic advancement.

## Core Loop

1. Inspect compact current state with `sensei task-status --active --compact`.
   Use `sensei convergence-status --session <session.yaml> --verify-bundle
   <dir>` only for expanded convergence diagnostics.
2. While the primary action is `run_static_evidence`, run `sensei advance-task
   --active`. Re-read compact status after each one-step advancement.
3. When needed, assess closure from explicit offline inputs:
   `sensei assess-closure --request <closure-request.yaml> --claims <claims.yaml> --graph-nt <graph.nt> --repo <checkout> --format yaml`.
4. Classify the missing action: architect, evidence, governance, or mechanical
   repair.
5. Generate questions only when needed:
   `sensei generate-questions --closure <closure.yaml> --claims <claims.yaml> --graph-nt <awareness.nt> --created-at <RFC3339> --output <dialogue.yaml>`.
6. Record only the primary architect answer after the architect supplies it:
   `sensei record-answer --dialogue <dialogue.yaml> --question <id> --statement <text> --classification <type> --author-role <role> --recorded-at <RFC3339> --output <dialogue.yaml>`.
7. Adjudicate separately:
   `sensei adjudicate-answer --dialogue <dialogue.yaml> --answer <id> --status <status> --output <dialogue.yaml>`.
8. Plan probes when no task-local plan exists:
   `sensei plan-probes --closure <closure.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --graph-nt <awareness.nt> --output <probes.yaml>`.
9. Record non-static or externally produced probe results only after an external actor reports
   them:
   `sensei record-probe-result --probes <probes.yaml> --probe <id> --result-status <status> --evidence-status <status> --evidence-freshness <freshness> --observed-at <RFC3339> --executed-by <actor> --output <results.yaml>`.
10. Advance exactly one iteration for manual closure inputs:
   `sensei advance-convergence --closure-request <request.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --evidence-state <state.yaml> --graph-nt <graph.nt> --repo <checkout> --question-created-at <RFC3339> --output-dir <dir>`.
11. Inspect status again. Return to `sensei-admission` only after explicit inputs
    changed.

## Compact Output

```text
Closure Status
- Verdict: <closed | open | stalled | oscillating | budget_exhausted>
- Waiting on: <architect | evidence | governance | mechanical_repair | none>
- Next action: <one action>
- Artifact touched: <dialogue | probes | evidence-state | convergence bundle | none>
- Mutation allowed: no
```

## Routing

- Exact mutation after closure changes: return to `sensei-admission`.
- Foreign repository onboarding: use `sensei-import`.
- Blind historical external proof: use `sensei-benchmark`.
- General architecture audit or incident reasoning: use `sensei-architect`.

## Non-Negotiables

- ArchitectAnswer is not Evidence.
- Answer is non-probative until separately adjudicated and supported where
  required.
- Only `advance-task` may execute registered `static_read` probes. Runtime,
  command, network, secret, mutation, and destructive probes remain plans only.
- Record only externally reported probe results.
- Advance exactly one convergence iteration per closure action.
- Surface stall, oscillation, budget exhaustion, unavailable evidence, and stale
  inputs.
- Do not mutate source code from this skill.
- Do not promote candidates, claims, answers, or probe results.
- Do not claim bounded task closure means full repository understanding.
- MCP supports task status, task briefing, and controlled task advancement.
  Use CLI fallback for the lower-level manual closure commands.

## References

- [references/CLOSURE-MODEL.md](references/CLOSURE-MODEL.md)
- [references/DIALOGUE-WORKFLOW.md](references/DIALOGUE-WORKFLOW.md)
- [references/EVIDENCE-PROBE-WORKFLOW.md](references/EVIDENCE-PROBE-WORKFLOW.md)
- [references/CONVERGENCE-WORKFLOW.md](references/CONVERGENCE-WORKFLOW.md)
- [references/HONESTY-BOUNDARIES.md](references/HONESTY-BOUNDARIES.md)

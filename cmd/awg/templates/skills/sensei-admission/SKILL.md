---
name: sensei-admission
description: Use for architecture-sensitive implementation when the agent asks "may I change this", "safe to modify", "admit this task", "permission to edit", "change envelope", "verify admission", "scope compliance", or needs admission before mutation. This skill decides whether one exact proposed change may be attempted; it does not prove correctness.
---

# Sensei Admission

Use this skill to decide whether one exact architecture-sensitive mutation is
permitted inside a bounded convergence bundle.

Admission is permission to attempt. It is not proof of correctness, not a
substitute for tests or review, and not a promotion path for candidates.

## When To Use

- The user asks whether an exact edit is allowed.
- A task has a convergence bundle and mutation is about to start.
- A diff must be checked against an existing admission envelope.
- `sensei-architect` routes an architecture-sensitive implementation here.

Do not use this skill for broad design, repository import, blind external proof,
or closure work that is still waiting on questions or evidence.

## Core Loop

1. Identify the exact move: files, allowed operation, expected behavior, and
   known proof still pending.
2. If no convergence bundle exists and the repository has
   `.sensei/project/claims.yaml`, create the awareness checkpoint first:
   `sensei prepare-change --repo <checkout> --repo-domain <domain>
   --description "..." --mode modify --task-class <class> --risk-class <risk>
   --direction <direction> --graph-nt <checkout>/.sensei/project/graph.nt
   --file modify:<path> ...`.
   - This command runs one deterministic convergence iteration and admission
     evaluation without mutating source.
   - Brief each planned file with `sensei task-briefing --repo <checkout>
     --active --file <path>`, then inspect `sensei task-status --repo <checkout>
     --active --compact`.
   - Run `sensei advance-task --repo <checkout> --active` while bounded static
     Evidence is the primary action. Ask only the primary architect question
     when status selects it.
   - On `waiting`, route the primary unresolved action to `sensei-closure`. Do
     not invent an empty bundle, ask every generated question, or bypass it.
3. Prefer MCP `admit_change` when an explicit bundle is already available:
   `bundle_dir`, `request_path`, `graph_nt`, `repo`, optional `policy`, optional
   `detail`.
4. CLI fallback:
   `sensei admit-change --bundle <dir> --request <request.yaml> --graph-nt <graph.nt> --repo <checkout> --output <decision.yaml> --format yaml`.
5. Interpret the decision:
   - `admitted`: edit only inside the envelope.
   - `admitted_with_conditions`: edit only inside the envelope and keep the
     conditions visible as pending proof.
   - `waiting`, `refused`, `uncertifiable`: stop mutation and route to
     `sensei-closure` or the user.
6. During editing, stop if the needed file set, behavior, or authority surface
   expands beyond the envelope.
7. Verify the diff with MCP `verify_admission`:
   `decision_path`, `bundle_dir`, `repo`, optional `detail`.
8. CLI fallback:
   `sensei verify-admission --decision <decision.yaml> --bundle <dir> --repo <checkout> --output <verification.yaml> --format yaml`.
9. Inspect receipts when needed:
   `sensei admission-status --decision <decision.yaml> --verification <verification.yaml>`.

## Compact Output

```text
Admission Brief
- Move: <exact action>
- Decision: <admitted | admitted_with_conditions | waiting | refused | uncertifiable>
- Envelope: <allowed files and operations>
- Conditions: <pending proof or none>
- Waiting on: <architect | evidence | governance | none>
- Stop rule: <what would require new admission>
```

```text
Admission Verification
- Scope: <compliant | scope_violated | unavailable>
- Extra tracked paths: <paths or none>
- Extra untracked paths: <paths or none>
- Correctness: not certified
- Remaining proof: <tests, review, observations>
```

## Routing

- Architecture-sensitive mutation: stay here.
- Waiting or refused because architecture is incomplete: use `sensei-closure`.
- Foreign repository onboarding: use `sensei-import`.
- Blind historical external proof: use `sensei-benchmark`.
- General architecture audit or incident reasoning: use `sensei-architect`.

## Non-Negotiables

- Never call admission proof of correctness.
- Never call scope compliance correctness.
- Never broaden the envelope while editing.
- Never hide untracked files from verification.
- Never mutate on `waiting`, `refused`, stale, or `uncertifiable`.
- Never execute ad hoc probes; only `advance-task` may run its closed static-read registry.
- Never record or invent architect answers from this skill.
- Never treat inspection admission as mutation admission.
- Never interpret compact status or briefing as proof of correctness.
- Never use Preflight, Gate, or EditCheck as admission substitutes.
- Never invent a bundle, digest, decision, or verification receipt.
- Never treat candidate knowledge as active authority.
- Never treat repository reconstruction as task closure. Its claims and unknowns
  are inputs to the bounded awareness checkpoint.

## References

- [references/ADMISSION-MODEL.md](references/ADMISSION-MODEL.md)
- [references/AGENT-WORKFLOW.md](references/AGENT-WORKFLOW.md)
- [references/DECISION-SEMANTICS.md](references/DECISION-SEMANTICS.md)
- [references/DIFF-VERIFICATION.md](references/DIFF-VERIFICATION.md)

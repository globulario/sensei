---
name: sensei-benchmark
description: Use only for explicit blind historical external proof or cold-start evaluation of Sensei on an external repository such as Gin, Caddy, or etcd. Trigger on requests for blind evaluation, sealed oracle, benchmark freezing, reconstruction, intervention ledger, oracle evaluation, false-green review, or categorical benchmark reporting. Do not use for ordinary import, audit, edit, performance benchmarks, or unit-test benchmarks.
---

# Sensei Benchmark

Use this skill only when the user explicitly asks to evaluate Sensei itself with
a blind historical external task.

The goal is not to make Sensei look successful. A precise open verdict is
acceptable. A hidden critical false green is not.

## When To Use

- The user asks for a blind external generalization pilot.
- The task involves a historical external repository with a sealed future
  oracle.
- The workflow needs curation, freeze, blind reconstruction, intervention
  accounting, oracle reveal, and categorical evaluation.

Do not use this skill for ordinary repository import, code edits, architecture
review, performance benchmarking, or running unit tests.

## Core Loop

1. Require explicit benchmark intent.
2. Enforce role separation: curator, blind runner, and evaluator are separate
   roles even if one agent is coordinating receipts.
3. Curate historical candidates from real repository history. Do not invent a
   task. Stop for human selection before freezing.
4. Freeze:
   `sensei benchmark-freeze --task <task.yaml> --source-repo <repo> --oracle <sealed-oracle.yaml> --output-dir <workspace> --format yaml`.
5. Reconstruct blind:
   `sensei benchmark-reconstruct --workspace <workspace> --question-created-at <RFC3339> --format yaml`.
6. Review generated questions before oracle reveal.
7. Account for every intervention in an intervention ledger.
8. Freeze the final blind state before evaluation.
9. Evaluate:
   `sensei benchmark-evaluate --workspace <workspace> --oracle <sealed-oracle.yaml> --question-review <review.yaml> --oracle-mapping <mapping.yaml> --output <report.yaml> --format yaml`.
10. Inspect compact status:
    `sensei benchmark-status --workspace <workspace> --report <report.yaml>`.
11. Report critical false greens first.

## Compact Output

```text
Benchmark Status
- Intent: explicit blind external proof
- Repository: <domain>
- Candidate: <selected historical task or awaiting human selection>
- Phase: <curation | frozen | reconstructed | reviewed | evaluated>
- Oracle: <sealed | revealed after review>
- Interventions: <count and ledger path>
- Verdict: <demonstrated | open | failed | critical_false_green>
```

## Routing

- Foreign repository onboarding without sealed oracle: use `sensei-import`.
- Exact architecture-sensitive mutation: use `sensei-admission`.
- Blocked bounded architecture knowledge: use `sensei-closure`.
- General architecture design or incident review: use `sensei-architect`.

## Non-Negotiables

- Require explicit benchmark intent.
- Keep curator, blind runner, and evaluator responsibilities separate.
- Do not leak oracle facts into blind reconstruction.
- Do not invent tasks, source changes, or future fixes.
- Record every intervention.
- Surface critical false green first.
- Never execute agents, run Tests, use external network access, or mutate source
  from this skill.
- Do not import external repository rules into active Sensei.
- Do not treat the oracle as automatic correctness authority.

## References

- [references/BLIND-EVALUATION.md](references/BLIND-EVALUATION.md)
- [references/TASK-CURATION.md](references/TASK-CURATION.md)
- [references/HUMAN-DIRECTION.md](references/HUMAN-DIRECTION.md)
- [references/ORACLE-EVALUATION.md](references/ORACLE-EVALUATION.md)
- [references/FALSE-GREEN-RUBRIC.md](references/FALSE-GREEN-RUBRIC.md)

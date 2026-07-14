# Sensei Closure Skill

This managed skill guides an agent when a bounded task is blocked by incomplete
architectural knowledge. It records exact answers, plans evidence probes,
records externally supplied probe results, and advances one convergence
iteration when explicit inputs changed.

Primary entry point:

- `SKILL.md`

Reference files:

- `references/CLOSURE-MODEL.md`
- `references/DIALOGUE-WORKFLOW.md`
- `references/EVIDENCE-PROBE-WORKFLOW.md`
- `references/CONVERGENCE-WORKFLOW.md`
- `references/HONESTY-BOUNDARIES.md`

It is installed by `sensei init` with managed manifests. Local edits are
preserved unless `--skills-force` is used.

Skill prose is not architectural authority. It teaches agents how to keep
answers, evidence, governance, and convergence receipts separate.

Neighboring skills: `sensei-admission` controls mutation, `sensei-architect`
handles broad architecture reasoning, `sensei-import` onboards repositories, and
`sensei-benchmark` handles explicit blind external proof.

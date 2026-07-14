# Sensei Benchmark Skill

This managed skill guides explicit blind external proof of Sensei on historical
repository tasks. It keeps task curation, blind reconstruction, human direction,
oracle reveal, and final evaluation separate.

Primary entry point:

- `SKILL.md`

Reference files:

- `references/BLIND-EVALUATION.md`
- `references/TASK-CURATION.md`
- `references/HUMAN-DIRECTION.md`
- `references/ORACLE-EVALUATION.md`
- `references/FALSE-GREEN-RUBRIC.md`

It is installed by `sensei init` with managed manifests. Local edits are
preserved unless `--skills-force` is used.

Skill prose is not architectural authority. It teaches agents how to evaluate
Sensei honestly without oracle leakage or hidden critical false greens.

Neighboring skills: `sensei-import` onboards repositories, `sensei-admission`
controls mutation, `sensei-closure` closes bounded knowledge gaps, and
`sensei-architect` remains the broad architecture router.

# Sensei Admission Skill

This managed skill gives agents a small entry point for architecture-sensitive
mutation: determine whether one exact proposed change may be attempted, edit
only inside the admitted envelope, and verify the resulting diff.

Primary entry point:

- `SKILL.md`

Reference files:

- `references/ADMISSION-MODEL.md`
- `references/AGENT-WORKFLOW.md`
- `references/DECISION-SEMANTICS.md`
- `references/DIFF-VERIFICATION.md`

It is installed by `sensei init` with `.sensei-managed.json` manifests in the
canonical `.sensei/skills/` tree and native agent skill trees. Local edits are
preserved unless `--skills-force` is used.

Skill prose is not architectural authority. It teaches agents how to use
Sensei's admission receipts; correctness still comes from source inspection,
tests, runtime observation, review, and governed architecture.

Neighboring skills: `sensei-architect` routes broad architecture work,
`sensei-closure` handles incomplete architectural knowledge, `sensei-import`
onboards repositories, and `sensei-benchmark` handles explicit blind external
evaluation.

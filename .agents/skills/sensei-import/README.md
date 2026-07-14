# Sensei Import Skill

A managed skill that teaches an agent to onboard a repository into Sensei in one
guided run: clone, choose extraction depth, extract the contract layer (LLM
intent/contract grounding), extract structure, mine day-0 history/PR signals,
build a domain-scoped graph slice, verify, and stop at human promotion.

It exists so that "import gin" (or a bare git URL) becomes a safe, repeatable
pipeline instead of a sequence of CLI commands the operator has to remember and
order correctly. The guardrails — never auto-promote, always scope by domain,
require full history, degrade honestly — are encoded in `SKILL.md` so every agent
runs the import the same safe way.

- `SKILL.md` — the skill contract and core loop.
- `references/IMPORT-PLAYBOOK.md` — a worked example and the degradation branches.

Installed and managed by `sensei init` alongside `sensei-architect`. See
`docs/sensei-architect-skill.md` for the shared install-safety and update model.

After import, use `sensei-closure` to evaluate bounded closure for the first real
task and `sensei-admission` before mutation. Use `sensei-benchmark` instead of
this skill for explicit blind historical evaluation with a sealed future oracle.

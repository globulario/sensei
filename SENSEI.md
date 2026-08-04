# Sensei Report

Repository: github.com/globulario/sensei
Evaluated commit: 6a7312a07b089890d47f719e87d2c396ca613dd2 (resolved)
Evaluated content digest: aa961047cf19dab5c6f293c04540713259e5e87f57f27c54267897b07c6b96d3
Report schema: sensei.report.v1
Report freshness: CURRENT

## Summary

- Blocking findings: 1
- Advisory findings: 4
- Memory candidates awaiting review: 17

## Current Work

Task: task.documentation.8b5971ee1097
Title: Add a doc comment to multiString in cmd/awg/cmd_validate.go explaining its purpose, since it is reused as a repeatable-flag.Var type by several unrelated commands with no comment today
Disposition: BLOCKED
Scope: 1 file(s)
Authority: inspect: uncertifiable, modify: refused
Remaining blockers: 1
Primary blocker: write or high-risk scope has no identifiable required Test

## Important Findings

- [blocking] write or high-risk scope has no identifiable required Test
- [advisory] applicable authority records disagree
- [advisory] applicable authority records disagree
- [advisory] applicable authority records disagree
- [advisory] preserve requires current intended basis

## Verification

- Latest governed task: task.documentation.8b5971ee1097 — BLOCKED
- Report freshness: CURRENT
- Repository-wide verification: NOT RUN

## Behavioral Memory

- Candidates awaiting review: 17
  - candidate.authority.cmd.awg.cmd.serve.runserve (AuthoritySurface) — docs/awareness/candidates/authority_surface_candidates.yaml
  - candidate.authority.golang.server.main.serve (AuthoritySurface) — docs/awareness/candidates/authority_surface_candidates.yaml
  - candidate.implementation_pattern.awareness_graph_read_only (ImplementationPattern) — docs/awareness/candidates/implementation_pattern/candidate.implementation_pattern.awareness_graph_read_only.yaml
  - imported.skill.engineering.ask_matt (ImplementationPattern) — docs/awareness/candidates/skills/imported_skill_engineering_ask_matt.yaml
  - imported.skill.engineering.grill_with_docs (ImplementationPattern) — docs/awareness/candidates/skills/imported_skill_engineering_grill_with_docs.yaml

## Reproduce

```sh
sensei report
sensei report --check
```

# Agent Prompt Snippet

Before editing any file in this repository:

- Call `awareness_preflight` with the task and target files; branch on
  `risk_class`. `SECURITY_RISK` / `DATA_LOSS_RISK` / `UNKNOWN_IMPACT` need user
  approval. `EMPTY` is never `LOW_RISK`.
- Call `awareness_briefing` with file path and task for each file you'll edit.
- Treat `OK` briefing output as relevant constraints/context.
- Treat `EMPTY` as no direct anchors, not proof of no risk.
- Treat tool errors / `DEGRADED` as degraded awareness; report this explicitly.
- Never request or use arbitrary SPARQL.
- Do not bypass required tests or guardrails surfaced by awareness context.

After a fix that taught you something durable, record it with one
`sensei propose --kind <…> --contract "<…>" …` call (contract-first; it stages,
never commits). The `Stop` hook `sensei feedback-check` reminds you if you forgot.

## Sensei

This project uses [Sensei](https://github.com/globulario/sensei) to prevent
architectural drift. Before editing files, consult the awareness graph — it holds
the invariants, failure modes, forbidden fixes, and required tests that no diff
shows.

Sensei also installs the **Sensei Architect** skill. For architecture-sensitive
planning, implementation, debugging, review, recovery, migration, security, or
state/convergence work, load `.sensei/skills/sensei-architect/SKILL.md` before
planning or editing. Prefer native discovery from `.agents/skills/sensei-architect/`
when your agent supports Agent Skills. Use MCP tools when configured and CLI
fallbacks otherwise. Stay proportional to the risk and preserve durable lessons
with `sensei propose`.

### Behavioral guidelines

General discipline for every change (paraphrased from Andrej Karpathy's
observations on how LLMs fail at coding — the popular
[`andrej-karpathy-skills`](https://github.com/forrestchang/andrej-karpathy-skills)
file). Sensei adds the *repo-specific* rules below; these are the *general* ones:

1. **Think before coding.** State assumptions out loud. If the request is
   ambiguous, ask. If a simpler approach exists, push back. When confused, stop
   and name what is unclear — do not just pick one interpretation and run.
2. **Simplicity first.** Write the minimum code that solves the problem — no
   speculative abstractions, no flexibility nobody asked for.
3. **Surgical changes.** Touch only what the task requires. Do not improve
   neighboring code or refactor what is not broken; every changed line traces
   back to the request.
4. **Goal-driven execution.** Turn a vague instruction into a verifiable target
   first — "add validation" becomes "write tests for invalid inputs, then pass
   them."

### Rules

1. **Consult before editing high-risk files.** Run `sensei briefing --file <path>`
   before modifying any file listed in `docs/awareness/high_risk_files.yaml`. It
   returns the invariants, failure modes, forbidden fixes, and required tests that
   govern that file.
2. **Respect forbidden fixes.** The briefing lists patterns that look correct but
   are known-broken. Do not use them — revise rather than force one through.
3. **Run required tests.** The briefing lists tests that must pass when touching
   protected files. Run them before committing.
4. **Record new scars.** When you fix a durable failure class, add it back with
   `sensei propose` so the next agent inherits it.

### Commands

```bash
sensei briefing --file <path>     # context for a file edit
sensei briefing --task "desc"     # context for a task
sensei edit-check --file <path>   # does proposed content violate a rule?
sensei gate --diff <range>        # gate a diff (CI or pre-commit)
```

Requires a running server: `sensei serve` (defaults to `localhost:10120`). If a
Sensei MCP server is configured, prefer the `mcp__sensei__*` tools.

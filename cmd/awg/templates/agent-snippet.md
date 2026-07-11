## Sensei

This project uses [Sensei](https://github.com/globulario/sensei) to prevent
architectural drift. Before editing files, consult the awareness graph — it holds
the invariants, failure modes, forbidden fixes, and required tests that no diff
shows.

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

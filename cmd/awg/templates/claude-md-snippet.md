## Awareness Graph (AWG)

This project uses AWG to prevent architectural drift. Before editing files
in protected directories, consult the awareness graph.

### Rules

1. **Call briefing before editing high-risk files.** Run `awg briefing --file <path>`
   before modifying any file listed in `docs/awareness/high_risk_files.yaml`.
   Claude Code hooks enforce this automatically.

2. **Respect forbidden fixes.** The briefing output lists patterns that look
   correct but are known-broken. Do not use them. A PreToolUse hook
   (`edit-check-guard.sh`) also runs your proposed content through
   `awg edit-check` and blocks an edit that introduces a forbidden-fix shape;
   if that fires, revise rather than force it through.

3. **Run required tests.** The briefing output lists tests that must pass
   when touching protected files. Run them before committing.

4. **End non-trivial code tasks with the AWG summary line:**
   ```
   AWG: briefing(<target>) | invariants: X, Y | uncertainty: Z
   ```

### Commands

```bash
awg briefing --file <path>     # Context for a file edit
awg briefing --task "desc"     # Context for a task
awg preflight --file <path>    # Risk classification
awg check                      # Validate YAML sources
awg build                      # Recompile after editing YAML
```

### Adding Knowledge (one typed call)

After fixing a bug, record the scar with a single `awg propose` call. It
appends the entry to the right YAML source, rebuilds the seed, reloads the
local store, and stages the change — then stops. **You review and commit.**

```bash
# A way the system broke (links the invariant it violated):
awg propose --kind failure_mode --title "Stale seed served after reload" \
  --related-invariant <inv.id> --evidence "observed stale node" \
  --source-file golang/server/reload.go --required-test path_test.go:TestReloadFresh

# A test that proves the behavior now:
awg propose --kind required_test --id "path_test.go:TestReloadFresh" \
  --title "Reload serves fresh triples" --related-failure <fm.id>

# A repair that must never be applied again:
awg propose --kind forbidden_fix --title "Cache the reload path" \
  --related-invariant <inv.id> --description "caused the stale-serve failure"
```

Every entry must answer the contract-first questions: what contract was
violated/clarified, what failure we observed, what test proves it, what future
fix is forbidden, and which invariant/failure_mode it connects to. Vague notes
are rejected. If the contract is unknown, use `--kind contract_unknown` with a
`--proposed-contract` or `--revision-request`.

The `Stop` hook (`awg feedback-check`) reminds you if a session fixed a durable
error class but added no graph feedback.

# Diff Verification

Verification compares the working-tree diff with the admitted envelope.

It should surface:

- modified tracked paths inside or outside the envelope,
- untracked paths,
- missing decision or bundle receipts,
- stale or mismatched repository state.

MCP `verify_admission` accepts `decision_path`, `bundle_dir`, `repo`, and
optional `detail`.

CLI fallback:

```bash
sensei verify-admission --decision <decision.yaml> --bundle <dir> --repo <checkout> --output <verification.yaml> --format yaml
```

Never summarize a scope violation as mostly compliant. Any extra tracked or
untracked path means the action needs a new admission decision or explicit user
direction before further mutation.

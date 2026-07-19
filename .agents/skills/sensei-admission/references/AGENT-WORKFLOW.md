# Agent Workflow

1. Name the exact move before asking for admission.
2. Preserve the requested files and operation as the envelope.
3. If no bundle exists and `.sensei/project/claims.yaml` is present, run
   `sensei prepare-change` with `.sensei/project/graph.nt` and the exact files.
4. Run `sensei task-briefing --active --file <path>` for every planned file.
5. Run `sensei task-status --active --compact`, then `sensei advance-task
   --active` while automatic static Evidence is the primary action.
6. Ask only the primary architect question when required. Preserve all other
   questions in the task record.
7. For an existing explicit bundle, run MCP `admit_change` or the CLI fallback.
8. Stop mutation on `waiting`, `refused`, stale, or `uncertifiable`; admitted
   inspection does not admit mutation.
9. If admitted, edit only inside the envelope.
10. If the work needs a new file or broader behavior, stop and request new
   admission.
11. Verify the final diff with MCP `verify_admission` or the CLI fallback.
12. Report remaining proof plainly; compact task context is not correctness proof.

CLI verification:

```bash
sensei verify-admission --decision <decision.yaml> --bundle <dir> --repo <checkout> --output <verification.yaml> --format yaml
```

Receipt inspection:

```bash
sensei admission-status --decision <decision.yaml> --verification <verification.yaml>
```

The normal path should load only this skill. Route to `sensei-closure` only when
the admission result says architectural knowledge is incomplete.

The task bundle is the durable awareness record for unresolved questions. It is
not necessary to pretend the whole repository is closed before work can be
assessed, but no question that explicitly blocks the task's admission may be
silently bypassed.

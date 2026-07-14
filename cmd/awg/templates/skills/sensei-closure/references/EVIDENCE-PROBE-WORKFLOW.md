# Evidence Probe Workflow

EvidenceProbe artifacts are plans. Sensei does not execute probes, run tests,
perform shell commands, or create runtime observations.

Plan probes:

```bash
sensei plan-probes --closure <closure.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --graph-nt <awareness.nt> --output <probes.yaml>
```

Record results only after an external actor reports them:

```bash
sensei record-probe-result --probes <probes.yaml> --probe <id> --result-status <status> --evidence-status <status> --evidence-freshness <freshness> --observed-at <RFC3339> --executed-by <actor> --output <results.yaml>
```

Keep unavailable, stale, inconclusive, rejected, and failed results visible.
Never convert a planned probe into Evidence without an external observation.

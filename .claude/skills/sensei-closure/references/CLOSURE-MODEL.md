# Closure Model

Bounded closure asks whether one task has enough architectural knowledge to
proceed. It is not a claim that the whole repository is understood.

Closure inputs are offline artifacts: closure request, maintained claims,
dialogue, evidence state, graph snapshot, plane assessment, and maintenance
report as applicable.

Assessment command:

```bash
sensei assess-closure --request <closure-request.yaml> --claims <claims.yaml> --graph-nt <graph.nt> --repo <checkout> --format yaml
```

Possible next-action classes:

- architect: ask or record a human architectural answer.
- evidence: plan a probe or record an externally produced result.
- governance: wait for explicit governance.
- mechanical_repair: fix an artifact or unavailable input.

Closure artifacts remain non-authoritative until the repository's governance
promotes or accepts the relevant knowledge through its normal path.

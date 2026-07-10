<!-- Summary: what changed and why. -->

## Summary



## Governance-corpus diagnosis

If this PR mutates governance corpus — `docs/awareness/**`, `docs/awareness-control/**`,
or the embedded seed (`golang/server/embeddata/awareness.nt`) — **in response to a
detector finding** (`awg lifecycle`, `awg audit`, a self-coherence / principle check),
classify where the defect actually lives **before** the mutation. Do not relabel the
thermometer: a detector can be wrong about its own truth source.

State the diagnosis class (delete this whole section if the PR is not responding to a
detector finding):

- [ ] **corpus defect** — the truth source (YAML) is genuinely wrong → edit the corpus
- [ ] **reader / tool defect** — the detector/reader is stale or buggy → fix the tool, not the corpus
- [ ] **shared-vocabulary drift** — two readers disagree on a tier/term → reconcile the vocabulary
- [ ] **stale evidence plane** — regenerate/refresh the generated artifact, don't hand-edit source
- [ ] **external cross-repo lag** — tolerated lag → resync the mirror, don't force-equalize

Diagnosis class:

> _(one of the above, with one line on how the other classes were ruled out — or "n/a: not responding to a detector finding")_

_Enforces `governance.self_coherence_findings_require_source_of_truth_diagnosis`
(review-discipline; advisory). The 12-conflict lane is the precedent: a "7 UNCLASSIFIED"
finding was a stale **reader**, not corpus rot — the fix was the tool, not the truth source._

## Verification

<!-- tests / gates run; for governance changes: awg validate, awg audit -check, awg lifecycle, and `awg merge-check` before merge. -->

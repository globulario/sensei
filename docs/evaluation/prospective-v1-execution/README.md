# Prospective v1 — execution identity and pre-label disclosures

**This does not amend v1.** `docs/evaluation/prospective-v1-reference-set/` is frozen and
read-only. This directory freezes the *apparatus that will later interrogate it*, plus
everything else decided before the first applicability label exists.

`execution-identity.json` — `sha256:4c641b99482504fcda656dd89922110c895227087d0ebe69650d73e6441ebb2b`

## Why the environment is frozen now

The same graph, served with a different repository-context binding, reports a different
retrieval status for the same query:

| repository context | `preflight --file Makefile` |
|---|---|
| `github.com/globulario/sensei-code` | `PREFLIGHT_STATUS_EMPTY` |
| `github.com/globulario/sensei` | `PREFLIGHT_STATUS_DEGRADED` |

Zero governing items surfaced under both, so the substance is identical — but retrieval
status is itself a reported metric under protocol §7.3, and `EMPTY` and `DEGRADED` are
different results. Left unfixed, the loophole is: labels exist → try configuration A → try
configuration B → keep the better one. Nobody need intend that for it to happen.

The binding is therefore fixed above, before any label exists, with **no sampled change
having been run against it**.

## What `C_anchored` means, and does not

The protocol defines stratum C as a surface for which Sensei holds usable graph facts
*before* the change. It does not say the authoring path reaches them. These came apart in
practice during instrument construction: `query --mode related --id source_file:Makefile`
returns a governing invariant, while `impact` and `preflight` on the same path return
nothing.

So:

```
C_anchored          = the answer exists somewhere in Sensei's governed knowledge
prospective recall  = did the authoring path actually surface it
```

A low C is therefore not "the graph is empty there". It reads as *Sensei knows the law and
cannot deliver it to the author* — a sharper and more actionable finding than weak
retrieval in general. Recorded here, before the numbers exist, so it is not reached for
afterwards. It is consistent with §5 and §7.3 and amends neither.

## Pre-label probe disclosure

During instrument construction an AI (Claude) ran retrieval-shaped commands while
diagnosing the harness. Protocol §11 permits an AI to navigate evidence; it forbids an AI
deciding applicability, generating the answer key, or resolving disagreement. This is the
honest record of what was observed, so a reader can judge the exposure rather than take
an assurance.

Every probed path, audited against the 48 frozen packages:

| path | surface | in the sample |
|---|---|---|
| `golang/architecture/prospective/sample.go` | `preflight` (frozen surface) | no |
| `Makefile` | `preflight` (frozen surface) | no |
| `internal/tui/model.go` | `preflight`, `sensei-code` domain | no — different repository |
| `golang/server/reload.go` | `impact`, `by_file` | no |
| `golang/server/query.go` | `briefing --file` | no |
| `.claude/hooks/enforce-briefing.sh` | `briefing --file` | no |
| `golang/architecture/investigation/model.go` | `briefing --file` | no |
| `cmd/eval-arms/protocol.go` | `awareness_preflight` via an assisted context packet | no |
| `golang/architecture/evalsample/select.go` | `awareness_preflight` via an assisted context packet | no |
| **`README.md`** | `impact` only | **yes — 2 packages** |
| **`cmd/eval-arms/sample.go`** | `awareness_preflight` via an assisted context packet | **yes — 2 packages** |

Potentially exposed packages:

```
pr1:61776a08e7619e322a056b3460bd2c25   README.md
pr1:f5232357a6aef95e5534dbd7d4e9b01c   README.md
pr1:7b06a3ca309fc9d4a0c4d42abb290749   cmd/eval-arms/sample.go
pr1:bd0464ff35d3dc2a3b851c3637d4516a   cmd/eval-arms/sample.go
```

What was actually observed for those two paths:

- `README.md` — `impact` returned no rows. Zero information, and `impact` is not the frozen
  retrieval surface.
- `cmd/eval-arms/sample.go` — a task-scoped preflight over a three-file set reported
  `anchors=0`, coverage insufficient, and surfaced exactly one item: an
  `implementation_pattern`. **That class is not in the eligible corpus** (`invariant`,
  `failure_mode`, `forbidden_fix`, `incident_pattern`, `contract`, `meta_principle`), so it
  cannot be the answer to any applicability pair in this experiment.

### Standing constraint

The exposed party is an AI that is not adjudicating, and no human has seen any of this. The
mitigation is therefore behavioural and checkable rather than a promise:

- Claude does not adjudicate applicability and does not advise on it.
- Any labeling tool it writes orders and groups the corpus **deterministically from the
  blind corpus alone** — by `id`, `class`, `title`, `statement` — with no ranking,
  recommendation, or relevance ordering of any kind. That property is readable in the diff.
- If a human adjudicator would rather not rely on that, the four packages above can be
  routed to a different adjudicator. Doing so requires no change to v1.

`cannot_adjudicate` must **not** be used to absorb any of this. That label means the frozen
evidence cannot settle applicability; it does not mean somebody saw retrieval output.

## Second adjudicator

`second_adjudicator_unavailable`, as recorded in the reference set's `overlap-subset.json`.
The 10-item overlap subset is fixed regardless, before any label exists, so it is usable if
a second human becomes available. No AI may stand in for one, and none has.

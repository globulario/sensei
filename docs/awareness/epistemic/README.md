# The epistemic lane — uncertain design belief

`ledger.yaml` holds declared design questions, the hypotheses about them, and
the observations that move those beliefs. It implements the first slice of
[#288](https://github.com/globulario/sensei/issues/288), from
`docs/design/experimental-engineering-epistemology.md` §3–§7 and §11.

## What this is not

**Not canonical knowledge.** Nothing here is an invariant, a contract, a
decision or a scar. `yaml2nt` does not read this directory, no seed contains it,
and no routing surface consults it — `decideRoute` cannot reach it even by
accident, because there is nothing in the graph to reach. That is a structural
guarantee, not a promise.

**Not a review queue.** A DesignQuestion is never promoted into an invariant.
Entries in `candidates/` are awaiting a decision about becoming law; these are
not, which is why they do not live there.

**Not an authority grant.** Failure to retrieve knowledge is still not
permission to experiment. Nothing here reads an `EMPTY` retrieval, and there is
no path from silence to autonomy.

## Why it exists

Sensei already had rich classes for *established* knowledge. What was missing
was somewhere to put a claim that is **believed, testable and not established**.
Such a claim previously had to masquerade as law or go unrecorded.

```
CONSTRAINT        what must remain true            established knowledge
DESIGN QUESTION   what is actually being decided   positively declared uncertainty
HYPOTHESIS        what we currently believe        prediction + falsifier + horizon
OBSERVATION       what actually happened           evidence that moves a belief
```

## The two rules the schema enforces

**A disposition is computed, never authored.** There is no `disposition` field.
An agent exposes the decision structure — which alternatives survived constraint
binding, and what experimenting would actually cost — and the disposition
follows:

```
constraints leave exactly one alternative        → CONSERVATION
two or more viable + every consequence reversible → EXPLORATION_CANDIDATE
two or more viable + an irreversible consequence  → AUTHORITY
constraints eliminated everything                 → OVER_CONSTRAINED
```

`AUTHORITY` is reached by **consequence**, never by technical difficulty. There
is deliberately no field expressing that a question is hard, so "this is
difficult, a human should decide" is not sayable. An AI that routes a hard
technical question to a human has not found an authority boundary; it has found
work.

**Unrefuted is not supported.** `SUPPORTED` requires a recorded observation
*after* the horizon matured. Before it, the state is `AWAITING_HORIZON` however
many green runs accumulate; after it with nothing observed, `OVERDUE`. Silence
never becomes support — that is the horizon leak, and it is the most common
epistemic move in software engineering.

## Working with it

```bash
sensei epistemic declare      --id dq.x --question ... --alternative a=... --alternative b=... \
                              --constraint inv.y --consequence "..."
sensei epistemic hypothesize  --id h.x --question dq.x --alternative b \
                              --prediction ... --falsifier ... --due 2026-12-01T00:00:00Z
sensei epistemic observe      --id o.x --hypothesis h.x --outcome supports|refutes|inconclusive \
                              --what ... --evidence ...
sensei epistemic status       [--json] [--tripwire]
```

`make epistemic-check` runs the tripwire, and CI runs it on every push. A
hypothesis past its horizon with nothing observed fails the build. Clearing it
means recording an observation, or moving the horizon and saying why; letting
the date pass is not one of the options.

A falsifier must name an observation that could occur **while every existing
gate still passes**. `"the tests fail"` restates the merge gate and is refused —
though only the obvious restatements are caught, and no mechanical check
deserves authority over the rest.

## The failure this cannot prevent

The shape of a fake reasoning loop is:

```
declare a question → predict an answer → observe the answer was right → call it evidence
```

One actor congratulating itself at four stages. Everything in this lane
validates *shape*, and that loop is perfectly shaped.

So `status` counts it instead of pretending to block it:

```
self-confirmed:    1 of 1 supported (1.000) — supported only by the actor that declared the belief
                   reasoning has not yet escaped the reasoner here.
```

One supporting observation from a different actor clears a hypothesis. The bar
is deliberately that low — it measures whether anything outside the believer
ever agreed, not how much did.

It is reported and never gated, because the same shape is what an honest
one-person project looks like. A count nobody can see is how the shape becomes
normal. The current reading is `1 of 1`, on this lane's own first record.

## Experimental code, and the sediment barrier

The danger this lane creates if left alone:

```
agent guesses B → implements B → extraction records that B exists
    → B becomes architecture → agent can no longer replace its own guess
```

Architecture by sediment. Sensei would start defending experiments before
anyone knows whether they were any good, and every guess would become permanent
by the act of having been made.

So a hypothesis may name the code that exists **only** to test it:

```yaml
experimental_scope:
  - golang/placement/v2
```

> Code created to test a hypothesis must not become governing architecture
> merely because it exists. It remains mutable under its established
> architectural envelope until an explicit evidence-backed adoption promotes it.

> Promotion to architecture is an epistemic event, not a side effect of
> implementation.

**Naming a scope does not remove governance.** The established envelope still
holds — surrounding invariants, contracts and forbidden fixes apply exactly as
before. What it says is narrower: the design *inside* that envelope is
provisional and may be rewritten freely while the question is open.

```
        ESTABLISHED ARCHITECTURE
┌────────────────────────────────────┐
│ invariant A                        │
│ contract C                         │
│    ┌─────────────────────────┐     │
│    │ EXPERIMENTAL DESIGN     │     │
│    │ hypothesis H17          │     │
│    │ rewrite freely in here  │     │
│    └─────────────────────────┘     │
└────────────────────────────────────┘

        Conserve the envelope. Explore inside it.
```

`sensei epistemic scope` reports two things, and CI runs it:

| finding | meaning |
|---|---|
| `ARCHITECTURE_BY_SEDIMENT` | canonical architecture cites a path that exists only to test a **still-open** hypothesis |
| `ORPHANED_EXPERIMENT` | the hypothesis was refuted; the code written to test it is still declared as its scope |

A `SUPPORTED` hypothesis is **not** exempt. Going quiet there would restore
promotion-on-SUPPORTED — the automatic status transition adoption exists to
refuse. An **adopted** path is the only way out.

## Adoption: the event between SUPPORTED and ESTABLISHED

`SUPPORTED != ESTABLISHED`. Reaching SUPPORTED earns a design the **right** to
be adopted; it does not adopt it.

Every other route was eliminated before the verb existed:

```
promotion by implementation  → architecture by sediment
promotion on SUPPORTED       → an automatic status transition, refused by §9
implicit promotion           → architecture with no evidential basis
```

So `sensei epistemic adopt` is required rather than offered, and it is **the
only way out of the sediment check**. A supported design whose code canonical
architecture already defends stays a finding until adopted.

```yaml
adoptions:
  - id: ad.placement
    resolves_question: dq.placement
    design: b
    evidence_hypotheses: [h.placement]
    remaining_uncertainty: unmeasured above 10k writes/sec
    scope: [golang/placement/v2]
```

`remaining_uncertainty` is required, and `"none identified"` is an acceptable
answer — the point is that the sentence gets written. An adoption that silently
implies certainty is how SUPPORTED becomes PROVEN six months later, when nobody
remembers which one it was.

**Adoption is not a synonym for human approval.** When the question carries only
reversible consequences, the agent that ran the experiments may adopt. What
matters is not who typed the command but that the record carries what was
adopted, why, from what evidence, under what uncertainty, and under whose
authority. When the question reached `AUTHORITY` — an irreversible consequence —
`--authority` must be given.

*Caveat:* nothing **verifies** that a named authority is a person or that a
person agreed. It records an attribution, and treating the field as proof would
be the too-strong claim this lane exists to avoid.

### Two adoption paths, and their mirror-image risks

A `CONSERVATION` question needs no `--evidence`: its answer came from the
constraints rather than from an experiment, and demanding a hypothesis would
force a fake one to confirm what was already decided — epistemic ceremony.

```
EXPLORATION   multiple viable alternatives → hypothesis → experiment
              → observation → SUPPORTED → adoption

CONSERVATION  established constraints → exactly one viable alternative
              → adoption
```

Both require explicit adoption. Only the evidence basis differs.

The dangerous part is not the exemption. It is **how something earns
`CONSERVATION`**:

```
EXPLORATION risk   AI hypothesis → AI experiment → AI interpretation → AI adopts
CONSERVATION risk  AI wants design C → AI authors a constraint implying C
                   → question computes CONSERVATION → AI adopts with no evidence
```

The second reconstructs self-confirmation through a different door: the
experiment is not faked, it is made *unnecessary*. Both need something outside
the local reasoning loop — for exploration that is **observation**, for
conservation it is **established constraint provenance**.

> Conservation may bypass experimental evidence only to the extent that the
> constraints resolving the question are independently established enough to
> carry that decision.

*Independently* is the load-bearing word, and nothing here measures it: a
constraint is free text, and provenance strength is not a number. So `status`
counts the **exposure** rather than grading it —

```
adopted:  1 — 0 on a supported belief, 1 on constraints alone
```

— and `dq.conservation_adoption_evidence` records the question rather than
settling it. The one adoption made this way rests partly on a rule authored in
the same session as the elimination it justifies, which is weaker evidence and
is written down as such.

## Failed designs are kept, not erased

A refutation retires a design from architectural authority and **gains** it
value as evidence. So a refuting observation must carry its conditions:

```yaml
outcome: refutes
failure_conditions:
  - network partition with leader turnover
remaining_applicability: may still hold for non-authoritative cache placement
```

`"design B is bad"` is almost never what was observed. B failed under partition
plus leader turnover, or above some write volume — and a record that drops the
condition turns one experiment into a universal prohibition nobody tested. That
would make failed designs a *second* kind of frozen dogma.

`remaining_applicability` is optional and may be blank when nothing survives.

## What is deliberately not decided here

Whether any of this should ever inform routing. #288 is explicit that it must
not, until there is evidence the objects cause better engineering behaviour.
The router would currently have nothing to route.

## Known limitations of slice 1

- **Constraint references are not resolved.** `constraints` and `eliminated_by`
  are checked against each other, not against the graph — nothing verifies that
  `awareness.missing_evidence_produces_unknown` exists or that a `doc:` reference
  points at a real file. An id here looks authoritative and is not, which is why
  it is written down rather than left for a reader to discover.
- **Whether governance needs two axes is open.** `dq.governance_axes` records
  what slice 1 actually did (an implementation is experimental exactly while an
  open hypothesis names it) beside the alternatives, rather than retrofitting a
  justification for it.
- **There is no supersession.** An established design cannot yet be challenged
  and retired with its history kept. The lifecycle here runs one way —
  candidate → experimental → supported → (adoption, unbuilt) — and architecture
  should not become immortal merely because it was once established.
- **There is no ExperimentPlan.** The falsifier carries the preregistration
  burden today. A separate plan object only means something once an experiment
  can be *executed* by something other than the agent proposing it, which is the
  next slice, not this one.
- **`materially distinct` is not checked**, and cannot be. Only verbatim
  repetition is caught.

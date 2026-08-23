**Status: DESIGN HYPOTHESIS. Not implemented. Not governing law. Not authority policy.**

# Experimental engineering epistemology

Nothing in this document has been built, and nothing in it constrains any change.
It records a line of reasoning, the evidence that produced it, and the claims it
makes that could turn out to be false. A reader six months from now must be able
to tell *"we thought this was promising"* from *"Sensei requires this"*, and the
first line of this file is the whole answer to that question.

No router was changed, no schema was added, no graph was rebuilt, and no scar was
filed to produce it.

## 1. The problem, as observed

Measured against the live graph `def94857…` for domain
`github.com/globulario/sensei-code`, using its own authority router
(`internal/workflow/authority.go`, `decideRoute`):

```
183 tracked files probed
157  PREFLIGHT_STATUS_EMPTY   → "graph coverage is absent for the planned files"
 26  PREFLIGHT_STATUS_OK      → all 26 carry at least one blind spot
────
  0  files can reach architectural authority
```

No governed task in that repository can complete without a human, whatever the
providers do. The router is not malfunctioning: it grants only for `OK` **and**
zero blind spots **and** no inferred premise **and** a classified gate of `none`.

The 41 blind spots on those 26 covered files are:

```
22  file path under high-risk directory
15  anchor with severity=critical
 4  anchored entity in security/auth/rbac/pki/jwt/cert namespace
```

**41 of 41 are risk or severity signals. None represents missing knowledge.**
`anchor with severity=critical` fires *because* the graph knows something
important — a file becomes less grantable the more strongly it is governed. And
on 22 of those 26 files the risk-policy channel (`ChangeRisk.Gate()`) says
`NONE`, i.e. no human approval required, while the blind-spot channel escalates
anyway. Two existing channels state opposite things about the same file.

Two further observations shaped what follows.

**Causal attribution barely exists.** Across 991 commits of `globulario/sensei`,
explicit causal evidence links roughly 3%: 0 reverts naming a commit, 30 commits
naming an earlier SHA, and 2 of 253 scar entries mentioning a commit at all. The
cause was structural — `sensei propose` had no field for it — which is now
addressed by `--introduced-by` (PR #279). A rule linking a scar to any change
touching a file it names marks 73% of commits as corrected: a label that fits
almost everything distinguishes nothing.

**Human-as-oracle has limits.** #259's adjudication asks one person to judge
48 × 866 = 41,568 applicability pairs.

Observed, on first contact with the tool: some corpus items carry too little
semantic content for applicability to be decided confidently from the frozen
evidence — which is information about the reference material, not about the
adjudicator.

Inferred, not demonstrated: at that scale, fatigue and bulk sweeping are a
plausible failure mode for the denominator. Nobody has shown that all 41,568
judgements reduce to guessing, and this document should not claim it.

What follows from both together is narrower and enough: human labels cannot be
equated with architectural truth. What they can honestly establish is human
judgement from the frozen evidence presented, which is a different and weaker
thing than a complete applicability set.

## 2. Two laws

These are the load-bearing sentences. Everything else here is scaffolding.

> **Failure to retrieve knowledge must never itself constitute permission to
> experiment.**

> **An experimental question must be positively declared, not inferred from the
> absence of governing knowledge.**

The first blocks the productivity-shaped escape hatch: an agent must not obtain
autonomy by causing Sensei to find nothing. The second explains why.

A residual cannot be computed as *all applicable law − retrieved law*, because
the left-hand term is unknowable and Sensei is built never to assert it.
`EMPTY` means "I found nothing", never "there is nothing". Subtracting from an
unknown total is the same epistemic error as reading an empty result as an
absence of law.

The valid construction inverts it:

```
declared questions − questions settled by established knowledge
        = unresolved declared questions
```

Both sides are positive statements somebody made. Exploration is then reached by
*naming a question*, which is a reviewable artifact — not by producing silence.

## 3. Four epistemic objects

Sensei already has rich classes for established architectural knowledge —
invariants, contracts, failure modes, forbidden fixes, decisions, patterns and
more. The gap is narrower than "one kind of thing", and stating it broadly would
make this section easy to dismiss.

What is missing is an epistemic lifecycle for *uncertain design belief*. A claim
that is believed, testable and not established has only one path available to
it: candidate awaiting review, then canonical knowledge. There is no first-class
state for "believed, testable, but not yet established", so an uncertain claim
must either masquerade as law or remain unrecorded.

The hypothesis is that at least four kinds are needed, and that having nowhere
to put the middle ones is what forces uncertainty to look like either law or
nothing.

```
CONSTRAINT        what must remain true            established knowledge
DESIGN QUESTION   what is actually being decided   positively declared uncertainty
HYPOTHESIS        what we currently believe        prediction + falsifier + horizon
OBSERVATION       what actually happened           evidence that moves a belief
```

with, retained separately:

```
SCAR              expensive failure that must not be rediscovered
                  scar ──introduced-by──→ change        (PR #279)
```

The forward arc is the mirror of that edge. A failure gained a causal past; a
decision still has no epistemic future.

## 4. DesignQuestion rule

A declared question must name **at least two materially distinct alternatives
that are still viable when it is declared**.

Two, because one alternative is a decision already made. *Materially distinct*,
because a schema requiring two alternatives will otherwise be satisfied with
manufactured ones. *Still viable*, because alternatives already eliminated by
established constraints do not represent freedom.

A question that names no alternatives is a topic heading, not a degree of
freedom, and must not confer anything.

The disposition then emerges from constraint binding rather than being asserted:

```
constraints eliminate all but one alternative      → CONSERVATION
two or more viable alternatives remain             → OPEN QUESTION
open question + bounded consequences               → candidate for EXPLORATION
open question + unbounded/unacceptable consequences → AUTHORITY
```

An agent never writes `regime: exploration`. It exposes the decision structure
that would justify one, and the disposition is computed.

Note what this implies about scope: conservation, exploration and authority are
**dispositions of questions**, not classifications of files or changes. One
change may contain all three. #259's strata (A/B/C/D) describe *retrieval
conditions*, not regimes, and must not be used as a proxy for them.

## 5. Hypothesis rule

Mandatory: an **observable prediction**, an **observation horizon**, and a
**falsifier**. Optional: everything else.

Rationale and alternatives prose are the easy fields to write and the impossible
ones to check; a model will always produce plausible architectural oatmeal. Only
the fields reality can disagree with are worth enforcing.

The falsifier must name an observation **that could occur while every existing
gate still passes**. A falsifier of *"the tests fail"* restates the merge gate
and says nothing about the architectural claim.

```
weak     Hypothesis: this abstraction reduces authority leakage.
         Falsifier:  tests fail.

useful   Hypothesis: splitting evidence trust from verdict closure prevents
                     trustworthy evidence being reported UNTRUSTED.
         Falsifier:  a finding whose evidence is authoritative, fresh and
                     complete is still classified UNTRUSTED solely because
                     closure failed — with CI green.
```

## 6. Liveness

Horizons must produce work, or they are decoration.

```
prediction → due condition → observation → hypothesis update
```

A hypothesis becomes **due** at its horizon or on a stated condition
("after 100 successful reconciliation cycles"). Overdue, unobserved hypotheses
must be detectable:

```
past_due_hypotheses / active_hypotheses      plus the raw count
```

**`unrefuted ≠ supported`**, and `supported` must never be derivable from
"tests passed" — that is the horizon leak, and it is the most common epistemic
move in software engineering. A long-horizon claim whose clock has not matured
is `AWAITING_HORIZON` or `INCONCLUSIVE`, never `SUPPORTED`.

Each regime has its own silent liveness failure, and they are mirror images:

```
CONSERVATION fails by blocking legitimate work
             → detected by an execution tripwire   (exists: sensei-code #66)

EXPLORATION  fails by accumulating belief nobody revisits
             → detected by an overdue-observation tripwire   (does not exist)
```

A growing hypothesis table looks like learning. The overdue count is the only
thing distinguishing a learning loop from a filing cabinet, and it must ship
with the primitive rather than after it.

## 7. Consequence boundary

Reversibility refers to **consequences, not version control**. A branch is
trivially revertible; the experiment that ran on it may have mutated a database,
spent quota, published an artifact or sent something outward.

Experimental authority is therefore not licensed by *"I don't know"*. It is
licensed by *"I don't know, and I can learn without crossing an unacceptable
consequence boundary."*

## 8. Falsifiable claims about this architecture

Stated now, untested. This design is itself a belief, and the same discipline
applies to it.

**H-DQ1 — DesignQuestion prevalence.** Most nontrivial design-bearing changes
contain at least one genuine design question for which two or more materially
distinct alternatives survive established constraints.

*Scope:* nontrivial design-bearing changes only. A formatting pass, a generated
file refresh, a typo fix or a mechanical rename may legitimately contain no
design question; requiring one would recreate ceremony.

*Anti-circularity requirement.* The rule that decides which changes are
design-bearing must be fixed **before** any DesignQuestion is looked for, and
must not consult whether one was found. Selecting changes because they "look
like they contain real choices" selects on the presence of the very thing being
measured, and the claim then confirms itself:

```
select changes that appear to contain design decisions
        ↓
discover they contain design questions
```

The selection rule is deliberately not specified here. Specifying it is part of
testing the claim, and it must be frozen and published before the sample is
drawn — the same discipline #259 applies to its own inventory.

*If false:* DesignQuestion adds cost to every plan and buys nothing, and the
interesting uncertainty lives somewhere this model does not look.

*How it must NOT be tested:* not against #259's 48 pinned changes while
adjudication is active. Those are a blinded ruler, an AI has already seen
retrieval output for paths appearing in four of the packages (disclosed in
`docs/evaluation/prospective-v1-execution/README.md`), and interpreting them now
would be an accidental contamination event. Test after
`prospective-labels.json` is frozen, or on a separate mechanically selected
sample outside #259.

**H-LIVE1 — Overdue detection is necessary.** Without an overdue-observation
tripwire, the ratio of observed to recorded hypotheses declines over time.
*If false:* the tripwire is unnecessary bookkeeping.

**H-RESID1 — Declared questions are sufficient.** The unresolved-declared-question
construction identifies the genuinely open decisions in a change without needing
a completeness claim about the graph.
*If false:* important open decisions go undeclared and the model gives a false
sense of coverage — which would be worse than today, because it would look
principled.

## 9. Non-decisions

Explicitly **not** decided, and not to be inferred from this document:

- no change to `decideRoute` or any routing behaviour
- no `EMPTY → exploration` shortcut, in any form
- no numeric #259 score used as an autonomy threshold — the ruler's own
  limitations must be calibrated before it certifies anything
- no automatic hypothesis promotion; no automatic status transitions
- no learning engine
- no relaxation of any existing invariant, gate or blind-spot behaviour
- no claim that the risk-versus-uncertainty conflation in §1 is a defect; two
  readings remain open and #259 is the input that separates them

## 10. Sequencing

```
finish #259 adjudication
        ↓
measure prospective constraint discovery
        ↓
causal attribution capture            (PR #279, write path only)
        ↓
DesignQuestion — make uncertainty explicit
        ↓
Hypothesis — make guesses falsifiable
        ↓
due Observation + the back-edge that closes the loop
        ↓
accumulate real records
        ↓
only then revisit routing
```

Routing comes last because the future router would have nothing to route:
there are no declared design questions, no hypothesis lifecycle, no due
observations and no observation back-edge. Changing routing first would be
building doors before the rooms exist.

## Provenance

Reasoning developed in a working session on 2026-08-22/23 between the repository
owner, Claude (Opus 5) and ChatGPT (GPT-5.6 Sol), alongside the sensei-code
headless-execution work (#64, #65, #66) and the causal-attribution write path
(#279).

Several of the formulations this document rests on arrived through that
three-way exchange rather than from any one participant: the
conservation/exploration/authority distinction, both laws in §2, the correction
that a residual cannot be computed from retrieval's silence, and the
hypothesis/observation framing in §3. Recording that is not credit bookkeeping.
A document arguing that knowledge must carry its causal provenance should carry
its own. The measurements in
§1 are reproducible: file-level probes via `sensei preflight` against the graph
digest named there, and commit-level counts via `git log` over the ranges stated.

AI systems participated in producing this document. It is a design hypothesis
for humans to accept, reject or amend — not architecture already accepted, and
not knowledge.

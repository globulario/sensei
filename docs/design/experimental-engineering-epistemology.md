**Status: DESIGN HYPOTHESIS. Not implemented. Not governing law. Not authority policy.**

# Experimental engineering epistemology

Nothing in this document has been built, and nothing in it constrains any change.
It records a line of reasoning, the evidence that produced it, and the claims it
makes that could turn out to be false. A reader six months from now must be able
to tell *"we thought this was promising"* from *"Sensei requires this"*, and the
first line of this file is the whole answer to that question.

No router was changed, no schema was added, no graph was rebuilt, and no scar was
filed to produce it.

**Amended 2026-08-23 — Amendment 1, §11.** §10's sequencing made #259's
adjudication the gate on everything downstream of it. That was this document's
own evaluation protocol leaking into the product architecture, and §11 revises
it. The original sequencing is left in place rather than overwritten, because a
document arguing that beliefs must carry their history should not quietly edit
its own.

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

*Materially distinct* is the part no deterministic validator can settle. A model
can reason that "append-only record" and "mutate the existing receipt" differ
materially while "append-only record" and "append-only record with a renamed
type" do not; what is unavailable is a mechanical check that deserves
architectural authority on its own. So this is another judgment under evidence
rather than a concept beyond automation — which is consistent with the rest of
this document, and falsifiable by whether manufactured questions actually appear
in practice.

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

**H-WU1 — Decomposition beats partial execution.** Nontrivial mixed-authority
changes can frequently be decomposed into independently coherent work units,
such that human-owned dimensions do not unnecessarily block autonomous ones.

*Why it matters.* Composition is where the §1 measurement could repeat one level
up. If a change carries four questions and any single `AUTHORITY` disposition
escalates the whole change, then per-question routing buys nothing the moment
real changes routinely contain one sensitive dimension — which they will, since
anything touching persistence, publication or external state qualifies. The
grantable set would be zero again, for a better-articulated reason.

*Why not partial execution.* Half-applying a candidate produces state semantics
nobody wants: tests against which intended state, what completion means, which
commit represents the partial decision, whether the deferred question can
invalidate the executed part. Sensei's strongest property is that when it says
something happened, one coherent thing happened. Decomposition **before**
execution preserves that; partial application spends it.

*Anti-gaming requirement.* The cut line is itself a decision and can be gamed —
an agent that isolates the human-owned dimension into a small deferred unit and
takes autonomy over the rest has reached the escape hatch one level up. The
proposed criterion: **a split is legitimate only when the deferred unit's
resolution cannot invalidate the executed unit.** If the human could decide B
either way and A remains correct, they were independent. If B's resolution would
require A to be redone, the split was authority convenience, and executing A
commits to an answer nobody gave.

*If false:* most architectural decisions are too tightly coupled to split, and
partial autonomy buys far less than expected. That would be worth discovering
before building a work-unit ontology, not after.

**H-LIVE1 — Overdue detection is necessary.** Without an overdue-observation
tripwire, the ratio of observed to recorded hypotheses declines over time.
*If false:* the tripwire is unnecessary bookkeeping.

**H-RESID1 — Declared questions are sufficient.** The unresolved-declared-question
construction identifies the genuinely open decisions in a change without needing
a completeness claim about the graph.
*If false:* important open decisions go undeclared and the model gives a false
sense of coverage — which would be worse than today, because it would look
principled.

## 8a. A candidate law, not yet adopted

Every scalar this project has tried has broken against reality:

```
file    → grant / refuse
change  → grant / refuse
risk    → human / not human
```

Each time the structure turned out to be more dimensional than the verdict. The
pattern suggests a law, recorded here as a candidate only:

> **Authority belongs at the smallest semantic unit that can still be executed
> coherently.**

Not the file. Not necessarily the change. Not an arbitrary individual thought.
Probably a coherent work unit — but "coherent" is exactly what H-WU1 tests, and
until that is measured this remains an observation about a pattern, not a rule
anything should obey.

## 8b. A second candidate law, not yet adopted

*Recorded 2026-08-23. Two instances, no ontology.*

The same failure has now been found twice in unrelated mechanisms, and both
times it looked like working verification:

```
#282  graph says identity X
      graph contains N triples
      → "live store matches expected validated graph artifact"

#295  server serves artifact X
      same server serves the digest of X
      → "verify the exact GitHub release asset digest"
```

In both, the thing being checked also supplied the expected answer. What each
verifier actually established was *"the observation agrees with what its source
currently says the observation should be"* — which detects corruption in
transit and nothing else. What each verifier **claimed** was that the artifact
was the trusted one.

The candidate:

> **A verifier's reference must be independent of the observation it verifies,
> to the degree the claim requires.**

"To the degree the claim requires" is doing real work and is not hedging. A
transfer-integrity check may legitimately take its reference from the same
host; what it may not do is describe itself as artifact identity. The defect in
both cases was the pair, not either half alone — a weak check is fine, and a
strong claim is fine, and a weak check wearing a strong claim is the failure.

The repair has the same shape both times. The expected value must exist
**before and independently of** the observation being checked:

```
repository decision → expected digest ─┐
                                       ├→ compare → verified
downloaded artifact ───────────────────┘
```

And it is a genuine tradeoff rather than free rigour. #295 bought independent
artifact identity at the cost of pins that must move with `OXIGRAPH_VERSION`,
and the correct consequence of forgetting is a loud failure — never querying
the network for whatever answer makes the build pass, which would turn the
verification back into theatre by the shortest available route.

**Why this is recorded and not built.** Two instances are a pattern, not a law.
Sensei has an existing invariant covering the neighbourhood
(`invariant.an_identity_claim_must_be_derived_from_the_exact_thing_it_cl`,
whose own description already lists seven occurrences), and building an
independent-evidence ontology on top of it now would be inventing machinery
ahead of the evidence — the thing §5 and §9 exist to prevent. Let it recur.
If a third instance arrives in a mechanism unlike these two, that is when the
shape is worth naming formally.

*If false:* the two instances share a cause narrower than this sentence — both
were verification-at-fetch-time in a build path — and generalising from them
would produce a rule that fires on cases where taking the reference from the
source is exactly right.

## 8c. An observed prospective-recall specimen

*Recorded 2026-08-23. Not part of #259's frozen sample, and must not become
part of it.*

`scripts/fetch-oxigraph.sh` already carried the lesson, in its own header:

> "With no explicit version, the script follows GitHub's public 'latest
> release' redirect. It deliberately avoids the rate-limited Releases JSON API."

`deploy/Dockerfile` then implemented the same responsibility — fetch the
Oxigraph release asset — and used the rate-limited API, which failed a CI run
on a shared runner months later.

```
knowledge exists in the repository
        ↓
new implementation touches the same concern
        ↓
knowledge is not surfaced at authoring time
        ↓
the old failure reappears
```

That is precisely the faculty #259 was written to measure, occurring in
ordinary development rather than in an experiment. It is recorded here as an
anecdote with a date, and deliberately **not** added to #259's inventory: that
sample is frozen, its selection was deterministic from a frozen manifest, and
appending a case discovered *because it failed* would bias the denominator with
the very outcome being measured. A specimen chosen after seeing the result is
not a sample.

Its value is as a reminder that the frontier does not need invented test cases.
Real development keeps producing the shapes.

## 8d. Two constraints on machine admission, recorded before anything is built

*Recorded 2026-08-23, from the promote experiment in globulario/sensei#298 and
the open question globulario/sensei-code `dq.closure_knowledge_admission`. Both
constrain a classification system that does not exist yet, which is the point:
they are cheapest to state now and most expensive to retrofit.*

### Derivability is an output of verification, not an input

The tempting shape, once claims are split into ones a machine can establish and
ones it cannot:

```
agent:   claim_type = mechanically_derivable
Sensei:  fine, use the mechanical admission path
```

That hands the claimant the choice of which standard its own claim is judged
by, which is the same defect as free-text evidence wearing a taxonomy. The
B specimen would simply label itself derivable.

> **A claimant must not obtain authority by classifying its own claim.
> Mechanical derivability is established by successful derivation, not by a
> label.**

So the classification is a RESULT:

```
agent:   claim X
Sensei:  attempt derivation D(X) from pinned source, graph, tests, history
         succeeds → mechanically established
         fails    → not mechanically established
         cannot be attempted → UNKNOWN, which is an answer
```

`UNKNOWN` is the honest outcome and must not soften into "probably semantic, so
admit it under the weaker path". A derivation that was never run establishes
nothing, and saying so costs less than a category that quietly means "we gave
up".

### Claimant-controlled evidence: the narrow rule is not the general one

`#298` refuses an authority-increasing claim whose evidence references all
carry the commit that introduced the claim. That defeats direct
self-authorization and is deliberately narrow.

It must not be generalised into *"evidence from the introducing commit is never
valid"*. Real architecture will be created by agents, and its adoption record
will naturally cite the commit that implemented it:

```
hypothesis → experiment → observation → implementation commit → adoption
```

The implementing commit is legitimate evidence of WHAT WAS BUILT. It simply
cannot, alone, establish that what was built is correct.

> **Evidence controlled by the claimant may contribute to an
> authority-increasing claim, but may not be its sole establishing basis.**

The narrow refusal is a safe implementation of that sentence for the one case
measured. The sentence is the rule; the implementation is not yet the rule, and
the gap between them is where a future generalisation would go wrong.

### What these two are for

They bound the same remaining problem, which is now much smaller than it was:

```
these bytes exist                     — #298 establishes this
these bytes establish proposition P   — open
```

The question is no longer *"how can an AI write trustworthy architectural
knowledge"*. It is:

> **Which architectural propositions can Sensei independently derive strongly
> enough to admit them?**

That set starts small — a call edge, a lock discipline over a field, a test
exercising a path, an ownership relation — and can grow. Claims outside it stay
`candidate + verified evidence + not established`, which is a real state and
not a failure.

## 8e. Truth may compound, but authority must remain traceable

*Recorded 2026-08-23, alongside the first registered derivation. Nothing here is
built: derivations currently read pinned source only, and no established fact is
yet a premise for another.*

Once Sensei can establish a fact for itself, the obvious next step is to let
established facts be premises:

```
F1  Resource.spec is mutated only through Owner.Update()
F2  Owner.Update() increments generation
F3  reconciliation acts only on generation changes
        ↓
D1  a direct write to Resource.spec bypasses generation-based reconciliation
```

That is genuinely powerful — the graph stops merely storing what people wrote
and starts computing consequences of what the project already knows. It is also
where the prose path returns, wearing better clothes:

```
LLM reads F1, F2, F3 → writes "therefore X" → X becomes established
```

which is the thing every rung of this ladder exists to prevent. So the
constraint, before anyone implements premises:

> **Established project truth may serve as a premise for further truth, but
> every derived claim must retain a reproducible proof path to premises that
> were themselves established within compatible scope.**

Shorter:

> **Truth may compound, but authority must remain traceable.**

### What a compounding fact must carry

Enough to answer *"why do you believe this"* with a chain rather than a
sentence: the proposition, the premise ids **at the revision they were
established at**, the derivation and its version, the result, the scope, and the
revocation dependencies. Then

```
D2 ← D1 + I4 ← F1 + F2 + F3 ← source, contract, established architecture
```

is a thing a reader can walk, and a thing a machine can re-run.

### No epistemic inflation

A derived fact is no stronger than its premises and its derivation justify.

```
observation at revision R  →  a conclusion about revision R
contract                   →  a contract-level consequence
runtime sample             →  condition-scoped runtime evidence
```

"At commit abc123 every observed write passes through Owner.Update()" does not
become "Owner.Update() is eternally the only valid mutation mechanism". Only a
contract or invariant establishes that, and if none does, the stronger claim is
`UNKNOWN`.

### Supersession invalidates downstream, and this is the expensive part

An established fact stays usable **within its recorded scope and revision** until
something invalidates, supersedes or contradicts it. Not *once true, always
true* — software changes:

```
F1  sole mutation path is Owner.Update()      true at R1
R2  introduces Transaction.Apply()
        ↓
F1  superseded at R2, not nonsense — scoped
        ↓
anything derived from F1 must be re-derived, narrowed, or marked stale
```

Compounding makes that mandatory rather than tidy. One superseded premise can
silently invalidate a subtree of conclusions, and a chain nobody can walk
backwards is a chain nobody can invalidate. `Receipt.InvalidatedBy` exists today
for exactly this reason and covers only the single-step case.

### Why it is recorded and not built

Premise-chaining needs supersession to be real first, and supersession is
deliberately unbuilt (§11.9 leaves it to the case that actually needs it). A
compounding engine over facts that cannot be retired would accumulate
conclusions nobody can withdraw — the filing-cabinet failure of §6, with proofs
attached.

*If false:* the propositions worth deriving in real repositories do not chain,
each is established directly from source, and the premise machinery is
unnecessary weight. That is worth discovering before building it.

## 8f. Scope is part of truth, not metadata

*Recorded 2026-08-23. The world-compatibility half is implemented; the storage
half is blocked, and the blocker is named below rather than worked around.*

These are different propositions:

```
P    every Bus.subs access occurs under Bus.mu

P'   within the observations visible to field_access_under_lock/v1,
     over the files it read, at commit C,
     every observed Bus.subs access occurs under Bus.mu
```

The derivation establishes **P′**. If the graph stores **P**, the claim was
strengthened during serialization — by nobody, on no evidence, at the exact
moment the fact stopped being checkable.

> **Scope is part of truth, not metadata. Removing the scope from an established
> proposition creates a stronger proposition that was never established.**

This is the same law that produced §8b, and it has now appeared at three
boundaries in three weeks:

```
#298   an evidence reference must not outrun the observation
#300   a derivation must not outrun what it can see
#8f    stored knowledge must not outrun the derivation's scope
```

Three instances in three mechanisms is the recurrence §8b said to wait for. It
is still recorded rather than generalised into an ontology, for §8b's reason —
but the count is now worth stating.

### The world-compatibility half, implemented

A fact derived at commit C must not silently govern a candidate that changed the
files the derivation read.

> **A derived fact may govern only worlds compatible with the world and the
> dependencies from which it was derived.**

`Receipt.GovernsWorld` decides that from the inputs the derivation ACTUALLY
READ, not from the commit alone — so unrelated churn does not invalidate a fact,
and a change to what it read does. Anything it cannot determine is stale:
re-deriving is cheap, and a wrong answer here is authority granted on evidence
that no longer holds.

Measured: same world governs; an unrelated commit governs; a commit that moves
one access out from under the lock does not, names the file that moved, and
re-derivation against that candidate returns `NOT_DERIVED`. The fact is not
merely withdrawn — it is recomputable, which is what makes this different from
forgetting.

### The storage half, resolved by storing less

The blocker was real: no corpus class carries a world, a derivation identity and
a completeness scope, and forcing P′ into an `invariant` drops all three.

The way out is not a richer container. It is storing something weaker.

```
Derive      -> Established   the fact, in memory, scoped, now
Admit       -> StoredFact    the RECIPE, durable
Revalidate  -> Established   the fact again, in THIS world
```

> **Storage remembers what to check, not what is true.**

A StoredFact carries the typed proposition and the derivation that can decide
it, plus provenance describing where it was once established. Nothing in it
asserts that the proposition holds now, and there is no accessor that reports it
as true without re-running the derivation.

Three things fall out, and none of them needed building:

**A forged record is harmless as a truth claim.** Anybody can write the bytes;
nobody can make a derivation succeed by writing them. The worst a fabricated
entry achieves is wasting one derivation — which is why this object can be
serialized at all, where a stored verdict could not be.

**Supersession needs no engine.** A recipe whose re-derivation stops succeeding
stops producing a fact. There is no cached truth to invalidate because none was
cached, and §8e's dependency-invalidation problem does not arise for facts
nobody stored the answer to.

**The ratchet still holds.** What the project no longer rediscovers is *which
proposition is worth checking here* — the judgment-bearing half an agent's
investigation produced. Recomputing the answer is parsing a package.

### Descriptive, not normative

A derived fact says *this was independently derived as holding, within this
envelope, at this world*. It does not say *this must remain true*.

Deriving that `Bus.subs` is accessed under `Bus.mu` does not oblige any future
implementation to keep that mutex: replacing it with ownership, a channel or an
atomic may be entirely correct, and a description has no standing to forbid it.
`Normative()` is permanently false, as a method rather than a comment, so a
caller reaching for *may I treat this as a constraint* gets an answer instead of
a silence it can interpret.

That is also enough for the coverage question, which only ever asked whether
Sensei knows anything here.

### What is still not done

The remaining step is graph participation: a preflight that consumes a
StoredFact by revalidating it, so `PREFLIGHT_STATUS_EMPTY` becomes covered
because a derivation succeeded — not because a record exists. Coverage that
counted stored recipes without re-running them would let a forged entry close a
gap, which is the one way this design can still be got wrong.

### The original blocker, for the record

There is no corpus class for a scoped, revision-bound, derivation-backed fact.
The promotable classes are `invariant`, `failure_mode`, `incident_pattern`,
`intent`, `meta_principle`, and none of them carries a world, a derivation
identity, or a completeness scope.

Storing P′ as an `invariant` would drop every one of those fields, and an
invariant reads as *this must remain true* where the derivation established
*this property was independently derived as holding within this observation
envelope*. Those are not the same epistemic object, and the conversion is
exactly the strengthening this section forbids.

So the remaining question is narrow and concrete:

> **How does a machine-established proposition participate in governance
> without stripping the scope that made its establishment legitimate?**

Not answered here. Forcing it into an existing class to make the coverage
machinery happy would close globulario/sensei-code#67 by cheating its own
experiment.

## 8g. Persist the procedure or the verdict, according to what makes it durable

*Recorded 2026-08-23, from the §8f storage problem dissolving rather than being
solved. One case; not generalised into machinery.*

The storage half of §8f looked like it needed a richer container: a knowledge
object carrying world, derivation identity and completeness scope, so that a
derived fact could be stored without being strengthened. What it actually needed
was to store **less**.

> **Storage remembers what to check, not what is true.**

A durable record carries the typed proposition and the derivation that can
decide it. Nothing in it asserts the proposition holds now. Truth is recomputed
in the world being asked about, and never cached — so it never goes stale,
because staleness is a property of caches.

The candidate law behind it, which is broader than this one mechanism and is
therefore recorded rather than acted on:

> **Persist the epistemic procedure when the truth is cheaply reproducible;
> persist the verdict only when the verdict itself has durable authority.**

Some knowledge is durable because its AUTHORITY is durable — a contract, an
adopted decision, a policy, a scar. Somebody with standing decided it, and no
amount of recomputation replaces that. Other knowledge is durable only as a
METHOD OF OBSERVATION: a technical fact about the current code, cheap to
recompute and wrong to freeze.

Sensei had good representation for the first kind and none for the second, which
is why the second kept trying to disguise itself as an invariant.

*If false:* the propositions worth persisting are not cheaply reproducible after
all — a derivation over a large repository is expensive, and recomputing on
every assessment is the wrong trade. That would argue for caching verdicts with
explicit invalidation, which is the design this one avoided.

### The forbidden collapse, and why it is a type and not a rule

```
stored recipe present  ->  coverage          FORBIDDEN
stored recipe -> revalidate -> Established -> CoverageAnchor    the only path
```

A recipe is a question. Counting questions as knowledge turns *"I know what to
ask here"* into *"I know the answer"* — the same error as counting a non-empty
evidence string as evidence, one layer up, and it would let a fabricated record
close a real gap.

`AnchorFor` takes an `Established`, which only `Derive` returns, so a caller
holding a recipe has nothing to pass it. A test pins the signature, because the
dangerous version of this feature is one overload away.

Three attacks, each blocked and each tested:

```
forged recipe                        -> NOT_DERIVED  -> no anchor
purpose claim (the B specimen)       -> UNKNOWN      -> no anchor
valid recipe vs a candidate that
  broke the discipline               -> NOT_DERIVED  -> no anchor
```

And an anchor may not claim a world it was not derived in, so a fact established
at the base cannot be re-pointed at a candidate by hand.

## 8h. Relevance is not truthfulness — a preserved adversary

The second derivation family was chosen over a third candidate, and the
rejected candidate is worth keeping forever, because it is the most seductive
wrong answer this architecture will be offered.

### The specimen

Asked to pick what to derive next, the obvious optimisation is coverage reach.
By that measure the clear winner was a layering proposition:

    package P does not import package X

It is cheap, it is exactly derivable from the import graph with no analysis at
all, it never returns UNKNOWN, and measured against `sensei-code` it came back
**41/41 DERIVED** — every package, every proposition, true. It would have
anchored coverage across the entire repository in a single pass and closed more
EMPTY regions than both shipped families combined.

Every one of those 41 facts is TRUE. Not one of them is USEFUL. Nobody was
uncertain whether `internal/tui` imports `internal/publish`; no change was ever
blocked on it; no failure mode was ever traced to it. It is a machine
enumerating things nobody asked, and then counting the answers as knowledge.

### The law

> **Truthfulness is necessary for coverage, but truthfulness alone is not
> sufficient for relevance.**

A derivation that is always true is not thereby valuable. It may be measuring
something that could not have come out otherwise — in which case it produces
facts at high volume and information at zero.

### Why it is dangerous specifically HERE

The whole point of this bridge is that a machine-derived fact does not need the
proposing model to be trustworthy. That defence is aimed at FALSEHOOD. It is
completely silent about IRRELEVANCE, because an irrelevant fact passes every
check honestly: it derives, it revalidates, it anchors, it survives
adversarial review. A model that wanted to manufacture autonomy would not
forge a fact — forging is what the architecture already stops. It would derive
thousands of true, trivial ones and let coverage do the rest.

So `reach` must never become the selection criterion for a derivation family.
The second family was chosen because it is a different SPECIES of relationship
(ownership of an external boundary vs. a synchronization relation), and its
first real run refuted a confinement two packages were quietly violating —
information that could have come out the other way, and did.

### The standing test

Before a new derivation family is added, it must answer:

1. Could this proposition have come out false in this repository? If no family
   member is ever refuted, the family is measuring a tautology of the language
   or the build, not of the architecture.
2. Was anyone uncertain? Name the change, review, or failure that would have
   gone differently had this been known.
3. Is it a different species from what exists, or the same relation at greater
   volume?

The layering family fails 1 and 2 and is retained, underived, as the specimen.

### Status

Recorded, not adopted. This is a candidate law under the same rule as §8a and
§8b: it constrains what gets built next, and it is not yet an invariant.

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

**Superseded by §11.** Kept as written so the correction is visible.

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
measure mixed-authority frequency (H-WU1)
        ↓
only then revisit routing and decomposition
```

Routing comes last because the future router would have nothing to route:
there are no declared design questions, no hypothesis lifecycle, no due
observations and no observation back-edge. Changing routing first would be
building doors before the rooms exist.

## 11. Amendment 1 — the human is not the technical answer key

*Added 2026-08-23. Supersedes §10. Amends §4's interpretation. Changes nothing
in §2, §7 or §9.*

### 11.1 What was wrong

Not a claim in §1–§9. The **sequencing** in §10, and the reading of §4 it
implied. Followed literally, they produce this:

```
AI proposes design
        ↓
Sensei says "ask the owner which rule applies"
        ↓
owner adjudicates
        ↓
AI continues
```

That is a very elaborate human-powered compiler. It is the opposite of the
system this document was written to reach.

The evidence for the correction was already in §1, three paragraphs above the
mistake: human labels *"cannot be equated with architectural truth. What they
can honestly establish is human judgement from the frozen evidence presented."*
If they are not truth, they cannot be the gate that everything downstream must
pass through. §10 made them one anyway.

### 11.2 What does not change

Both laws in §2 stand exactly as written. Failure to retrieve knowledge is still
not permission to experiment, and an experimental question must still be
positively declared. §7's consequence boundary stands. Every non-decision in §9
stands.

§11 changes **who resolves a declared question**. It does not change whether
silence can confer authority — it cannot, and nothing here provides a route to
autonomy through an empty retrieval.

### 11.3 #259 and #131 are equipment, not gateways

Both are preserved **exactly as frozen**. Editing a frozen experiment on
discovering it is inconvenient is bad science, and the finding below is worth
more intact than the experiment would be rewritten.

What changes is their standing. Neither is a prerequisite for the epistemic work
in §11.6. Their protocols keep their own authority over their own results:
#131's §14 still forbids an AI generating that reference set's labels, and #259
still refuses to run without its frozen answer key. Nothing here weakens either.
Adjudication may continue whenever the calibration is judged worth its cost, and
nothing waits on it.

> A frozen experiment measuring prospective retrieval against a human-labelled
> reference set is a ruler. It does not become the road.

### 11.4 Observation O-SCALE1

Recorded as an observation in the §3 sense — something that happened, stated at
the strength the evidence supports.

> Two independently designed evaluation programs, written to different protocols
> for different faculties, both reached machinery-complete and both terminated in
> an exhaustive human adjudication step: **805 items** (#131, protocol v2 §4's
> six human-truth metrics) and **48 × 866 = 41,568 applicability pairs** (#259).

*What this is evidence for:* exhaustive human ground truth is a scaling bound on
evaluation **design**, for at least these two programs, and it binds before the
system under test is ever exercised. Both were blocked with zero labels
recorded, so the bound is not a fatigue claim — it is reached at n=0.

*What it is not evidence for:* that the labels would be wrong; that adjudication
is worthless; that any specific alternative ruler works. n=2, both designed by
the same people in the same month, both for retrieval-shaped questions. A third
program designed by someone else might not land here at all.

### 11.5 Rulers that do not need an oracle

Four constructions where the expected answer comes from how the experiment was
built, or from what already happened, rather than from someone deciding it:

1. **Constructed-positive.** Start from an established invariant, construct a
   prospective change that would violate it, withhold the relationship from the
   retrieval path, and ask whether the invariant came back. The target is known
   because the experiment was built out of it.
2. **Historical-causal.** `change X → failure Y → scar S → corrective change Z`.
   Ask prospectively whether S would have surfaced before X. `--introduced-by`
   (PR #279) is the write path that makes this population exist at all; §1
   measured why it did not before.
3. **Mutation.** A valid implementation, a bounded controlled violation of a
   known contract, and the governing knowledge that must be retrieved. #131's
   world 4 already works this way.
4. **Discriminating execution.** Two surviving alternatives, one workload and
   invariant suite, measurement decides. Nobody needs to know the answer
   beforehand — which is the property the other three do not have.

Each has its own way of lying, and naming them is part of adopting them:

- **1 and 3** measure retrieval against defects *we already know how to
  describe*. A seam nobody thought to mutate is outside the sample, and the
  construction can be gamed by building only defects the current retrieval
  happens to catch. The construction rule must therefore be frozen and published
  before the sample is drawn — the same discipline §8's anti-circularity
  requirement imposes on H-DQ1, for the same reason.
- **2** inherits every bias in what got written down. §1 measured explicit causal
  attribution at roughly 3%; a population drawn from the recorded 3% is not a
  random sample of failures, and a recall number over it must say so.
- **4** answers only what the workload discriminates. *"A was faster on this
  suite"* is not *"A is architecturally correct"*, and the distance between those
  two sentences is exactly where a benchmark quietly becomes the design.

None of these removes the human. They narrow what the human is asked for.

### 11.6 The narrowed human surface

```
HUMAN            goal; unacceptable outcomes; value and product tradeoffs;
                 spend; irreversible or outward-facing consequence;
                 "yes, this is the thing I want built"

NOT HUMAN        which data structure; whether invariant X governs file Y;
                 which retry strategy is correct; which generation number;
                 whether this abstraction preserves the contract
```

The second list is engineering. §7 already says what licenses settling one
without asking: not *"I don't know"*, but *"I don't know, and I can learn
without crossing an unacceptable consequence boundary."*

So:

```
uncertain technical choice + bounded consequences   → AI resolves it
value choice, or unbounded/irreversible consequence → human authority
```

### 11.7 DesignQuestion, reinterpreted

§4 built the object but left its purpose ambiguous. Exposing a decision *so it
can be reviewed* and exposing it *so it can be resolved* are different systems
wearing the same schema. It is the second.

> **DesignQuestion exposes uncertainty so the AI knows what it has to resolve.**

```
DesignQuestion — declared by the AI
        ↓
bind established knowledge as constraints
        ↓
does exactly one alternative survive? ── yes ──→ CONSERVATION
        │ no
        ▼
can a bounded experiment discriminate the survivors?
        │
   yes ──┴── no
    │          │
    ▼          ▼
 hypothesis   consequence or value boundary
 experiment            ↓
 observation      human decides — and only here
 decision
```

§4's disposition table stands unchanged. What §11 fixes is the reading of
`AUTHORITY`: it is the **narrow** branch, reached by consequence or by value,
never by technical difficulty. An AI that routes a hard technical question to a
human has not found an authority boundary. It has found work.

### 11.8 H-RATIO1 — a new falsifiable claim

```
human technical decisions
─────────────────────────    must fall over time
completed engineering decisions
```

and must fall **because** uncertainty became measurable and experimentally
resolvable — not because the standard for calling something resolved dropped.

Which is why the ratio alone is not the metric. It travels with the defect and
scar-per-change rate over the same window, and is read only against them: a
falling ratio beside a rising scar rate is the failure mode, not the result. A
single number here would be the horizon leak of §6 in a new costume — the AI
grading its own autonomy by how little it asked.

*Observation horizon:* first reading once ten decisions have run through §11.7,
whichever regime they landed in.

*Falsifier:* the ratio falls while scar-per-change rises over the same window —
or it does not fall at all after ten decisions, which would say the uncertainty
in this codebase is mostly not experimentally resolvable and §11.6's second list
is more optimistic than reality allows.

### 11.9 Revised sequencing

Replaces §10.

```
merge the graph-integrity fix                          (#283)
preserve #284 and #285 as frozen equipment, de-gated
        ↓
DesignQuestion — the AI declares what it must resolve
        ↓
Hypothesis — prediction, horizon, falsifier            (§5 rules unchanged)
        ↓
discriminating experiment, inside the §7 consequence boundary
        ↓
Observation — the back-edge that closes the loop
        ↓
AI technical decision: implemented, verified
        ↓
accumulate real records
        ↓
measure H-RATIO1, then H-WU1
        ↓
only then revisit routing and decomposition
```

Routing still comes last, for §10's original reason: the future router would
have nothing to route. The change is what sits at the top — the epistemic slice
begins now instead of behind an adjudication queue.

### 11.10 The first proof must be real work

Not a demonstration written to succeed. The next genuine uncertainty encountered
while developing this repository is the first case, and it runs in this order:

```
declare the question
generate alternatives
bind established constraints
preregister what would discriminate the survivors
execute
observe
choose
implement
verify
```

Preregistration **before** execution is the load-bearing step. Without it the
record is a story told after the fact, and §5 already refuses that shape for a
falsifier for exactly the same reason.

The human watches, and intervenes when a real authority boundary is crossed.

### 11.11 Provenance

Correction raised by the repository owner on 2026-08-23, on reading §10 of this
document alongside the state of #259 and #131. Developed in that session with
Claude (Opus 5). It revises this document's own sequencing on the strength of an
argument this document already contained, which is the outcome §8 asks for and
not a failure of the original.

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

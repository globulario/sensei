# Where agent memory pays for correctness: the write-time / read-time trade-off

*Design only — a principle, not a feature. No engine, rule, or graph changes are
made by this document. It names the general law that Sensei instantiates, states it
as a trade-off rather than a slogan, and records when the Sensei side of the trade
is the right bet and when it is the wrong one.*

## The principle

> **Agent memory must pay for its own correctness somewhere. The only choice is
> *where*: at write time, by constraining what is allowed to enter the store, or
> at read time, by reconstructing trust probabilistically on every retrieval.
> You cannot avoid the cost; you can only move it.**

Every durable agent-memory system sits on a spectrum between two poles:

- **Fail-open (read-time correctness).** Accept any write. Store free-form text.
  Defer all judgement to retrieval: embed everything, rank by similarity,
  re-rank, hope the right chunk surfaces and the wrong ones don't. RAG and
  free-form memory files live here.
- **Fail-closed (write-time correctness).** Reject a write unless it names an
  explicit contract, carries provenance, and types cleanly. The store is a
  verifiable artifact, not an accumulating pile. Sensei lives here: `awg propose`
  routes anything that resolves to `contract_unknown` into `candidates/` instead
  of the graph.

Neither pole is "correct." They are two ways of buying the same property —
*trustworthy recall* — and they bill you at different times.

## Why it is a trade-off and not a winner

The fail-closed pole has a real, structural cost that the slogan
"structured beats free-form" hides:

**Write-time constraints add friction, and friction suppresses capture.**

A write that must name a contract can only be made where a contract exists and
is discoverable. Where it doesn't, the write is *refused* — correct by
construction, but the knowledge is lost rather than captured. Sensei's own
cold-bootstrap finding is exactly this failure mode: solo repos don't
triangulate (no commit + PR-review channels to ground intent against), so the
contract-first gate has nothing to validate against and learning stalls. The
gate didn't capture worse knowledge; it captured *no* knowledge.

Symmetrically, the fail-open pole's cost is deferred but not avoided:

- **Drift instead of compounding.** Free-form memory degrades as it grows. More
  text means more retrieval noise; contradictions accumulate silently because
  nothing rejects them at the boundary. The store gets bigger without getting
  better.
- **Provenance is archaeology.** "Why does the agent believe this?" is a dig
  through chat logs, not a query.
- **You cannot cleanly un-learn.** A wrong fact smeared across embeddings can't
  be deleted; it can only be diluted.

So the honest statement is not "fail-closed wins." It is:

> **Choose where you pay for correctness — write-time or read-time — based on
> whether your domain can supply the constraints cheaply.**

## Factual vs behavioral memory: why norms force the write-time pole

The trade-off above assumes both poles are *available* and you are choosing
between them. For most of what Sensei actually stores, one pole is barely
available — and which pole depends on what *kind of claim* a memory makes.

Sort memory by the kind of claim it makes:

- **Factual** — truth-conditioned, checkable against the world, re-derivable.
  In Sensei this is the structural substrate: import graphs, Components,
  Boundaries, the Evidence that records what was observed. "X imports Y" is
  true or false, and you can re-derive it.
- **Behavioral / normative** — *no truth value*; it carries justification and
  applicability conditions instead. In Sensei this is the center of gravity:
  Contracts ("this boundary must be honored this way"), Decisions, the
  MetaPrinciples, and nearly everything the feedback write path captures.
  "Fail closed on ambiguity" is not *true* — it is a policy that is good or bad
  *in a context*.

Sensei is therefore not all behavioral, but its shape is a thin **factual scaffold**
(structure) carrying a thick **behavioral superstructure** (norms). The
behavioral layer is the hard-won, distinctive part; the structural extraction is
comparatively cheap.

This split decides which pole is even usable, because:

> **The read-time correctness mechanism exists for facts and barely exists for
> norms.**

The fail-open pole works by deferring correctness to retrieval: pull a chunk,
check relevance, let the model sanity-check the retrieved *fact* against its own
knowledge. Hand that same model the retrieved string "fail closed on ambiguity"
and it cannot verify that this is the *right* norm to apply here — applicability
is contextual, and there is no embedding-similarity equivalent of "is this norm
correct?" A norm has no read-time truth-check to defer to.

So for behavioral memory the choice collapses:

> **Behavioral knowledge cannot use the read-time correctness mechanism. That
> pushes it to the write-time pole — not by preference, but because the
> alternative does not work for norms.**

This is *why* contracts must be explicit. It is not a stylistic choice layered on
a generic store; it is forced by the content. A factual store can get away with
implicit grounding — re-derive truth on read. A normative store cannot, because
its norms have nothing to re-derive against. Sensei being mostly behavioral makes
its fail-closed stance **more** justified, not less.

Two consequences follow, and both are already visible in the design:

1. **Evidence means something different for a norm.** For a fact, Evidence is
   *proof* — it shows the claim is true. For a norm, Evidence is *endorsement or
   outcome* — "this principle was applied here and paid off," not "this principle
   is true." The two senses of grounding should not be conflated, because the
   write-time gate validates them differently.
2. **Conflict resolution is not "which is true."** Two contradicting facts mean
   one is wrong. Two contradicting norms mean a *precedence / scope* question —
   which applies, where, and which yields. That is exactly what the
   meta-principle categories and the review-only ratchet are for; that machinery
   only makes sense for behavioral content.

## The decision rule

Pick the fail-closed (Sensei-style) pole when **all** of these hold:

1. **Contracts genuinely exist** in the domain — there is a real, nameable thing
   each memory must bind to (an interface, an invariant, a typed boundary).
2. **They are discoverable cheaply** — the agent can resolve the contract at
   write time without a human in the loop for every write.
3. **Compounding matters more than coverage** — you would rather have a smaller
   store you can trust and audit than a larger one you must re-validate on every
   read.
4. **Un-learning is a requirement** — being able to point at a wrong belief and
   delete it is worth the friction.

Pick the fail-open (free-form / RAG) pole when **any** of these hold:

1. **No contract exists** — e.g. a user's casual preferences, episodic notes,
   "remember that I like terse answers." There is nothing to bind to, so a
   write-time gate only suppresses capture and buys nothing.
2. **Constraints are expensive to supply** — cold-start, solo repos, domains
   with no triangulation channel. Until the constraint substrate exists,
   fail-closed starves.
3. **Coverage matters more than per-item trust** — you would rather capture
   everything noisily and sort it out later than lose anything.

## Where Sensei sits — and where it deliberately doesn't

Sensei bets the fail-closed pole for **structured engineering knowledge**, because
that is precisely the domain where the four "pick fail-closed" conditions hold:
contracts exist (Components, Boundaries, Contracts, Decisions), they are
extractable, the graph is meant to compound across sessions and repos, and
deleting a wrong node is a first-class operation.

It would be the *wrong* bet for the consumer-memory case — remembering that a
user prefers terse answers has no contract to validate against, and a
write-time gate there would just drop the note. That case correctly belongs to
the fail-open pole. This is not a weakness of Sensei; it is the trade-off being
honored rather than ignored.

## A hybrid is legitimate, not a cop-out

Because the cost is *where* not *whether*, a system can place different memory
classes at different poles:

- **Structured engineering facts** → fail-closed graph (Sensei, `awg propose`,
  contract-first, Evidence-backed).
- **Episodic / preference / cold-start notes** → fail-open free-form capture,
  with a *later* promotion path: a note can graduate into the graph once a
  contract becomes discoverable for it. This is the same shape as the existing
  `candidates/` quarantine — capture now, validate when grounding exists, never
  block capture on a constraint that isn't there yet.

The principle tells you the hybrid is not a compromise between two correct
designs; it is the correct design *recognizing that different memory classes
have different constraint economics*.

## What this does and does not claim

- It does **not** claim structured memory is universally better. The slogan
  "structured beats free-form" is the part that does *not* transfer.
- It **does** claim the *trade-off* transfers: any agent-memory designer must
  decide where correctness is paid, and that decision should be driven by the
  domain's constraint economics, not by taste.
- It locates Sensei honestly: the right bet for a domain rich in cheap, discoverable
  contracts, the wrong bet for a domain with none, and a hybrid wherever a single
  system spans both.

## Related

- [memory-scope-bands](memory-scope-bands.md) — *what* memory Sensei is responsible
  for (the durable, system-specific band) and why **agent + Sensei** covers the
  surface; this doc is *where* that band pays for correctness.
- [contract-first-resolution](contract-first-resolution.md) — the explicit-contract
  rule and the corpus→pack→seed promotion path the write-time gate depends on.
- [hard-gate-design](../hard-gate-design.md) — the same fail-open/fail-closed
  axis applied to *enforcement availability* (the EditCheck gate fails **open**
  on outage, deliberately — a different memory class, a different pole).
- The cold-bootstrap milestone and case study — the empirical record of
  fail-closed starving when constraints are absent.

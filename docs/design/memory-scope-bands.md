# What Sensei covers, and why it is small: the three bands

*Design only — a scope statement, not a feature. No engine, rule, or graph
changes are made by this document. It records which band of software-engineering
knowledge Sensei is responsible for, which bands it deliberately refuses, and why
the combination **agent + Sensei** covers almost the whole surface by construction.*

## The claim

> **Sensei is the complement of the agent, not a re-implementation of it. It covers
> exactly the one band of knowledge the agent structurally cannot hold — durable,
> system-specific judgment — and refuses the two bands the agent already owns.**

The temptation is to ask "does Sensei cover software engineering?" and conclude it
falls short. That is the wrong frame. The right question is "does **agent + Sensei**
cover it?" — and the answer is *almost all of it*, because the surface divides
cleanly into three bands, each with a natural home.

## The three bands

Sort everything an engineer or coding agent knows by **where it should live**:

| Band | What it is | Home | Provided by |
|---|---|---|---|
| **1. Universal knowledge** | What a REST API is, how a for-loop works, what a deadlock is, idiomatic Go | model weights | **the agent** |
| **2. Durable, system-specific judgment** | *This* system's structure, contracts, decisions, norms, and their grounding | **Sensei** | **the external graph** |
| **3. Transient session state** | Today's stack trace, the current bug, what we just tried | context window | **the agent** |

- **Band 1** is already in the weights. Storing it in Sensei would be redundant —
  the model has it. *Don't capture it.*
- **Band 2** is the intersection of **durable** and **system-specific**: the
  things true of *this* system that *don't* change every run, and that are *not*
  in any textbook. Components, Boundaries, import structure; Contracts (what must
  hold); Decisions (why it is this way); MetaPrinciples (how to act *here*);
  Evidence (what grounds it). This is precisely what is **lost between agent
  sessions** and **not in the weights**. *Capture it.*
- **Band 3** evaporates by design and should. Persisting it would pollute the
  store with state that is false by tomorrow. *Don't capture it.*

Sensei owns band 2 and band 2 only. That is not a gap; it is the thesis. The
valuable memory is the band the weights do not hold and the context cannot keep.

## Why agent + Sensei closes the surface

The agent brings bands 1 and 3 — its weights *are* the universal knowledge, its
context window *is* the transient working memory. Sensei supplies band 2. So:

> **agent + Sensei ≈ the whole knowledge surface, by division of labor.**

This is *why Sensei is small*. It is not trying to be a model of software
engineering; it is the **complement** of one. Every byte it stores is a byte the
agent structurally cannot carry. Anything the agent already holds, Sensei omits on
purpose.

## What this leaves out (deliberately)

Even within software engineering, Sensei does not store large surfaces — and each
omission lands in band 1 (the agent can reason about it) or band 3 (the agent
fetches it live), so the *combination* still covers it:

- **Runtime / operational dynamics** — perf, incidents, live traces. Observed
  *band 3* state, fetched at read time (e.g. via Globular's MCP), not durable
  judgment. A different memory class — see
  [memory-correctness-tradeoff](memory-correctness-tradeoff.md).
- **Process** — tickets, planning, estimation. The agent reads these live; not
  system judgment.
- **Requirements / product intent** — what the software *should* do. Arrives in
  band 3 from the human, or via the separate, weakly-grounded intent-mining
  channel.
- **Code mechanics** — syntax, algorithms. Band 1: left to the model and the
  compiler.
- **Tacit skill** — debugging intuition. Not expressible as typed,
  contract-bound nodes; lives in the weights or nowhere.

None of these is a hole in *agent + Sensei*. They are simply not band 2.

## The honest "almost"

The combination covers the *surface*. It does not guarantee the surface is
*correct* or *populated*. Three residuals remain, and they are about correctness
and capture, not coverage:

1. **Coverage is not correctness.** Sensei can hold a stale norm; the agent can hold
   a wrong belief. The pair spans everything without guaranteeing any of it is
   right. This is the entire reason for the fail-closed write gate, Evidence, and
   the freshness/ownership audits — they defend band 2's *correctness*, which
   coverage alone does not buy.
2. **Band 2 is potential, not automatic.** It only fills if something *writes* to
   Sensei. The cold-bootstrap finding is exactly this: no grounding channel → band 2
   stays empty → the agent falls back to guessing. Coverage is a capability, not
   a state.
3. **Genuinely novel judgment must be *generated*.** The first time anyone faces
   a problem, no band holds the answer — it is neither in the weights nor yet
   written down. That is the irreducible creative residual the architecture
   *enables* (by freeing the agent from re-deriving bands 1–3) but does not
   *contain*.

So the precise statement is:

> **Agent + Sensei covers almost all of the software-engineering knowledge surface,
> by dividing it so each part lives where it belongs. What is left over is not a
> fourth band — it is the gap between covering something and getting it right, and
> the work of writing band 2 down in the first place.**

## Related

- [memory-correctness-tradeoff](memory-correctness-tradeoff.md) — where each band
  pays for correctness (write-time vs read-time), and why band 2 (mostly
  behavioral) is forced to the write-time pole.
- [contract-spine-v1](../contract-spine-v1.md) — the band-2 model itself
  (Contracts, Invariants, Evidence) and the "Evidence is overloaded" note.
- The cold-bootstrap milestone and case study — the empirical record of band 2
  failing to populate when no grounding channel exists.

# Keeping Control of Your Codebase in the Age of AI

*A draft on why the Awareness Graph exists, and how it changes what an AI agent does before it writes a line.*

---

## The new failure mode

For most of software's history, the scarce resource was the ability to write
code. You hired for it, you mentored it, you protected the people who had it.
That constraint is gone. An AI agent will write a clean, idiomatic,
well-tested patch for almost any well-scoped task, faster than any human, at
any hour.

What the agent will *not* do reliably is respect the parts of your system that
aren't in the code it's looking at.

It doesn't know that this `paid` flag can only be written after the payment
processor confirms — never from a local cache. It doesn't know that this
"convenient" `os.Getenv()` fallback was the exact shape of last quarter's
outage. It doesn't know that this heartbeat handler is only allowed to *observe*
state, never write it. Those rules are real, they are load-bearing, and they
live nowhere the agent can see: in a senior engineer's head, in a post-mortem
nobody re-reads, in a Slack thread from six months ago.

So the agent makes the reasonable-looking change. It passes review, because the
reviewer is also holding all of this in their head and it's late. It ships. It
breaks something three weeks later in a way that's expensive to trace back. And
because the agent has no memory, the *next* agent, in a fresh session, makes the
same class of mistake again.

This is the control problem of the AI era. It is not that agents write bad
code. It is that they write *plausible* code that quietly drifts your system
away from its own design, one obvious fix at a time — and no single commit ever
looks wrong.

## What "control" actually means here

Control is not slowing agents down. If your answer to drift is "review
everything more carefully," you've just moved the bottleneck back onto the one
resource AI was supposed to free up — senior attention — and you'll lose that
race as agent throughput climbs.

Real control means the architectural knowledge that currently lives in your
best people's heads becomes **explicit, versioned, and queryable at the exact
moment a change is being made** — by a human or an agent, it doesn't matter
which. The rule reaches the edit *before* the edit happens, not in a document
someone might read, not in a review someone might catch.

That is the entire thesis of the Awareness Graph (AWG).

## How AWG changes what an agent does

An ordinary agent's loop is: read the task, read some files, write a patch.

With AWG wired in, one step gets inserted that changes everything downstream:

```
$ awg briefing -file src/payment_processor.py -task "refactor mark_paid"

Direct invariants:
- [critical] payments.paid_state_requires_processor_confirmation —
  An order records as paid only after processor confirmation,
  never from a local cache write

Forbidden fixes:
- write_paid_flag_from_cache_for_speed

Required tests:
- TestPaidRequiresProcessorConfirmation

Failure modes:
- [critical] payments.double_charge_on_cache_replay
```

Before touching the file, the agent asks the graph *what it needs to know*, and
gets back the invariants that apply, the fixes that are known-broken, the tests
that must still pass, and the incidents that this file has a history of causing.

Three things about that output matter more than they look:

1. **It is deterministic.** It is not an LLM guessing what might be relevant.
   It is compiled from YAML your team wrote into an RDF graph and queried in
   about two milliseconds. Same file, same answer, every time. You can trust it
   the way you trust a compiler, not the way you trust a chatbot.

2. **It encodes forbidden fixes, not just rules.** The most valuable thing you
   can tell an agent is often the *obvious* solution that is secretly wrong —
   the env-var fallback, the cache write, the pass-through wrapper that skips a
   verification step. "Don't do X" is more actionable than "do Y," because X is
   exactly what a fast, confident, historyless agent reaches for first.

3. **It can be enforced, not just offered.** A hook can require the briefing
   before an edit to a high-risk path is allowed to land. The agent cannot skip
   it on the grounds that the fix is "too simple to need checking" — which, we
   learned the hard way, are precisely the edits that violate a critical
   invariant. A one-function wrapper that looked purely mechanical once shipped
   without a manifest check because it seemed too obvious to verify. AWG exists
   so that "too obvious" is never an exemption.

The net behavioral change: the agent stops being a brilliant stranger who
wandered into your codebase this morning, and starts behaving like someone who
has read every post-mortem you've ever written.

## The pieces you actually write

AWG is not magic and it is not a model. It is a small set of YAML files in your
repo that you own, version, and edit — plus a compiler and a query layer.

- **Invariants** — rules that must always hold. *"Session tokens must use
  HttpOnly cookies."* Each names the files and symbols it protects, the
  forbidden fixes that violate it, and the tests that pin it.

- **Failure modes** — incidents that happened or could happen: the symptom, the
  root cause, the *correct* architectural fix, and the tempting-but-wrong fixes
  to refuse. The best entries come straight out of a real post-mortem.

- **Incident patterns** — dangerous *edit shapes*. "If you're adding an env-var
  fallback for local convenience, watch out — here's the failure mode it
  triggers and the test that catches it."

- **Activation rules** — when awareness is *required* versus optional, and what
  to do when the graph has nothing to say. A typo fix in a safe file proceeds
  silently; a logic change inside a high-risk directory with an empty briefing
  is treated as a degraded signal and escalated, not waved through. Empty is not
  the same as safe, and AWG refuses to pretend it is.

Every incident you encode makes the graph denser. After fifty of them you have
a map of your codebase's danger zones that every future edit — human or agent —
inherits for free, from its very first change.

## The meta-principles: knowing where the next bug hides

Encoding fifty incidents from a real platform, a pattern emerged: they weren't
fifty unrelated bugs. They were the same handful of *shapes* recurring. A
fallback that hides a failure by returning the real response's shape. A write
with no cleanup path. Two writers racing on one field. An intermediate state
that satisfies a "done" check before it's actually done.

AWG distills these into ~130 domain-independent **meta-principles** across eight
categories — authority, signal, lifecycle, dependency, perception, composition,
structure, evolution — and ships them as seed content with `awg init`. They're
queryable on day one, before you've written a single project-specific rule.

Their real power is as a *lens after an incident*. "This bug violated
`meta.fallback_must_degrade_semantics`. Where else do we return the primary
response's shape from a fallback path?" The principle points you at code that
hasn't broken yet but is built the same wrong way. It turns one fixed bug into a
search for its unfired siblings.

## Does it actually work? The evidence so far

Claims about "context helps agents" are cheap. The honest question is whether
*this specific* architecture-aware context produces measurable gain over an
already-capable agent that simply has good tools. We ran a controlled pilot to
find out, and we're going to report it warts-and-all.

The setup was a Multi-SWE-bench Go pilot: ten real issue-resolution tasks from
`github.com/cli/cli`, run under four conditions with the model, checkout, issue
text, budget, and test command all held constant. Only context and tools
changed:

- **Mode A** — agent alone (isolated model, no tools)
- **Mode B** — agent + normal tools (the honest apples-to-apples baseline)
- **Mode C** — agent + AWG
- **Mode D** — an experimental contract-first lane

Scoring was 100 points: 40 from mechanical test-pass verification, 60 from
judged localization, minimality, architecture-safety, test quality, and
explanation. The mechanical lane is a separate, non-prose signal, which matters
— it means the result doesn't rest only on an LLM judge's opinion.

The headline numbers:

| Mode | Avg score | Full test-pass tasks | Test cases passed |
|------|-----------|----------------------|-------------------|
| A — agent alone     | 71.7 | 3/10 | 88/144 |
| B — agent + tools   | 86.7 | 5/10 | 127/144 |
| **C — agent + AWG** | **92.2** | **7/10** | **135/144** |

Read that carefully, because the interesting comparison is **C versus B**, not
C versus A. Of course tools beat an isolated model — Mode B already jumps 15
points over Mode A. The question AWG has to answer is whether it adds anything
*beyond* giving the agent tools.

It does. Mode C improved the average judged score from 86.7 to 92.2, lifted
full test-pass tasks from 5/10 to 7/10, and — in the cleanest cut of the data —
beat the plain tool-assisted baseline on **7 of 10 tasks**, tied or near-tied on
one, and lost narrowly on two. On a per-task basis the wins were sometimes
large (+21, +14, +10 points) and the losses were small (−1). The gain is not
"tools help." It is "project-aware governance helps *above* a tool baseline."

What Mode C added, concretely, was a **briefing** (project-specific context
around the files and task), **impact/resolve** guidance (related files, symbols,
and architectural blast radius), and **preflight** pressure against drifting
outside the intended contract or authority boundary. The observable result in
the runs: fewer wandering edits, stronger localization, and more tasks where
*all* the tests passed.

### The honest limits

We are not going to oversell ten tasks.

- **The sample is small** — ten tasks from one Go repository. It's a strong
  signal, not universal proof. The next benchmark needs 25–50 tasks across at
  least three repositories.
- **60 of 100 points are judged.** The mechanical test lane is separate and
  clean, but a majority of the score still depends on an LLM judge.
- **Mode D is a prototype, not the headline.** Completed Mode D runs scored very
  high (94.8), but 4 of 9 records never received a final judged score, so the
  conservative zero-filled reading is 52.7. We're reporting it as an
  experimental lane with a high ceiling and a finalization bug, not as product
  proof. Reporting the ceiling and hiding the finalization failures would be
  exactly the kind of "fallback that returns the real shape" the meta-principles
  warn against — so we don't.
- **The result validates the Mode C *bundle*,** not which individual feature
  (briefing vs impact vs preflight) drove most of the gain. Ablations are the
  next step.

Confidence, stated plainly: **medium-high** that AWG helps on this benchmark
slice, **medium** that it generalizes across repos, **low** that the
contract-first lane is production-ready. That's the honest shape of the evidence
today, and we'd rather you trust the method than the marketing.

## Why this is a control tool, not just a quality tool

It would be easy to file AWG under "makes agents write better patches." That's
true but it undersells the point.

Architecture drifts one simple fix at a time, and the damage is invisible in
isolation. By the time drift is visible in production, reversing it means
reconstructing intent that was never written down. AWG's real job is to **record
the intent and enforce consultation against it at the point of change** — so the
drift never accumulates in the first place.

That's what keeps a team in control as agent throughput scales past what any
review process can hold in its head:

- **The knowledge stops living in people.** It's YAML in your repo, versioned
  with the code, readable and editable by anyone — not a SaaS, not a model's
  hidden context, not one senior engineer's memory.
- **It's tool-agnostic and local.** A CLI plus a local gRPC server. Use it from
  Claude Code, Cursor, Codex, a CI step, or a plain shell. No cloud, no account.
  `-no-seed` keeps the graph 100% yours.
- **It compounds.** Every incident you encode raises the floor for every future
  edit. The institutional memory that used to walk out the door when someone
  left now stays in the repo and reaches the next agent on its first commit.

The agents aren't going away, and you don't want them to — they're the most
productive contributors you have. AWG is how you let them run at full speed
without letting your architecture drift out from under you. It gives the fast,
confident, historyless contributor the one thing it's missing: your project's
memory, delivered at the exact moment it's about to matter.

---

*AWG is open source: [github.com/globulario/awareness-graph](https://github.com/globulario/awareness-graph). It began inside [Globular](https://github.com/globulario), a distributed platform where these principles were validated against real production incidents, and now runs standalone for any codebase. Benchmark figures are from the Multi-SWE-bench Go pilot (10 tasks, `cli/cli`); see the full report for per-task tables and source notes.*

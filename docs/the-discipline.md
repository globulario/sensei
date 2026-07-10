# The discipline

AWG is tooling. This is the thing the tooling can't teach you: **how to think
so that the tooling has something true to enforce.**

The graph holds *what* your project knows. The discipline is *how you work* so
that what it knows stays honest as a thousand AI edits — and a few human ones —
wash over it. Learn the seven moves below and the rest is mechanical: the gates
reward the discipline and punish its absence, automatically, so nobody has to
police anyone.

Each move is illustrated with a real moment from the days this system was built,
because the discipline is easier to copy than to define.

---

## 1. Encode intent before code

Write the rule before you write the thing it governs. Brief before you edit.
Author the *law of the page* before you build the page.

This feels backwards the first time. It isn't. The moment you are most certain a
change is "too simple to need a briefing" is exactly the moment you are most
likely to miss the invariant that makes it dangerous. Intent written after the
code is a description; intent written before is a constraint.

> **From the build:** the ObjectStore-topology screen contract — every claim
> bound to its authority, every guard named, the closure chain spelled out —
> existed as a document *before a single component*. When someone finally
> builds that screen, the law is already there to confront them.

**The tool's part:** `awareness_briefing(file=…)` runs in ~10ms and surfaces
the invariants before the edit. Hooks make it non-optional in high-risk paths.

---

## 2. Declare what you do NOT enforce

You will not be able to mechanically prove every principle. That is fine. What
is *not* fine is letting an unenforced principle look enforced. An unattested
gap is a lie; a tracked gap is honesty.

Name the tier of every rule: is it gated by a scanner, by a test, by a
declaration — or by nothing but human review? "Review-only" is a legitimate,
terminal answer for principles no artifact can prove. But it must be *declared*
as that, out loud, in a file someone reviews.

> **From the build:** every meta-principle has an enforcement tier in
> `docs/awareness-control/meta_principle_coverage.yaml`. A new principle cannot land *unclassified* —
> the coverage gate fails until you say how it is, or is not yet, protected.

**The tool's part:** `TestMetaPrincipleCoverage` fails on any principle with no
declared tier. Naming the absence is required, not optional.

---

## 3. Mechanize over declare

A principle is advice until it is a gate. Advice rots; gates bite. The single
highest-value move in this whole practice is taking a rule that lives in prose
and turning it into a check that fails the build.

You will not be able to mechanize everything at once, and you should not try.
But every time a real codebase gives you the chance to convert one principle
from *review-only* to *enforced*, take it. That is how the discipline compounds.

> **From the build:** `meta.ui.theme_tokens_must_encode_roles_not_preferences`
> was review-only philosophy — until a real frontend (globular-admin) showed
> 520 raw color literals, including two different greens both meaning "healthy."
> A 30-line bash ratchet froze the count and wired it into CI. The principle
> grew teeth the day a real codebase existed to bite.

**The tool's part:** none, here — this is the move only a person can make. The
tool just records the result and lowers the ratchet when you do.

---

## 4. Keep the enforced-to-declared ratio honest — with a ratchet

Discipline that relies on *remembering* to be disciplined fails. The slow death
of any principle set is the unenforced pile quietly becoming the majority, one
reasonable-sounding "we'll mechanize it later" at a time, until the whole thing
is a beautiful document that protects nothing.

So measure it, and make backsliding a build failure. Not a hard threshold —
some principles are irreducibly review-only — but a ratchet: the count can only
go *down* without a conscious, reviewable decision to let it rise.

> **From the build:** `enforcement_ratchet.max_review_only`. Add a review-only
> principle and the build fails until you either mechanize one to make room or
> deliberately bump the ceiling — a number that shows up in the diff and asks
> "did you mean to make the unenforced pile bigger?" The first time it moved,
> it moved *down*: 43 → 42, because a principle got mechanized.

**The tool's part:** the ratchet test prints the ratio every run and fails on
silent growth. The number stays in front of you.

---

## 5. Surface, don't proceed

On work that is not yours, and on anything hard to reverse: stop and say so.
Do not silently fix someone else's in-flight code. Do not quietly bury a
mismatch. Do not delete what you did not create. The honest move when you find
something off is to *name it and hand the decision back* — not to barrel
through and hope.

This is the move that protects trust, which is the only thing that makes a
shared knowledge layer worth sharing.

> **From the build:** a `git add -A` once swept two unreviewed engine files
> into a commit labeled "docs only." The fix was not to quietly rewrite history
> — it was to write `commit-integrity-notes.md`, correct the record forward,
> and add a CI guard so the *class* of mistake can't recur. The dishonest
> commit became a gate.

**The tool's part:** `check-commit-scope.sh` fails a "docs only" commit that
touches engine paths. The discipline, once learned, became enforcement.

---

## 6. A red gate is telling the truth

When a check goes red, your first instinct will be to make it green. Resist the
version of that instinct that papers over the failure. A red gate is almost
never the gate being wrong — it is the gate refusing to pretend a broken
contract is whole. The job is not to silence it. The job is to make the thing
it's complaining about actually true.

> **From the build:** an ontology drift test stayed red for a whole day because
> a property (`aw:memberOfGroup`) existed in code but not in the ontology. It
> wasn't a flaky test to suppress — it was a genuinely incomplete contract. The
> fix was one declaration that made the contract whole, and the test went green
> for the honest reason.

**The tool's part:** the gates don't lie. Every one that fired during this
build fired on something real. Trust that.

---

## 7. Generic knowledge lives in the shared layer; anchoring stays local

What you *learned* belongs to everyone. Where it *bit you* belongs to you.

A principle like "a fallback must not look like confirmed truth" is universal —
it should live in the shared tool, portable to any project. The fact that *your*
release pipeline violated it in a specific file is local provenance — it stays
in your project. Keep the two separate, and the knowledge travels; mix them, and
every consumer inherits your project's scars.

> **From the build:** the meta-principle corpus moved out of the Globular
> project and into awareness-graph — general knowledge the project uses, not
> project knowledge that happens to be general. The few code anchors that came
> with it are documented as *provenance* ("validated here; replace with yours").
> A stranger cloning the tool now gets the whole corpus, not a leftover copy.

**The tool's part:** `awg init` ships the portable pack to any project; the
project authors its own local invariants on top.

---

## Why "the rest will work"

These seven aren't rules the tool enforces *on* you. They're the way of working
that makes the tool *worth* having. Internalize them and something quietly
profound happens: you stop needing to police anyone. The agent that briefs
before editing, declares what it can't prove, mechanizes when it can, and
surfaces what isn't its to bury — that agent produces a graph that stays true.
And the gates make every lesson permanent, so it only has to be learned once.

The revolution isn't agents writing more code faster. It's codebases that can
absorb a flood of AI-assisted change without losing the design that makes them
work — because the knowledge that keeps them coherent is explicit, enforced, and
honest about its own gaps.

Teach the discipline. The rest is mechanical.

---

## The secret

There's an old line, the closest thing software has to a fundamental theorem:

> *"All problems in computer science can be solved by another level of
> indirection."* — David Wheeler

That is what makes this whole thing work, and it's worth saying plainly at the
end, because it's the trick hiding under everything above.

**AWG is that level of indirection.** It is a layer placed *between the agent
and the code* — a place where the project's intent, invariants, failure modes,
and hard-won lessons can live as something queryable, instead of dissolving into
a model's context window or a senior engineer's memory. The agent no longer has
to *carry* the architecture in its head to avoid breaking it. It asks the
indirection layer one question — *"what do I need to know before editing this
file?"* — and the truth is handed back. The knowledge stopped depending on who
happens to be looking.

Every move in this document is really just *using that indirection well*: encode
intent into the layer before you act; declare honestly what the layer can and
can't prove; push truth down into gates the layer enforces; keep the layer's
own books honest; let the layer carry what no one person should have to.

But Wheeler's theorem has a famous second half, and the discipline lives in it:

> *"…except for the problem of too many levels of indirection."*

That caveat is not a footnote here — it's a principle the corpus enforces
(`meta.code.abstraction_must_be_deeper_than_its_interface`). Indirection earns
its existence only when it hides *more* complexity than it adds. A shallow layer
— one that wraps and renames and hides nothing — is the disease, not the cure.

AWG survives that caveat because it is a *deep* layer. Behind one tiny interface
(a file path, a question) it hides an enormous amount: every incident the
project ever metabolized, every rule someone learned the hard way, the whole
accumulated weight of "how this system actually works." That is the deepest a
single level of indirection can get — and it's why this one is worth having.

So that's the secret. Not the graph, not the gates, not the seven moves. The
secret is that the hardest problem in keeping a codebase coherent under a flood
of change — *holding its truth somewhere it can't be lost* — yields, like nearly
everything else, to one more level of indirection. You just have to make it a
deep one, and have the discipline to keep it honest.

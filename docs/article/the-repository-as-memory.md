# The Repository as Memory: A Knowledge Graph for AI-Assisted Engineering

*A draft, from an engineering perspective, on how Sensei turns invariants,
contracts, failure modes, incidents, and meta-principles into one graph the code
carries with it — the memory an AI agent inherits the moment it enters the repo,
and the reason the code it writes is correct rather than merely plausible.*

---

## 1. The problem is not the code. It's everything the code doesn't say.

Open any source file and you are looking at the visible tip of a much larger
structure. Under the file sits a web of decisions the file depends on but never
states. Which actor *owns* this piece of state — is this code allowed to write
it, or only to observe it? Which endpoint is the sole legal path to mutate it?
What guarantee is this function on the hook for, and what test is the only thing
standing between this line and a regression? Which "obvious" fix looks correct
but silently reopens an incident that took a patch release to close last quarter?

Human engineers accumulate that structure slowly, and painfully. It is what
"knowing a codebase" actually means: not memorizing the code — you can re-read
the code — but internalizing the constraints *around* the code. That takes
months. Most of it is never written down anywhere. It lives in the heads of the
three people who were on the incident call, in a post-mortem nobody re-reads, in
a review comment that scrolled out of history.

An AI agent has none of it. It arrives at your repository every session with a
flawless reading of the syntax and zero memory of the architecture. It will
produce a patch that compiles, passes the tests it can see, reads beautifully,
and quietly violates a rule that was in none of the files it opened. This is
**not** a model-quality problem you can wait out. A stronger model reads the code
*better*; it still cannot read what the code does not contain. The knowledge was
never in the repository to be read.

Sensei is an attempt to fix that at the source: to write the
missing structure down in a form a machine can traverse, and to attach it to the
repository, so that *entering the repo is loading the memory*. Not a prompt you
hope the agent skims. A typed, queryable graph the agent consults — and, in the
paths that matter, is forced to consult — before it writes.

This article is about what's actually in that graph: the node types, the edges,
and why each one exists. It is deliberately concrete. Every principle quoted
below is real, shipped in the seed corpus, and carries the scar of the incident
that produced it.

## 2. The unit of correctness is the contract, not the file

The first thing Sensei does is refuse the file as the unit of reasoning.

A file is an implementation detail. What actually has to stay true is a
**contract** — a guarantee some part of the system owes to the rest of it.
*"Config is never mutated without a valid authority token." "An order records as
paid only after the processor confirms, never from a local cache." "This
heartbeat handler observes runtime state; it never writes it."* Files come and
go, get split, get renamed, get rewritten by an agent at 2am. The guarantee has
to survive all of that.

So the graph is organized around contracts, and every other node type exists to
answer one of four questions an engineer — or an agent — should be forced to
answer before changing anything load-bearing:

> *What rule does this code owe? Why does that rule exist? What proves it is
> honored? Do I dare claim I've resolved the task without breaking it?*

Hold those four questions in mind. The entire ontology below is just a machine
that answers them in under two milliseconds, deterministically, for whatever file
you're about to touch.

## 3. Meta-principles: the generative laws that predict where bugs hide

Start with the layer you get for free, on day one, before you have written a
single line of project-specific knowledge: the **meta-principles**. There are 133
of them in the seed pack, and they are the distilled *shapes* of real production
failures — not style advice, not linting opinions, but the recurring architectural
mistakes that a fast, confident, historyless contributor reaches for first.

They came from a hard-won observation. Encoding dozens of incidents from a real
distributed platform, the incidents were not dozens of unrelated bugs. They were
the *same handful of shapes*, recurring: a fallback that hides a failure by
returning truth's shape; a write with no cleanup path; two writers racing on one
field; an intermediate state that satisfies a "done" check before it is done.
Once you can name the shape, you can predict where the next instance is hiding.

Each meta-principle is authored as a structured record, not a sentence. Here is
one, verbatim from the corpus, because the structure is the point:

```yaml
- id: meta.fallback_must_degrade_semantics
  title: "Fallback must degrade semantics — a fallback that returns the
          same shape as truth will be mistaken for truth"
  category: signal
  antipattern: "Return something rather than nothing"
  wrong_instinct: Helpfulness over honesty — returning a value feels better
    than returning an error, but a fake value propagates silently
  severity: critical
  summary: |
    Fallbacks may preserve availability, but they must not preserve the
    illusion of certainty. A fallback value must not return through the same
    field/type/shape as canonical truth unless explicitly marked degraded…
    When a function returns "" for a missing cluster_id, or 0 for a missing
    build_number, the caller cannot distinguish "canonical answer is empty"
    from "couldn't reach the owner." The type is the same, so the lie propagates.
  enforcement: |
    Fallback return paths must use a distinct type, wrapper, or status field
    that the caller can inspect. Returning zero-values or empty strings through
    the same type as canonical truth is forbidden when the source is a fallback.
```

Look at the fields. The `antipattern` is the seductive one-liner an agent
believes ("return something rather than nothing"). The `wrong_instinct` names
*why* a competent engineer falls for it — helpfulness over honesty — which is
exactly the reasoning an LLM will reproduce, because it was trained to be
helpful. The `summary` grounds the abstract law in a concrete failure signature
(`return ""` for a missing `cluster_id`). The `enforcement` states the mechanical
rule a gate or a reviewer can actually check. This is not a proverb. It is a
diagnosis, a mechanism, and a remedy, in a shape a machine can serve at edit time.

The 133 principles fall into eight categories, and the categories are themselves a
map of where correctness lives in a system:

**Authority (20) — "Who owns this truth, and is this code that owner?"** The
foundational principle is `meta.storage_is_not_semantic_authority`: *"A shared
datasource (etcd, ScyllaDB, MinIO, local files) is only a source of durable
record. It is not automatically the source of truth. Truth belongs to the actor
that owns the semantic meaning of the state."* The wrong instinct it names is the
one every agent has: *"I can see the data so I own it."* The agent has read
access to the database, so it writes the database directly — bypassing the
service that owns what the row *means*. It compiles. It passes. It corrupts an
invariant no test in front of the agent could see. Authority also holds
`meta.identity_computation_must_be_invariant`: one field, one meaning, one
canonical computation everywhere — the trap where `entrypoint_checksum` is
`sha256(artifact bytes)` at publish time but `sha256(installed binary)` at verify
time, so the same field name silently becomes two different computations and the
verifier reports "mismatch" on a correctly installed binary.

**Signal (19) — "Is the truth arriving intact, or is something faking it?"**
`meta.fallback_must_degrade_semantics` (above) lives here, alongside
`meta.authority_must_express_uncertainty` (*"if the owner cannot say 'unknown',
callers will turn silence into lies"*) and `meta.absence_scope_must_be_explicit`
(*"'not found where' is not the same as 'does not exist'"* — a cache miss is not
proof of global absence, and acting on it as if it were is how you get a spurious
deletion).

**Lifecycle (38 — the largest category) — "Will this operation actually
complete?"** This is where most real outages live, which is why it is the biggest
bucket. `meta.write_creates_completion_obligation`: *"When code writes a state
record (etcd key, Scylla row, lock, status entry), it creates an obligation.
Either the same actor completes the lifecycle, or there is an explicit sweep that
clears stale records, or the record has a TTL. Half-written state that nobody owns
the cleanup for becomes a permanent stall."* And it does not stay abstract — the
principle names real offenders and real exemplars:

```
Known violations in this codebase:
  - writeCriticalKeyBlock: NO cleanup function exists at all
  - writeRuntimeDepBlock: sweep exists but was edge-triggered only (INC-2026-0005)
Known good examples:
  - ConvergenceCommitter: atomic txn — write + cleanup in same etcd op
  - Workflow defer B3: durable counter + Abandoned flag + ClearOnSuccess
```

Its sibling `meta.half_done_must_not_look_done` carries `INC-2026-0012`: a
completeness check that looked at `artifact_state` but not `manifest_json`, so a
partial write satisfied the "already installed, skip it" predicate and every
interruption became permanent. And `meta.silence_is_not_valid_for_unexpected`:
a `switch` with no `default`, silently no-op'ing on an unrecognized case — *"how
a system stops making progress without anyone noticing."*

**Dependency (7) — "What breaks if something non-critical is slow or down?"**
*"The critical path must not depend on non-critical services — a dependency you
don't need will kill you on the recovery path."* *"Every circular dependency must
have a break-glass path that doesn't go through the cycle — a system that deploys
itself must have a path that doesn't go through itself."* *"An actor's response to
network partition must be decided BEFORE the partition occurs — choosing in the
moment is the bug."*

**Perception (19) — "Is the UI telling the truth about system state?"** *"Every
screen claim binds to the correct authority — desired, cached, optimistic, and
confirmed state must not collapse into one visual meaning."* *"Certainty is part
of the value — loading, stale, unknown, optimistic, and confirmed must each look
like what they are."* Correctness does not stop at the API boundary; a green badge
over stale state is a lie the same way a faked fallback is.

**Composition (7) — "Does the layout make that truth perceivable?"** *"Visual
weight, order, spacing, and grouping must match the operator's decision path —
safety evidence outranks decoration, always."* *"Spacing is information — equal
spacing makes unrelated facts look related."*

**Structure (12) — "Is this unit shaped to last?"** *"A reusable unit is a stable
semantic concept — explicit contract, hidden complexity, owned lifecycle,
inspectable behavior."* *"Contracts outlive implementation fashion — callers
depend on semantic inputs/outputs/events, never private structure."* *"Hide
complexity, never truth."*

**Evolution (11) — "How may the project change safely over time?"** *"Main branch
must remain releasable — every merge preserves build, tests, graph validity, and
artifact freshness."* *"A large or risky change must decompose into reviewable,
behavior-preserving slices."* *"A high-risk change must carry test evidence — or a
documented, owned, expiring exception."* *"Generated artifacts must be fresh
against their source before merge."*

These principles change what an agent *does*, in three distinct modes:

1. **Brief before editing.** When the agent opens a file, the briefing surfaces
   the principles that apply to that file's *role* — authority code gets the
   ownership principles, UI code gets the perception ones — *before* the agent
   reaches for the obvious fix. The obvious fix is usually the antipattern, and
   the principle names it as such at the exact moment of temptation.
2. **Classify an incident.** When something breaks, the principle is the label:
   *this was a `meta.fallback_must_degrade_semantics` violation.* That is not
   bureaucracy; it is what makes step three possible.
3. **Find the siblings.** A classified bug predicts where the *same class* is
   hiding in code that has not broken yet. *"Where else do we return the primary
   response's shape from a fallback path?"* One fixed bug becomes a search that
   fixes five, in files nobody was looking at. This is the single highest-leverage
   move in the whole system, and it is only possible because the knowledge is
   structured rather than prose.

## 4. Invariants: meta-principles with an address in your code

Meta-principles are universal. Your codebase is not. An **invariant** is what you
get when you ground a universal law at a specific address in your repository:

```yaml
invariants:
  - id: payments.paid_state_requires_processor_confirmation
    title: An order records as paid only after processor confirmation,
           never from a local cache write
    severity: critical
    status: active
    protects:
      files: [src/payment_processor.py]
      symbols: [mark_paid]
    forbidden_fixes:
      - write_paid_flag_from_cache_for_speed
    required_tests:
      - TestPaidRequiresProcessorConfirmation
    related_failure_modes:
      - payments.double_charge_on_cache_replay
```

An invariant is a meta-principle made local and enforceable. Where
`meta.storage_is_not_semantic_authority` says *"truth belongs to the owning
actor,"* the invariant says *"in this repo, in `payment_processor.py`, the
`mark_paid` symbol, here are the exact forbidden fixes and the exact test."* Three
fields do the heavy lifting:

- `protects` gives the invariant an **address** — files and symbols — so the graph
  can attach it to the code and the briefing can find it by the path the agent is
  about to edit.
- `forbidden_fixes` is the most valuable field in the entire schema. It encodes
  the *obvious solution that is secretly wrong* — the cache write "for speed," the
  env-var fallback "for convenience." Telling an agent "don't do X" is far more
  actionable than "do Y," because X is precisely what it will reach for.
- `required_tests` names the proof. An invariant with no test is a hope; the test
  node is what lets "what must still pass?" be a query instead of a memory.

You do not need many. Five invariants drawn from your last five painful patch
releases is enough to start getting value on the very next edit.

## 5. Contracts: separating the guarantee from the surface that exposes it

Here is where Sensei gets genuinely more powerful than a rules file, and it is worth
slowing down for.

An invariant protects files. But the same guarantee is often exposed through a
concrete *surface* — a gRPC method, an HTTP route, a handler — and that surface is
what an agent actually edits. So Sensei splits the idea in two:

- An **architectural contract** is the semantic guarantee, stated independently of
  any endpoint: `contract.config_mutation_requires_valid_token` — *"config mutation
  requires a valid token."* This is the thing that must stay true no matter how
  many handlers come and go.
- An **implementation contract** is the executable surface that exposes it: the
  HTTP route `/api/save-config`, the `NewSaveConfig` handler that returns `401`
  without a token. Crucially, implementation contracts are **extracted, not
  authored** — a proto scan or an HTTP-route scan finds them mechanically from the
  code, so this layer stays honest to what the service actually exposes.

The link between them — *"this surface is the executable exposure of that
guarantee"* — is the `realizesContract` edge, and it is the most dangerous edge in
the graph, because a wrong one would make the briefing lie with authority. So Sensei
never infers-and-trusts it in one step. It uses a **safety ladder** from guess to
authority:

```
extractors find surfaces     proto-scan (gRPC) · http-scan (HTTP routes)
        ↓
generator proposes           sensei suggest-realizations   (candidates only, scored on hard evidence)
        ↓
human review confirms        sensei promote-realization    (explicit, one at a time)
        ↓
realizesContract = authority briefing speaks it; audit protects it
```

A conservative generator proposes `candidateRealizesContract` edges, scored on
hard evidence — shared source file, path-glob coverage, shared failure-mode
context, name-token overlap — and it *refuses weak evidence*: name overlap alone
yields nothing, a read-only surface won't be matched to a write-only contract, and
candidates never cross repository domains. Only an explicit human
`sensei promote-realization` — one reviewed pair at a time, never in bulk, never from
name overlap — turns a candidate into an authoritative `realizesContract`.
Promotion is a review action, never a scoring action.

This distinction is not pedantry. It is what lets the briefing be trusted *like a
compiler*. A graph that quietly promotes guesses to facts will eventually lie to
the agent at exactly the wrong moment. The ladder keeps the authoritative core
clean and keeps the speculative layer clearly, visibly labeled as speculative —
which you will see the agent is actually told, in §7.

## 6. Failure modes and incident patterns: the scar tissue

Invariants and contracts say what *must* be true. **Failure modes** say what has
gone *wrong*, and they are where a codebase's real institutional memory lives.

A failure mode is the full anatomy of an incident, not a one-line note:

```yaml
failure_modes:
  - id: config.env_var_overrides_production_setting
    title: Environment variable silently overrides production config
    severity: high
    symptoms:
      - "Service uses wrong database in production"
      - "Config store shows correct value but service ignores it"
    root_cause: |
      A developer added os.Getenv("DB_HOST") as a "convenient fallback" during
      local development. In production, a stale env var from a previous deployment
      overrode the config store value.
    architecture_fix: |
      Remove all os.Getenv calls for service config. Config store is the sole
      source of truth. If the config store is unavailable, the service must fail —
      not silently use a stale env var.
    forbidden_fixes:
      - add_precedence_logic_env_over_config
      - add_env_var_for_just_this_one_setting
    related_invariants:
      - meta.fallback_must_degrade_semantics
```

Notice what this captures that a code comment never could: the **symptoms** (so a
future engineer recognizes the failure when it recurs), the **root cause** (the
"convenient fallback" reasoning that felt right at the time), the **architecture
fix** (the *correct* remedy, which is often to remove code, not add it), and
again the **forbidden fixes** — the tempting patches (`add_precedence_logic`,
`add_env_var_for_just_this_one_setting`) that a well-meaning agent would propose
and that would make it worse. And the `related_invariants` edge ties the specific
scar back up to the universal law it violated — closing the loop between "what
broke here" and "the class of thing this is."

**Incident patterns** are the third member of this family, and they attack the
problem from a different angle: instead of describing a file, they describe a
dangerous *edit shape*:

```yaml
incident_patterns:
  - id: pat.env_var_fallback_added
    edit_shapes:
      - "Adding os.Getenv() for a service config value"
      - "Adding an env var fallback 'for local development'"
      - "Wrapping config lookup with env var override"
    failure_mode: config.env_var_overrides_production_setting
    lesson: |
      Environment variables are invisible, unversioned, and stale. A "convenient"
      env var fallback becomes the actual config source in any environment where
      that var happens to be set.
    wrong_fixes:
      - "Adding precedence logic (check config first, then env)"
      - "Documenting which env vars are 'safe' to use"
```

This is how the graph catches a bug you are *about to introduce*. It does not
wait for you to touch a known-bad file; it recognizes the *move* — "you're adding
an env-var fallback" — and surfaces the failure mode it triggers before you finish
the edit. Pattern-matching on the shape of a change, not the location, is what
lets Sensei warn about danger in code that has never broken before.

## 7. The edges are the knowledge — one worked traversal

Node types are just vocabulary. The knowledge lives in how they connect, and the
connections are typed, directional, and traversable. This is the Contract Spine —
the model that unifies everything above into a single graph:

```
MetaPrinciple / Invariant
        ▲ constrainedByInvariant
ArchitecturalContract  ◀── violatesContract ── FailureMode
        ▲ realizesContract            │ requiresTest
ImplementationContract               ▼
        ▲ exposesContract / implements   Test
Component / SourceFile
```

Follow one real path end to end and the value stops being abstract. An agent is
about to edit `internal/http/handlers/config.go`. It runs the briefing,
and the graph traverses:

```
contract.http.api_save_config              (the HTTP surface — extracted by http-scan)
  --realizesContract-->                     (promoted from a candidate by human review)
contract.config_mutation_requires_valid_token   (the guarantee the surface owes)
  --constrainedByInvariant--> meta.authority_must_express_uncertainty
  --requiresTest-----------> save_config_test.go:TestSaveConfig_RequiresToken
                             TestSaveConfig_InvalidToken401
                             TestSaveConfig_ValidTokenSaves204
```

From the source file it is touching → to the surface that file implements → to the
guarantee that surface owes → to the meta-principle that guarantee must satisfy →
to the exact tests that prove it. And from the opposite direction, a `FailureMode`
aims its `violatesContract` edge at the *same* guarantee, so the agent also
inherits *how this has broken before.*

What the agent literally receives is a repair instruction, with authority and
speculation clearly separated:

```
Realized architectural contracts (AUTHORITY — respect or do not claim resolution):
- HTTP /api/save-config realizes contract.config_mutation_requires_valid_token
  - The contract requires: Config mutation requires a valid token
  - Constrained by: meta.authority_must_express_uncertainty
  - Required proof: save_config_test.go:TestSaveConfig_RequiresToken, …
  - Do not claim resolution if this contract is bypassed, weakened, or left untested.

Candidate realized contracts (REVIEW-ONLY — not authority; promote with `sensei promote-realization`):
- HTTP /api/cors-diagnostics ~candidate~> contract.config_mutation_requires_valid_token
  (unverified — do not treat as a guarantee)
```

That traversal *is* the memory. It is what a senior engineer does in their head in
the half-second before they lean over and say "wait — don't do that, here's why."
Sensei makes it explicit, deterministic, and available to a contributor who has no
head-memory at all. And note the last line of the authority block: *"Do not claim
resolution if this contract is bypassed, weakened, or left untested."* The graph
does not merely inform the agent; it constrains what the agent is permitted to
*claim it accomplished*.

## 8. Governance: why the graph can be trusted like a compiler

A memory that can be quietly corrupted is worse than no memory, because you'll
trust it. So Sensei treats the *provenance* of every authoritative edge as part of
the engineering, and it wires that into the build:

1. **Candidates are never authority.** `candidateRealizesContract` renders in a
   separate, explicitly REVIEW-ONLY section and is never treated as a guarantee.
   The generator emits *only* candidates.
2. **Promotion is explicit and singular.** `realizesContract` is created only by a
   human running `sensei promote-realization`, one reviewed pair at a time — never in
   bulk, never from path or name overlap.
3. **Unproven authority fails the build.** The `contract-verification-wiring`
   audit check is **FAIL-level**: a home-domain contract that claims it requires
   verification but wires none of `requiresTest` / `constrainedByInvariant` /
   `violatedBy` / `detect` fails `sensei audit --check --domain <repo-domain>`. *Authority must carry its
   proof*, mechanically, or the gate goes red.

The consequence is a rule you can state in one line — *no resolution without a
respected contract* — that is simultaneously told to the agent at edit time and
enforced across the whole corpus by an audit gate. The briefing informs; the
audit protects; neither depends on anyone remembering to be careful.

## 9. The discipline: how you work so the graph stays true

Tooling holds *what* the project knows. It cannot hold *how you work* so that
what it knows stays honest as a thousand AI edits — and a few human ones — wash
over it. That is a practice, and it is worth stating because the graph is only as
trustworthy as the discipline that maintains it. Seven moves, each of which was
learned the hard way while the system was built:

1. **Encode intent before code.** Write the rule before the thing it governs.
   Brief before you edit. The moment you are most certain a change is "too simple
   to need a briefing" is exactly the moment you are most likely to miss the
   invariant that makes it dangerous.
2. **Declare what you do NOT enforce.** You cannot mechanically prove every
   principle, and that is fine. What is *not* fine is letting an unenforced
   principle look enforced. "Review-only" is a legitimate, terminal answer — but
   it must be *declared out loud*, in a file someone reviews. An unattested gap is
   a lie; a tracked gap is honesty. A coverage gate fails the build on any
   principle with no declared enforcement tier.
3. **Mechanize over declare.** A principle is advice until it is a gate. Advice
   rots; gates bite. `meta.ui.theme_tokens_must_encode_roles_not_preferences` was
   review-only philosophy — until a real frontend showed **520 raw color literals,
   including two different greens both meaning "healthy."** A 30-line bash ratchet
   froze the count and wired it into CI. The principle grew teeth the day a real
   codebase existed to bite.
4. **Keep the enforced-to-declared ratio honest with a ratchet.** The slow death
   of any principle set is the unenforced pile quietly becoming the majority, one
   "we'll mechanize it later" at a time, until you have a beautiful document that
   protects nothing. So measure it and make backsliding a build failure. Add a
   review-only principle and the ceiling test fails until you either mechanize one
   to make room or *consciously* bump the number in a reviewable diff. The first
   time that ceiling moved, it moved **down** — 43 → 42 — because a principle got
   mechanized.
5. **Surface, don't proceed.** On work that isn't yours, and on anything hard to
   reverse: stop and say so. Don't silently fix someone else's in-flight code,
   don't quietly bury a mismatch, don't delete what you didn't create. When a
   `git add -A` once swept two unreviewed engine files into a commit labeled "docs
   only," the fix wasn't to rewrite history — it was to correct the record forward
   and add a CI guard so the *class* of mistake can't recur. The dishonest commit
   became a gate.
6. **A red gate is telling the truth.** When a check goes red, the instinct is to
   make it green. Resist the version that papers over the failure. A red gate is
   almost never the gate being wrong — it is the gate refusing to pretend a broken
   contract is whole. An ontology-drift test once stayed red a whole day because a
   property existed in code but not in the ontology; the fix was one declaration
   that made the contract whole, and it went green for the honest reason.
7. **Generic knowledge lives in the shared layer; anchoring stays local.** What
   you *learned* belongs to everyone — "a fallback must not look like confirmed
   truth" is portable to any project. Where it *bit you* is local provenance and
   stays in your repo. Keep them separate and the knowledge travels; mix them and
   every consumer inherits your scars.

These aren't rules the tool enforces *on* you. They're the way of working that
makes the tool *worth* having — and once internalized, they mean you stop needing
to police anyone. The agent that briefs before editing, declares what it can't
prove, and surfaces what isn't its to bury produces a graph that stays true, and
the gates make every lesson permanent, so it only has to be learned once.

## 10. The trick underneath: a level of indirection

There is an old line, the closest thing software has to a fundamental theorem:

> *"All problems in computer science can be solved by another level of
> indirection."* — David Wheeler

That is what makes this whole thing work, and it is worth saying plainly. **Sensei is
that level of indirection, placed between the agent and the code.** It is a place
where the project's intent, invariants, contracts, failure modes, and hard-won
lessons live as something queryable — instead of dissolving into a model's context
window or a senior engineer's memory. The agent no longer has to *carry* the
architecture in its head to avoid breaking it. It asks the indirection layer one
question — *"what do I need to know before editing this file?"* — and the truth is
handed back. The knowledge stopped depending on who happens to be looking.

Every query is a traversal of that layer. `sensei briefing` answers *"what must I
know before touching this file?"* — files → invariants → contracts → failure modes
→ forbidden fixes → required tests. `sensei impact` answers *"what is the blast radius
of this change?"* — outward along the reference and contract edges. `sensei preflight`
answers *"am I about to drift outside the intended contract or authority
boundary?"* None of them call an LLM. They compile authored YAML into RDF triples,
load them into a local store, and answer with SPARQL: same repository state, same
answer, every time. The agent gets architecture-aware context with the reliability
of a build tool, not the variance of a second model in the loop.

## 11. Does it actually change behavior? The evidence

The claim that "context helps agents" is cheap. The honest question is whether
*this specific* architecture-aware layer produces measurable gain over an
already-capable agent that simply has good tools. We ran a controlled pilot to
find out, and we'll report it warts and all.

Ten real issue-resolution tasks from `github.com/cli/cli`, run under conditions
where the model, checkout, issue text, budget, and test command were held
constant. Only context changed:

- **Mode A** — agent alone (no tools): avg score 71.7, 3/10 tasks fully passing
- **Mode B** — agent + normal tools (the honest baseline): 86.7, 5/10
- **Mode C** — agent + Sensei: **92.2, 7/10**

The comparison that matters is **C vs B**, not C vs A — of course tools beat an
isolated model. The question is whether Sensei adds anything *beyond* tools. It does:
Mode C beat the plain tool-assisted baseline on **7 of 10 tasks**, tied or
near-tied on one, and lost narrowly on two, with per-task wins as large as +21,
+14, and +10 points against losses of only −1. On the separate mechanical test
lane — no LLM judgment involved — Mode C passed 135/144 test cases versus Mode B's
127/144. The gain is not "tools help." It is "project-aware governance helps
*above* a tool baseline": fewer wandering edits, stronger localization, more tasks
where *all* the tests pass.

The honest limits, stated plainly: the sample is small (ten tasks, one
repository — a strong signal, not universal proof); 60 of the 100 scoring points
are judged by an LLM, though the mechanical test lane is separate and clean; and
the result validates the Mode C *bundle*, not which individual feature drove the
gain. Confidence is medium-high that Sensei helps on this slice, medium that it
generalizes across repos. We would rather you trust the method than the marketing.

## 12. The repository becomes the memory

Here is the shift, stated once, plainly.

Today, when an agent enters your repository, the knowledge it needs to be
*correct* — not merely fluent — lives outside the repository: in your engineers'
heads, in a wiki nobody updates, in a model's context that vanishes at the end of
the session. So the agent is fluent and wrong, and it learns nothing between
sessions because there is nothing durable to learn from. The next agent repeats
the last agent's mistake.

With Sensei, that knowledge lives *in the repository*, as one typed graph, versioned
alongside the code it governs. Meta-principles encode the generative shapes that
predict the next bug. Invariants ground them at real addresses. Architectural
contracts encode the guarantees; implementation contracts bind them to the actual
surfaces; the safety ladder keeps that binding honest. Failure modes encode the
scars, incident patterns encode the dangerous moves, and tests encode the proof.
The edges between them encode the reasoning that used to exist only in a senior
engineer's intuition — and the audit gate keeps the whole thing from quietly
growing fake authority.

The repository stops being just the code. It becomes the code *plus the memory of
everything the code has learned* — and that memory is now something a fresh agent
inherits on its very first edit, the same way a new hire would, except instantly
and completely.

That is what it means to keep control of a codebase in an era where code gets
written faster than any one person can hold it in their head. You do not out-read
the agents. You give the repository a memory, wire it to fire at the point of
change, and let every contributor — human or machine — inherit it before the first
line is written.

---

*Sensei is open source: [github.com/globulario/sensei](https://github.com/globulario/sensei).
The node types and edges here are the Contract Spine v1 vocabulary
(`docs/contract-spine-v1.md`); all 133 meta-principles are documented in
`docs/meta-principles.md`; the practice is `docs/the-discipline.md`; the pilot is
the Multi-SWE-bench Go benchmark report. Sensei began inside
[Globular](https://github.com/globulario), where the meta-principles were
distilled from real production incidents (the INC-2026-\* references above are
those incidents), and now runs standalone for any repository.*

# Production graph served-generation authority

Written before the repair, from the census the code derives rather than from a
maintained list. Step numbering follows the commissioning brief.

Two laws are kept apart throughout, because collapsing them is how the first
version of this went wrong:

| law | question | owner today |
|---|---|---|
| endpoint ownership | *which* graph service is contacted | `resolveGraphReader` / `productionReaderFor` |
| served-generation authority | whether the returned graph state may be **trusted** | `graphReader.verifyServed`, at 6 of 20 call sites |

A correct endpoint is not authority. `sensei briefing` run from a subdirectory
reached a healthy live store on the right port and consumed a generation another
domain had published.

## Step 1 — the census, re-derived (not assumed)

Re-derived against this exact tree by `graphCommandsIn` in
`cmd/awg/graph_reader_test.go`, which parses every `cmd_*.go`, takes each `run*`
function that declares **its own** `addr` flag, and records two facts per
function. Nothing here is listed by hand.

```
subjects                                     20
resolve endpoint through the owner           20 / 20
compare the served generation                 7
do not                                       13
```

**Unchanged from the previous measurement**, so the "13" the brief names is still
the live number. The 20:

| # | subject | owner | compares |
|---|---|---|---|
| 1 | `cmd_briefing.go:runBriefing` | yes | yes |
| 2 | `cmd_gate.go:runGate` | yes | yes |
| 3 | `cmd_impact.go:runImpact` | yes | yes |
| 4 | `cmd_metadata.go:runMetadata` | yes | yes (reports) |
| 5 | `cmd_preflight.go:runPreflight` | yes | yes |
| 6 | `cmd_query.go:runQuery` | yes | yes |
| 7 | `cmd_resolve.go:runResolve` | yes | yes |
| 8 | `cmd_benchmark_brief.go:runBenchmarkBrief` | yes | **no** |
| 9 | `cmd_benchmark_score.go:runBenchmarkScore` | yes | **no** |
| 10 | `cmd_contract_bootstrap.go:runContractBootstrap` | yes | **no** |
| 11 | `cmd_edit_brief.go:runEditBrief` | yes | **no** |
| 12 | `cmd_edit_check.go:runEditCheck` | yes | **no** |
| 13 | `cmd_edit_guard.go:runEditGuard` | yes | **no** |
| 14 | `cmd_metadata.go:runDomains` | yes | **no** |
| 15 | `cmd_pattern_check.go:runPatternCheck` | yes | **no** |
| 16 | `cmd_repair_plan.go:runRepairPlan` | yes | **no** |
| 17 | `cmd_repair_report.go:runRepairGate` | yes | **no** |
| 18 | `cmd_repair_report.go:runRepairReport` | yes | **no** |
| 19 | `cmd_synthesis_run.go:runSynthesisRun` | yes | **no** |
| 20 | `cmd_verify_obligations.go:runVerifyObligations` | yes | **no** |

`cmd_serve.go` is excluded by name, with a reason: its `--addr` is what it
*listens* on, not a graph it reads. Named rather than pattern-excluded so a
future reader cannot slip through by resembling it.

### Is any subject exempt for want of an authority on the wire?

No. Measured mechanically against `proto/awareness_graph.proto`: **all eight
response types a subject can reach carry `GraphAuthority`** — `Briefing`,
`EditCheck`, `Impact`, `Metadata`, `Preflight`, `Query`, `ReferenceSites`,
`Resolve`. Eight of the 13 gaps reach one directly; the remaining five reach one
through `metadataRPC`, `repairPlanPreflight`, `buildAuthoritativeRepairPlan` or
`composeSynthesisRunIdentity`. Every subject is handed the identity it declines
to check.

## Step 2 — the per-subject authority table

Columns are the ones the brief requires. `project root` is how the subject finds
the configuration that decides its endpoint; `domain` is the identity the
comparison would quantify over.

### The 7 that verify

| subject | project root | domain | revision required | generation required | endpoint resolved | authority compared | on match / mismatch / absent | class |
|---|---|---|---|---|---|---|---|---|
| `runBriefing` | `--repo` | `resolveRepositoryDomain` | no | yes | `resolveGraphReader` | `cmd_briefing.go:114` | proceed / refuse / refuse | VERIFIED |
| `runGate` | `resolveProjectRoot` | `--domain` | no | yes | `productionReaderFor` | `cmd_gate.go:78` pre-loop, `:484` per verdict | proceed / refuse (`--report-only` ⇒ DEGRADED, exit 0, no findings) / refuse | VERIFIED |
| `runImpact` | `resolveProjectRoot` | `--domain` | no | yes | `productionReaderFor` | `cmd_impact.go:67` | proceed / refuse / refuse | VERIFIED |
| `runMetadata` | `resolveProjectRoot` | `--domain` | reports | reports | `resolveGraphReader` | `renderEndpointBlock` | reports all three | VERIFIED (reporting surface) |
| `runPreflight` | `resolveProjectRoot` | `--domain` | no | yes | `productionReaderFor` | `cmd_preflight.go:97` | proceed / refuse / refuse | VERIFIED |
| `runQuery` | `resolveProjectRoot` | `--domain` | no | yes | `productionReaderFor` | `cmd_query.go:102` | proceed / refuse / refuse | VERIFIED |
| `runResolve` | `resolveProjectRoot` | `--domain` | no | yes | `productionReaderFor` | `cmd_resolve.go:74` | proceed / refuse / refuse | VERIFIED |

`runMetadata` is deliberately a *reporting* surface: it states the verdict
instead of refusing on it, because its job is to describe graph state. It is
counted as verified because the comparison happens and is disclosed, not because
it gates.

### The 13 that do not

`absent` below means: the response carried no authority, or carried one stating
no generation.

| subject | project root | domain passed to the owner | generation required | authority compared | behaviour today on match / mismatch / absent | authoritative consumption | class |
|---|---|---|---|---|---|---|---|
| `runBenchmarkBrief` | `resolveProjectRoot` | **`""`** | yes | never | identical in all three | repair plan built from preflight; drives benchmark verdict | **GAP-B** |
| `runBenchmarkScore` | `resolveProjectRoot` | **`""`** | yes | never | identical | numeric score caps, exit code | **GAP-B** |
| `runPatternCheck` | `resolveProjectRoot` | **`""`** | yes | never | identical | `--fail-on-violation` ⇒ exit 1 (CI gate) | **GAP-B** |
| `runSynthesisRun` | `resolveProjectRoot` | **`""`** | yes | never | identical | run disposition ⇒ exit code, durable receipt | **GAP-B** |
| `runDomains` | `resolveProjectRoot` | **`""`** | see Shape C | never | identical | prints the selectable domain set (VS Code's filter) | **GAP-D** |
| `runContractBootstrap` | `resolveProjectRoot` | `--domain` | yes | never (calls `requireAuthoritativeGraph` only) | identical | writes contract scaffolding from preflight + impact | **GAP-A** |
| `runEditBrief` | `--root` | `--domain` | yes | never | identical | injects invariants / forbidden fixes into an agent's edit | **GAP-A** |
| `runEditCheck` | `resolveProjectRoot` | `--domain` | yes | never | identical | prints warnings, and prints *"no advisory rule tripped"* | **GAP-A** |
| `runEditGuard` | `--root` | `--domain` | yes | never | identical | **blocks the edit** (`deny` / exit 2) | **GAP-A** |
| `runRepairPlan` | `resolveProjectRoot` | `--domain` | yes | never (calls `requireAuthoritativeGraph` only) | identical | the plan an implementer executes | **GAP-A** |
| `runRepairGate` | `resolveProjectRoot` | `--domain` | yes | never | identical | gate verdict ⇒ exit 1 | **GAP-A** |
| `runRepairReport` | `resolveProjectRoot` | `--domain` | yes | never | identical | report + authority summary consumed as evidence | **GAP-A** |
| `runVerifyObligations` | `--repo` | `--domain` | yes | never | identical | PASS / FAIL / INDETERMINATE exit codes | **GAP-A** |

Two facts in that table matter more than the rest.

**The `reader` is resolved and then thrown away.** Every one of the 20 writes

```go
reader := productionReaderFor(fs, *domain, *addr)
*addr = reader.Addr
```

and keeps only `Addr`. `reader.DeclaredGeneration` — the thing the comparison
needs — is computed at resolution and discarded one line later. That is why this
is one incomplete invariant and not thirteen bugs: the identity was already
resolved everywhere, and dropped everywhere.

**Five subjects pass the empty domain.** `declaredActiveGeneration(path, "")`
reads `reg.Domains[""]`, the zero value, so `DeclaredGeneration` is `""` and
`verifyServed` is *structurally* inert for them — permanently, not situationally.
For those five the comparison cannot be added at the call site alone; the subject
must first know which domain it reads for. None of the five has a `--domain`
flag, but none needs one: `resolveRepositoryDomain(root, "")` already resolves
the governed domain from the project's own configuration (then `SENSEI_DOMAIN`,
then `AWG_DOMAIN`), which is exactly how `runBriefing` and
`runVerifyObligations` obtain theirs.

## Step 3 — the invariant, derived from the table

> **No production graph-reading subject may consume graph-derived information as
> authoritative until the authority returned *with that information* has been
> compared against the generation the registry declares ACTIVE for the domain the
> subject resolved.**

Stated so it can fail. What each part is doing:

- *with that information* — the authority must come from the same response. A
  separate `metadata` round trip proves a generation was active at some moment,
  not that it produced this answer.
- *the domain the subject resolved* — the comparison is per-domain, so a subject
  that resolved no domain has not established the referent. It is not exempt; it
  is incomplete.
- *compared* — not "generations equal". Absence is a third outcome and is
  refused, not skipped: a response that states no generation while one **is**
  declared is precisely the condition this exists to refuse on. Under law 13 a
  triple count is evidence, never identity, so nothing may stand in for a missing
  digest.
- *ACTIVE* — when the registry declares nothing, there is nothing to contradict
  and the check is inert. This is the same inertness the pointer itself has.
  Absence of a *declaration* is not absence of *authority*.

### The minimum complete authority tuple, per operation

Taken from what `GraphAuthority` actually carries and what each operation's own
contract already requires — not invented:

| field | required by this family | why |
|---|---|---|
| `live_store_graph_digest_sha256` | **yes**, all 20 | the generation that answered; the only field comparable to the registry's ACTIVE declaration |
| domain | **yes**, implicitly | carried by the reader, which resolved *for* a domain; the comparison is per-domain |
| `authoritative` + `graph_freshness_state` | only the 4 that already require it | a *different* question — see below |
| `graph_build_commit` (rule-snapshot revision) | **no** | **no subject in this family declares an expected value for it.** Requiring one would fabricate a field the contract does not have. `diffaudit` binds a revision separately, where an expected head *is* supplied. |

So generation-against-declaration is the complete tuple here, and revision is
**deliberately absent rather than forgotten**.

### Why this is not `requireAuthoritativeGraph`

`requireAuthoritativeGraph` (`cmd_repair_plan.go:164`) already exists and is
already called by four subjects — `runBenchmarkBrief`, `runContractBootstrap`
(twice) and `runRepairPlan`. It asks whether the graph is **internally**
authoritative: authority present, `Authoritative()` true, freshness `CURRENT`.

That is a different question, and a graph can answer it perfectly while being the
wrong generation for this domain. All four of those subjects appear in the GAP
table *because they call it*: they were consuming a graph that certified itself
and that nobody had compared.

The two compose; neither replaces the other. Folding them into one predicate and
applying it to all 20 would **over**-enforce: nine subjects do not require
freshness `CURRENT` today, and two of them (`edit-brief`, `edit-guard`) have a
documented contract never to wedge editing. A shared verifier is only valid while
it preserves that difference, so they stay separate.

## Step 4 — repair shapes, before any subject is repaired

The 13 group into **three** consumption shapes, not thirteen contracts.

### Shape A — domain known, authority in hand, never compared (8 subjects)

`runContractBootstrap`, `runEditBrief`, `runEditCheck`, `runEditGuard`,
`runRepairPlan`, `runRepairGate`, `runRepairReport`, `runVerifyObligations`.

The narrowest existing seam is the **owner's own comparison**, already used by
six verifying subjects. All six spell it the same way by hand:

```go
reader.verifyServed(resp.GetAuthority().GetLiveStoreGraphDigestSha256())
```

Repeated six times, about to be repeated thirteen more, and **already drifting**:
`cmd_edit_brief.go:278` reads `GetGraphBuildCommit()` into a field it calls
`Generation`. Which field carries the generation is a fact that belongs to the
owner once, not to nineteen call sites.

So the seam is one method on the type that already owns the answer:

```go
func (r graphReader) verifyServedAuthority(a *awarenesspb.GraphAuthority) error
```

This centralizes a rule that **already exists in one place**; it does not
introduce a second rule. `verifyServed` keeps the comparison, `declaresGeneration`
keeps the inertness predicate, and the new method only removes the nineteen-fold
repetition of *which field to read*. The four subjects that also need internal
authority keep calling `requireAuthoritativeGraph` in addition.

What differs per subject is not the check but the **refusal**, and that must be
preserved rather than unified:

| subject | refusal that preserves its contract |
|---|---|
| `runContractBootstrap`, `runRepairPlan`, `runRepairGate`, `runRepairReport`, `runVerifyObligations` | refuse: error to stderr, non-zero exit, nothing written |
| `runEditCheck` | refuse **before** printing a verdict — the danger is *"no advisory rule tripped"*, a false clean |
| `runEditBrief` | deliver nothing, exit 0 — its documented failure mode for an untrustworthy answer is already silence |
| `runEditGuard` | withhold the **block**, exit 0, say why on stderr — it fails open by contract, and a `deny` derived from another domain's rules is the defect |

### Shape B — the subject resolved no domain (4 subjects)

`runBenchmarkBrief`, `runBenchmarkScore`, `runPatternCheck`, `runSynthesisRun`.

Shape A's seam cannot help these: with `domain == ""` the comparison is
structurally inert. The repair is upstream of it — resolve the governed domain
through the existing owner instead of passing the empty string:

```go
reader := productionReaderFor(fs, resolveRepositoryDomain(root, "").Domain, *addr)
```

Then Shape A applies unchanged.

**Stated assumption, because this reaches past the family.** Giving the owner a
domain also lets `resolveDomainServiceAddr` consult that domain's registry
`service_addr`, one tier above the project config. That tier is documented as
inert until an operator declares a `service_addr`, so it moves nothing today —
but it is a real semantic change and is flagged here rather than slipped in. It
is also the correct direction under law 3: a reader states the domain it reads
for and *receives* the endpoint.

### Shape C — the subject that looked exempt (1 subject)

`runDomains` prints the set of domains a graph offers, for a picker. The plan was to
classify it non-authoritative by contract and write that contract down.

**That was wrong, and the rule the brief set is what caught it.** A subject is only
non-authoritative *by contract*, and what existed was an argument: no single domain's ACTIVE
declaration is the referent of an answer spanning all domains. But the picker chooses the
domain a governed operation then runs against, so the list is consumed authoritatively one
step later, by whoever picks from it. And a referent does exist — the project's own
configuration names its domain, which is Shape B's repair.

So `runDomains` takes Shape B's repair and then Shape A's check, and the family needs **no
exemption vocabulary at all**. `sensei metadata` remains the surface that *reports* a
generation disagreement rather than refusing on it; `domains` is not a diagnostic.

This is worth recording as the more useful outcome: the exemption would have passed review as
reasonable prose. The census cannot read prose, which is exactly how thirteen subjects stayed
invisible behind a sentence that was true when written.

## What the repair also had to fix to be possible

Four things found while wiring, each a precondition rather than a widening.

**The reader was resolved before the domain was known — four times.**
`contract-bootstrap` takes its domain from the task file, `verify-obligations` and
`edit-brief` resolve it through `resolveRepositoryDomain`, and `synthesis-run` resolves it
from `absRepo`. All four resolved the reader right after flag parsing, so it carried the
endpoint and declared generation of a *different* domain — usually none. Verifying with such
a reader is verification that cannot fail. Each now resolves the reader **after** its domain
is settled, and the comment at each site says why it is not where the convention puts it.

**`edit-brief` had a second, divergent endpoint precedence.** It called
`productionReaderFor` and then re-implemented the precedence inline against its own
`projectRoot`, while the owner resolves one from the working directory. The two could name
different endpoints — and then the reader holds the declared generation of a graph the RPC
never contacts. Deleted; one resolution, given the root and domain the command actually uses.

**`edit-brief` recorded the wrong field as the generation.** `editBriefOutcome.Generation`
read `graph_build_commit` — the rule-snapshot revision — into the value written to
`evidence.Event.GraphGeneration`, whose own contract says "the knowledge generation that
answered, so a surfaced law can be traced to the publication that held it". Right field name,
wrong operand. Now the live-store digest, and the owner's own test pins the distinction.

**Three self-certification helpers exist, and none of them is an identity check.**
`requireAuthoritativeGraph` (4 callers), `validateLiveBenchmarkAuthority`, and
`InterpretMetadataAuthority` all ask whether the graph vouches for *itself*. Every one of
their callers appeared in the GAP table **because** they call one: the graph answered the
self-certification perfectly and nobody had compared it to the registry. They are kept, and
composed with the new comparison, never replaced by it.

## Step 8 — closing the family

The completion condition is an assertion in the census itself
(`TestTheReaderCensusStatesWhyEachReaderDoesOrDoesNotVerify`), not a number anyone maintains:

```
production graph-reading subjects == subjects whose authoritative consumption
                                     is generation-verified
```

Measured on this tree: **20 subjects, 20/20 endpoint ownership, 20/20 generation verified, GAP
set empty.** A newly added production graph-reading command that consumes graph authority
without comparing it fails here with no list to edit, and there is deliberately **no
allowlist** — the way this gap opened was a sentence excusing two commands, true when written
and falsified by the commit that added `GraphAuthority` to `EditCheckResponse`.

### The census had to become transitive, and that is a correction to the instrument

Its predicate read only the `run*` function's own body. Once several subjects consume graph
evidence through ONE shared function — `generateRepairReport` serves repair-report and
repair-gate, `buildAuthoritativeRepairPlan` serves both benchmark commands — the comparison
correctly lives there, and a body-only census reported those commands as unverified while
they were verified. A census that pushes checks back out of shared code to stay readable is
measuring its own convenience. It now walks each subject's own call graph within the package.

Both name sets are read **by membership** of a recognised set, never by excluding known-bad
names: an unrecognised helper is not a verification, so a rename shows up as a gap rather than
silently certifying one.

**What it does not prove.** Reachability is not enforcement — a helper could compare on one
branch and consume on another. The census answers *completeness*; the witnesses answer
*correctness*; neither substitutes for the other, and the file says so where the traversal
lives.

**It is falsifiable, and that was measured rather than assumed.** Neutralising the comparison
in each of seven subjects (with a compiling no-op, so the failure is a gap and not a build
error) flips exactly the affected subjects — and flips *two* for `cmd_repair_report.go` and
*two* for `cmd_benchmark_brief.go`, which is the shared-seam grouping showing up in the
measurement.

## The witnesses

`cmd/awg/served_authority_test.go`. Every adversary is HEALTHY: prompt, well-formed, and
self-certified authoritative + CURRENT. The only thing wrong is which generation answered.
Every refusal witness is paired with its opposite, because a refusal with no opposite is
indistinguishable from a command that stopped working.

| # | subject | the defect it drives | the opposite |
|---|---|---|---|
| A1 | `repair-plan` | a complete plan printed beside the wrong `live_digest`, its self-certification gate passing | the plan is still produced when the generation agrees |
| A2 | `edit-guard` | a write DENIED on another generation's forbidden-fix rule, on both output adapters and both streams | still blocks a critical match from the declared generation |
| A3 | `repair-plan` | absent authority — refused today by the internal-authority gate, recorded so the repair is not credited to the wrong one | — |
| A4 | `edit-guard` | absent authority on a subject with **no** self-certification gate | (shares A2's opposite) |
| A5 | `edit-check` | *"no advisory rule tripped for this edit."* — a false clean an agent reads as permission | still reports, through the text **and** `--json` paths |
| A6 | `domains` | the picker's list from an undeclared generation | still prints the list when it agrees |
| A7 | `repair-gate` | PASS on evidence from an undeclared generation; asserts its own classification, not `stale_authority` | — |
| A8 | `edit-brief` | another generation's invariants pushed into an agent's edit; asserts the **ledger row** too | still delivers when it agrees |
| A9 | `repair-gate` | the store republished **between** Metadata and Preflight: verified once ≠ verified | — |
| B1 | the owner | a domainless reader can never compare; the repository-scoped reader can | a malformed domain config FAILS rather than falling through to the empty domain |
| C1 | `repair-plan` | — | nothing declared ACTIVE: everything proceeds exactly as before |

A3 is deliberately weak and says so: `repair-plan` refuses a nil authority through
`requireAuthoritativeGraph`, which runs first, so that subject cannot witness the new check's
absence handling. A4 does, on a subject without that gate.

## Mutation results

Restored from saved copies, never `git checkout`; each pattern asserted to match exactly once;
each `-run` selector asserted to select at least one test; a non-building mutant reported
`DID_NOT_COMPILE` rather than killed.

| mutant | verdict |
|---|---|
| M1 delete the comparison in one Shape A subject | KILLED |
| M2 delete it in a SHARED helper (2 subjects) | KILLED *(survived first — see below)* |
| M3 wrong operand: compare `graph_build_commit` | KILLED |
| M4 absence passes | KILLED |
| M5 Shape B regression: back to the empty domain | KILLED |
| M6 refusal downgraded to a warning | KILLED |
| M7 check moved AFTER consumption | KILLED *(survived first — see below)* |
| M8 inertness widened: nothing ever compares | KILLED |
| M9 a NEW graph-reading command consumes authority unverified | KILLED |

**9 of 9 code mutants killed.** Two survived the first pass, and both indicted the witnesses:

- **M2** survived because the *metadata* comparison fired first in every witness that existed,
  making the per-response preflight comparison look redundant. It is not: the store can be
  republished between two calls on one connection. Witness A9 was written for it.
- **M7** survived because the `exit-code` adapter writes its reason to **stderr**, and the
  witness read only stdout and the exit code. Both adapters and both streams are now asserted.

A tenth mutant downgraded the census's own assertion back to a log line. It survives by
construction — no test can detect its own oracle being weakened — so it is reported here as an
**oracle mutation**, not as a killed mutant and not as a code survivor. M9 is the protection
that actually matters: the property, not the phrasing.

## Residuals, stated rather than closed

- **`pattern-check --fail-on-violation` is fail-open on per-file errors**, by pre-existing
  contract: an unreachable server already produced `out.Error` and exit 0. A generation
  mismatch now lands in that same bucket. The family's invariant holds — the result is refused,
  not consumed — but the CI gate passes. Changing that is a different contract and is not
  touched here.
- **Every classification remains allow-listable** via `repair-gate --allow-classification`,
  including `undeclared_generation`. That is the existing escape shape, which `stale_authority`
  has too; it is explicit at the point of use and is not widened or narrowed here.
- **`requireAuthoritativeGraph` still does not check the generation**, on purpose. Folding the
  two into one predicate and applying it to all 20 would over-enforce: nine subjects do not
  require freshness `CURRENT` today, and two have a documented contract never to wedge editing.
- **Endpoint ownership for `runDomains` moved up a tier.** Giving the owner a domain lets
  `resolveDomainServiceAddr` consult that domain's registry `service_addr`, above the project
  config. That tier is inert until an operator declares one, so nothing moves today — flagged
  because it is a real semantic change and not a side effect anyone should discover later.
- **Two pre-existing test failures in this worktree are environmental**, not from this work:
  `TestBuiltSenseiBinaryInitializesSkillsOutsideSourceTree` (cmd/awg) and two stamped-commit
  tests in `golang/server` shell out to `go build` / `make` without `-buildvcs=false`. Verified
  to fail identically at the base commit with this work stashed.

## Two implementation defects, found by blind review of head `59372102`

The 20-subject design above is unchanged. Both of these were defects in how it was implemented.

### Finding 1 — an optional `--domain` made verification incapable

Raised at `cmd_edit_check.go:43`. The measurement is worse than the report: **eleven of the
twenty subjects handed the owner a raw flag value**, and **six of those eleven were among the
seven that verified before this family began**. Omit the flag and `""` reaches the owner,
`declaredActiveGeneration` reads `reg.Domains[""]`, and the comparison cannot fail.

So it is not a defect in `edit-check` and not a defect in the Shape A repair — it is a defect in
**what the owner is given**, which is why it is repaired in one place rather than eleven.

**The law after repair:**

> A production authority comparison receives a **resolved** expected domain, never a raw CLI
> flag value. Resolution belongs to the owner, and the owner is the only place a `graphReader`
> is constructed.

`resolveGraphReader` now calls `resolveRepositoryDomain(projectRoot, domain)` — which was
already the owner of that question and already implemented the precedence (explicit, then this
checkout's configuration, then `SENSEI_DOMAIN`, then legacy `AWG_DOMAIN`). Passing an
already-resolved domain back through is idempotent: it arrives as the explicit tier and wins.
`productionReaderForRepository` is **deleted** — once the owner resolves, "no flag" and "an
omitted optional flag" are the same input, and a second function could only let the two drift.

**Three states where there was one empty string.** This is what the review asked to be
distinguished, and each has a different consequence:

| state | meaning | consequence |
|---|---|---|
| `DeclaredGeneration == ""` | the domain is **known**; the registry declares nothing ACTIVE for it | inert — nothing can contradict |
| `DomainUnbound` | nothing states which domain this checkout is; the registry was never asked | outside the invariant's quantifier |
| `DomainInvalid` | a domain **was** stated and cannot be trusted | **refused** at the comparison |

**One deliberate narrowing, and the measurement that forced it.** The brief asked for
*"genuinely unresolvable expected domain → cannot_verify/refusal, never inert success"*. The
first implementation refused whenever the registry declared an ACTIVE generation for **any**
domain. That took **fifteen tests out of service in one run**: a checkout that states no domain
has no relationship to declarations made *for other domains*, and treating those as
possibly-governing turns every ad-hoc read on a machine with a populated registry into a
failure. `sensei query` from a directory that is not a Sensei project violates no declaration,
because it made none.

So the refusal is narrowed to `DomainInvalid` — something claimed an identity and it could not
be trusted — and the defect the brief was protecting against is closed **structurally** rather
than by a runtime refusal: the owner always resolves, and
`TestOnlyTheOwnerConstructsAProductionGraphReader` proves there is exactly one construction
site, so a reader carrying an unresolved domain cannot be obtained. **This is the one place the
repair does not follow the brief literally**, and it is flagged rather than quietly narrowed.

### Finding 2 — the response identity representation was over-constrained

Raised at `cmd_gate.go:77`. **The stated mechanism is wrong in two ways**, measured and recorded
because the correction matters more than the finding:

- `graphAuthorityFromSnapshotFor` discards a `*DomainPublication`, **not an error** — it returns
  no error at all;
- it has **exactly one `return`**, which is `&awarenesspb.GraphAuthority{…}` and never nil, from
  the **same** `freshness` snapshot and the **same** `servedGraphDigest` call that fills the
  top-level field. From this server the two representations cannot disagree and the auxiliary
  structure is never absent. `metadata.go` says so in its own comment.

**The conclusion holds anyway, for a case the review did not name.** An **older server** sends
`live_store_graph_digest_sha256 = 46` and no field 67, so `Authority` is nil on the wire while
the canonical served identity is stated. Refusing that manufactures a refusal out of a missing
auxiliary structure while valid evidence sits beside it. This repository already models exactly
that shape — `gate_generation_test.go`'s *"older server: Metadata states the generation, the
EditCheck response does not"*.

`MetadataResponse` is the **only** message in this proto with two representations; every other
response carries the served digest solely inside `GraphAuthority`. So this is a
`MetadataResponse` seam (`graphReader.verifyServedMetadata`), not a general one, and five
consumers use it: `gate`'s pre-loop check, `domains`, `repair-report`/`repair-gate`,
`benchmark-score`'s authority guard, and `synthesis-run`'s identity composition. `gate`'s
per-verdict check keeps `verifyServedAuthority`, because `EditCheckResponse` has no top-level
digest.

**The law after repair:**

> Served-generation verification uses the canonical identity evidence the response contract
> actually guarantees, and refuses when the required identity cannot be established.
> **Disagreement is a refusal, never a preference** — a response describing two generations
> attests to neither.

`GraphAuthority`'s other dimensions — `authoritative`, freshness, transaction certification —
are untouched and stay with the helpers that own them (`requireAuthoritativeGraph`,
`validateLiveBenchmarkAuthority`). This seam answers identity only.

### Witnesses and mutation

Finding 1 (`optional_domain_test.go`): the reproducer; the single-construction-site census; the
owner resolving with an explicit domain still winning; the three-state distinction; an invalid
explicit `--domain` refused like a malformed config; and the end-to-end pair at `edit-check`
with the flag omitted, refusing a foreign generation and still reporting on the declared one.

Finding 2 (`metadata_identity_test.go`): the five cases driven independently — canonical digest
alone sufficient, wrong canonical digest refused, authority-borne digest alone sufficient,
disagreement refused **in both orderings** (a preference rule would pass one, and which would
be arbitrary), no identity refused — plus the inert control and a driven older-server pair at
`domains`.

**16 of 16 code mutants killed** across the family, including all seven these findings require:
raw flag restored, resolved domain emptied, comparison skipped when omitted, top-level digest
ignored, missing identity accepted, disagreement accepted, and verification bypassed in one
consumer. One mutant reported `MUTATION_NOT_APPLIED` when its anchor moved and was re-aimed
rather than counted. The tenth remains an **oracle mutation** — downgrading the census's own
assertion to a log line, which no test can detect.

**Census after repair: 20 subjects / 20 owner-resolved / 20 generation-verified / GAP 0**,
derived per command from behaviour, with the recognised-name sets still read by membership.

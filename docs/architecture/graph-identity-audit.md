# Graph identity, resolution and generation lifecycle — Phase 1–3 audit

**Date:** 2026-09-13
**Branch:** `arch/graph-identity-and-generation-lifecycle`, from `739133dc` (origin/main)
**Status:** AUDIT AND LAWS. No implementation in this document. No live
import/build/rebuild/refresh was run to produce it; every number below is a
read-only measurement.

## 0. The headline

**Three Oxigraph stores are live. The one `netcfg` declares as the production
default is served by nothing.**

| endpoint | triples | store location | served by | domain |
|---|---|---|---|---|
| `127.0.0.1:7878` | **237,049** | `~/.local/share/awg/oxigraph` | **NOTHING** | — |
| `127.0.0.1:7881` | **142,739** | `~/.local/share/sensei/store-sensei` | awareness-graph `:10121` | `github.com/globulario/sensei` |
| `127.0.0.1:7882` | **35,268** | `~/.local/share/sensei/store-sensei-code` | awareness-graph `:10122` | `github.com/globulario/sensei-code` |

`golang/netcfg/netcfg.go` declares itself *"the single source of truth for the
default network endpoints used across the Sensei stack"* and declares:

```
DefaultServicePort   = 10120        live: CLOSED
DefaultProxyPort     = 10121        live: open — but this is the awareness ADDRESS
                                    sensei-code's config for the sensei repo dials
DefaultOxigraphBind  = 127.0.0.1:7878   live: open, 237k triples, UNSERVED ORPHAN
DefaultOxigraphBase  = http://localhost:7878
```

**Every netcfg default is wrong in production.** The declared service port is
closed; the declared store port holds a 237k-triple graph that no awareness
service reads and no marker corresponds to; and the declared *proxy* port is
being used as a *service* address by a real consumer.

Nothing prevents a reader that falls back to those defaults from receiving a
perfectly healthy 237,049-triple answer about nothing the caller asked about.

## 1. Topology

| component | reads/writes | domain | endpoint | marker | activation owner | source of truth |
|---|---|---|---|---|---|---|
| awareness-graph (sensei-code) | reads `:7882` | `…/sensei-code` | `:10122` | `<sensei-code>/.sensei/graph-authority.json` | none | `-addr`/`-oxigraph-url` flags |
| awareness-graph (sensei) | reads `:7881` | `…/sensei` | `:10121` | `<sensei>/.sensei/graph-authority.json` | none | flags |
| oxigraph (orphan) | `:7878` | unknown | `:7878` | none | none | netcfg default |
| `sensei build --repo` | writes a domain slice | flag | `--store-url` (default `:7878`) | `--graph-marker-file`, else cwd-resolved | itself | flags + cwd |
| `sensei build --all` | **replaces the ENTIRE store** | all | `--store-url` | same | itself | flags |
| `sensei import --refresh` | orchestrates bootstrap → project → `build --repo` | `--domain` | passthrough | **passes marker only if given** | delegates | flags |
| sensei-code governed run | reads via awareness MCP | `.sensei-code/config.json` | `--awareness-addr` | pins `graph_digest` in the receipt | none | per-repo config |
| `netcfg` | supplies defaults | — | 10120 / 7878 | — | — | claims to be SoT; **overridden by flags in production** |

Measured cross-domain isolation: `internal/ghbridge/transport.go` has 1 triple in
`:7882`, **0** in `:7881`, **0** in `:7878`. The three stores are different
graphs, not stale copies of one.

## 2. Every way identity can currently disagree

1. **netcfg default vs live** — a caller using the package that calls itself the
   single source of truth reaches the unserved orphan at `:7878`.
2. **Proxy port used as a service port** — `netcfg.DefaultProxyPort = 10121` is
   what `<sensei>/.sensei-code/config.json` supplies as `--awareness-addr`. The
   owner's own vocabulary says that is a gRPC-web proxy, not the service.
3. **Raw `--store-url`** selects any of three stores with no identity check. Law 14.
4. **Marker default is cwd-bound** — `defaultRuntimeMarkerFile()` →
   `resolveProjectRoot("")`. Correct only when invoked from the project root;
   silently writes another project's marker otherwise.
5. **`import` does not pass the marker** unless given
   (`cmd/awg/cmd_import.go:225-228`), and prints a warning where `build` would
   in fact have defaulted it. The warning is misleading; the cwd dependency is real.
6. **Graph build commit vs checkout** — measured live: a governed observation in
   the sensei repo at `bc1f6d12` was bound to `GraphBuildCommit 7c39060d`, a
   different branch's head, and `BaseSHA` was **empty**.
7. **Domain from two places** — the service's `-home-domain` flag and the
   consumer's per-repo config. Nothing reconciles them.
8. **Triple count as identity** — 35,268 appears in the marker AND as the store
   count, so agreement is asserted by a number two different graphs could share.
   Law 13.

## 3. Existing owners — Phase 2

Prefer completing these rather than inventing a competing authority.

| function | existing owner | verdict |
|---|---|---|
| endpoint defaults | `golang/netcfg` — *"single source of truth"* | **exists, not load-bearing.** Production overrides every default by flag |
| endpoint agreement | `requireStoreURLAgreement`, `requireServerAddrAgreement` (`cmd/awg`) | **exists.** Already the right shape; scope to be established |
| marker write/identity | `seedmeta.WriteMarkerFile`, `seedmeta.RuntimeMarkerPath` | **exists** |
| publication gate | `publication.SourceWitness`, `ConsumedFile`; `PUBLICATION_REFUSED` | **exists and works** — it refused dirty bytes correctly |
| ACTIVE generation | — | **DOES NOT EXIST** |
| generation registry | — | **DOES NOT EXIST** |
| domain → graph mapping | — | **DOES NOT EXIST** (per-repo config by hand) |

### The Phase 2 question, answered explicitly

> For domain `github.com/globulario/sensei-code`, which exact graph generation is
> ACTIVE right now?

**There is no single authority.** Answering it today requires consulting four
independent sources in order: the consumer's `.sensei-code/config.json` for an
awareness address → that service's `-oxigraph-url` flag for a store → the store
itself for a triple count → a marker file whose path depends on cwd. Any one can
be changed without the others noticing. **Graph identity is fragmented**, and
that is the root of the symptom list, not any single incident.

## 4. Why the failed refresh rewrote canonical state despite `mutation_started: false`

From the `import` plan order (`cmd/awg/cmd_import.go`, steps as printed):

```
1 intent-mine (skipped at -depth basic)
2 bootstrap --skip-history --skip-build      <- writes .sensei/project, quarantines the old
3 cold-bootstrap                              <- writes
4 compile project graph, write readiness      <- writes
5 build --repo --store-url                    <- THE PUBLICATION BOUNDARY
```

Steps 2–4 write to the **canonical checkout** and complete before step 5 is
reached. `mutation_started: false` is truthful — it describes the *graph*
transaction only. So the checkout became the staging area, which is exactly what
law 9 forbids. Observed: 12 regenerated skill files, `protection-coverage.yaml`
deleted, `.sensei/project` rebuilt with the old copy quarantined as
`project-invalid-*`, five new candidate sources, a generated contracts file, a
root `sensei-repository.yaml`, and a leftover `.sensei/project.lock` — all from a
run that loaded nothing.

**Second defect in the same command.** `cmd_import.go:230` prints
*"a scoped --repo update needs a non-empty store; seed with `sensei build --all`
first"* on **any** non-zero exit from `runBuild`. The real cause was
`PUBLICATION_REFUSED`, printed two lines above. A fabricated diagnosis attached to
a **destructive** remedy (`--all` = *"replace the ENTIRE store"*) in the tool that
governs everything else.

## 5. The laws

Transcribed from the front's brief, with the measurement each now rests on.

1. One ACTIVE graph identity per governed domain. *(today: none)*
2. A valid response from the wrong Oxigraph instance is still the wrong graph and must refuse. *(today: `:7878` answers healthily with 237k triples)*
3. All production readers resolve identity through one owner; they must not choose a port. *(today: each consumer picks its own)*
4. A governed run pins a graph identity, not a URL. *(today: it pins `graph_digest` — a start, but resolution is still by address)*
5. Every query in that run proves it belongs to the pinned identity. *(today: unproven per query)*
6. Publication is transactional: build G+1 in staging, validate, then atomically activate.
7. ACTIVE G remains usable until G+1 is certified.
8. A failed build/import/refresh must not mutate the active graph. *(holds today — `mutation_started: false`)*
9. A failed publication must not leave the canonical checkout rewritten. *(**violated**, §4)*
10. A marker cannot certify a different generation/store than the one served.
11. A generation must not be destroyed while an ACTIVE pointer, served store, marker or governed run refers to it.
12. Ambiguous identity fails closed; two ACTIVE claims are an error, not a choice.
13. Triple count is evidence, not identity.
14. Raw `--store-url` is test/dev/maintenance authority, visibly non-canonical.

## 6. Slice boundaries, adjusted to the real code

- **G1 read-side identity handshake — smallest safe first slice.** A consumer must
  be able to answer *"what exact graph am I talking to?"* and refuse a healthy
  wrong one. Implementable in sensei-code against the existing awareness
  interface, independent of publication. **Do this first.**
- **G2 single resolution owner.** Build on `netcfg` + the existing
  `require*Agreement` guards rather than a new abstraction; `netcfg` must become
  load-bearing instead of advisory, and must stop declaring an unserved orphan as
  the default.
- **G3 transactional publication.** Move the step 2–4 writes behind the boundary,
  into staging. This was believed to be the fix for law 9; the correction below
  records why it was not.
- **G4 unify CLI lifecycle.** One publication/identity primitive for build,
  import, refresh, bootstrap, rebuild. Delete the fabricated `--all` diagnosis.
- **G5 doctor/status.** One read-only report; `sensei doctor`/`metadata` already
  exist and should be completed rather than duplicated.

## 7. What must not be done next

`sensei build --all` must not be run against a served store on the strength of
`cmd_import.go:230`'s recommendation. It is a guess, and the operation it
recommends is destructive.

---

# Addendum — Phase 7 disposable-store proof, and the W3 root cause

**Date:** 2026-09-13. All measurements below come from a disposable Oxigraph on
`127.0.0.1:7899` with its own store location and its own marker file. **The live
stores and the live marker were never written to.**

## The root cause of the W3 knowledge limit

This corrects an earlier conclusion of mine. I reported that the refresh's only
blocker was an uncommitted corpus. With the corpus committed (`32f6117` in
sensei-code), the scoped load **still refuses**, for a different and structural
reason:

```
PUBLICATION_REFUSED
  reason: corpus root ".sensei/project" is not in the domain's allowed roots [docs/awareness]
  mutation_started: false
```

- `cmd/awg/domain_admission.go:316` refuses a corpus root the domain does not allow.
- `cmd/awg/cmd_import.go:205,224` **always** passes `.sensei/project` as an input.

**So `import --refresh` generates a build command that this domain's publication
gate must always refuse.** Import's own step 5 is unpublishable here, regardless of
cleanliness.

Proven by removing exactly that one input and changing nothing else:

| command | result |
|---|---|
| `--input docs/awareness --input generated --input .sensei/project` | `PUBLICATION_REFUSED` (allowed roots) |
| `--input docs/awareness --input generated` | **exit 0**, 35,255 triples loaded |

And `.sensei/project` is precisely where project reconstruction puts code-symbol
coverage — the ~248,000 triples that separate `docs/awareness` alone (35,245) from
the full compile (283,503).

**Therefore code-symbol coverage can never reach this domain's graph through
`import --refresh`, and the three files W3 needed examined could never become
examined by that path.** The knowledge limit W3 reported was truthful; its cause is
a corpus-root allowlist that excludes the extractor's own output directory, plus a
command that always violates it. Not a missing extractor run.

This also means the remedy text the knowledge limit prints — `sensei import
--refresh <root> --domain <domain>` — **cannot work as written** for this domain.
That is a defect in the remedy, traceable to this one.

## Phase 7 checklist — what is proven

| requirement | result |
|---|---|
| construct a complete generation in a disposable store | **PASS** — 35,255 triples |
| marker/store/generation agreement | **PASS** — marker digest `230a74f68fed…`, `triple_count 35255`, matching the store |
| generation valid after the builder exits | **PASS** — 35,255 after exit 0 |
| restart/reopen | **PASS** — store process killed and restarted, 35,255 intact |
| **the historical failure: a successful build whose generation vanishes when the builder exits** | **DID NOT REPRODUCE** on a scoped `--repo` load |
| failed publication leaves the active graph untouched | **PASS** — `mutation_started: false` on both refusals |
| the live marker is untouched when `--graph-marker-file` is explicit | **PASS** — live stayed `c0b660fc42a5` / 35,268 while the disposable marker was written |
| law 14: a raw override is visibly non-canonical | **PASS, live** — the notice fired naming `:7899` against the configured `:7882` |

Not yet proven, and still open: wrong-port identity refusal (needs G1/G2, since
nothing validates identity today), G→G+1 activation, and that a reader pinned to G
cannot silently consume G+1.

## One number worth noticing

The disposable generation built from the **committed** corpus is 35,255 triples.
The live graph is 35,268 — thirteen more. So the live graph is not the current
corpus; it corresponds to another corpus revision (`graph_build_commit 739133dc`).
Under law 13 that difference is evidence, not identity, and today nothing would
have reported it.

## Instrument note

Attempting this front through sensei-code's governed lane produced two external
refusals worth recording, neither a defect in the work:

- in the **sensei** repo: `no adapter took the architect role: no exact
  objective/world binding`, with `BaseSHA:` empty and `GraphBuildCommit 7c39060d`
  against a checkout at `bc1f6d12`, while the bridge mailbox points at
  `globulario/sensei-code` PR #157. Four identities disagreeing — §2 item 6.
- in **sensei-code** (slice G1): `architect could not produce a bounded decision:
  You've hit your usage limit … try again at 2:27 PM.` Provider quota, account-wide.

G1 therefore remains unimplemented and is still the correct next slice.

---

# Addendum 2 — G3 was largely already built, and the gaps that were real

Phase 2's rule held again: the transaction existed, and the work was to find what it
did not cover.

## `runScopedRepoUpdate` is already transactional

Measured by reading the ordering, not assumed:

```
compile candidate generation + marker   AppendMarker(postUpdateBase)
stage into a named graph                putNamedGraph(stagingIRI, stagedNT)
promote                                 client.Update(scopedPromoteStagingUpdate)
VERIFY over the promoted store          seedmeta.VerifyLiveContent(marker)
drop the staging graph                  deleteNamedGraph
write the marker                        seedmeta.WriteMarkerFile
```

So laws **6** (build G+1 in staging, validate, then activate), **7** (other domains
untouched: *"Only the replacement slice and its global marker are staged"*) and
**10**'s write side (the marker is written only after live verification) were already
satisfied on the scoped path. It even resolves an ambiguous promotion response by
re-verifying with a fresh context, because a lost HTTP reply does not tell you whether
the transaction committed.

That is why Phase 7's disposable-store test found the historical *"generation vanishes
when the builder exits"* failure did **not** reproduce.

## The three gaps that were real

| law | gap | fixed in |
|---|---|---|
| **9** | `import` steps 2–4 wrote the canonical checkout *before* the publication boundary | `37b6099d` hoisted admissibility ahead of the first write. **That did not fix it** — see the correction below. Closed by `50319cb5`; live-proved 0 → 0 changes |
| **10** read side | nothing compared the marker with the graph actually served | `10075941` — `metadata` now reports the verdict; live-proved as *agrees* from one repo and *DOES NOT match* from another, same address |
| **11** | `rebuild` guarded a destructive shrink; **`build --all` did not** | `4835659d` — same guard, same tolerances |

## Laws status

| law | state |
|---|---|
| 1 one ACTIVE identity per domain | **done** — the registry names both the endpoint (G2) and the ACTIVE generation (`active_generation`), so the Phase 2 question has one answer that does not depend on where it is asked from |
| 2 wrong instance must refuse | **done** for the consumer — sensei-code#173 |
| 3 readers resolve through one owner | **done** — `resolveDomainServiceAddr`, registry above project config |
| 4 a run pins an identity | **done for the decisive call** — a run pins `certifiedStart.GraphDigest()` and the admitting audit is refused unless it was answered by that generation (sensei-code#173). Still open: the run is *compared* against the registry's ACTIVE pointer rather than reading it |
| 5 every query proves it belongs | **done for the audit, both directions** — the evaluator brackets its own queries and refuses a switch *within* one audit (`graph_generation_switched`), and the run refuses a verdict answered by a generation other than the one it pinned. Still open: briefing and impact calls outside the audit are not each individually bound |
| 6, 7 transactional, ACTIVE survives | **already held** on the scoped path |
| 8 failed build does not mutate the active graph | **held** — `mutation_started: false` |
| 9 failed publication does not rewrite the checkout | **done** for import — `50319cb5`, after `37b6099d` was reported closed and was not (see the correction) |
| 10 marker cannot certify another generation | **done** — write side already held, read side now reported |
| 11 no destruction while referred to | **done** for `--all`'s shrink shape |
| 12 ambiguity fails closed | **done** — G1 fails closed on domain disagreement, and `verifyActiveGeneration` refuses two claimants for one domain as an ambiguity rather than a choice |
| 13 triple count is evidence, not identity | **enforced** in `markerAgreement`: an absent digest reports unverifiable, never agreement |
| 14 raw `--store-url` is visibly non-canonical | **done** — `9ba1aa49` |

## Correction: law 9 was reported closed by `37b6099d` and was not

Recorded because the claim was published before it was measured.

`37b6099d` hoisted the admissibility check ahead of the first write, and the
reasoning was that an inadmissible import would then refuse before touching the
checkout. The live run disagreed: **0 → 27 changed files with nothing loaded.**

The hoisted check passes, because at that moment the corpus *is* clean. Extraction
then writes candidates and generated contracts into `docs/awareness` — the very
directory the domain publishes and requires clean — and the publication gate
refuses what extraction just dirtied. The import defeats itself, and it does so
*after* rewriting the checkout. Moving a guard earlier cannot help when the input
it guards is created by the run itself.

`50319cb5` closes it by refusing the run up front: if any extraction write root is
an allowed corpus root and the domain requires a clean tree, the import cannot
succeed, so nothing is run. Live-proved 0 → 0 on the same invocation that
previously produced 0 → 27.

The general shape, which is worth more than the fix: *a guard's position is part of
its correctness, and a guard placed before a write is still useless if the thing it
checks is produced by the write.*

## The ACTIVE generation pointer — closed

Phase 2's unanswered question was:

> For domain `github.com/globulario/sensei-code`, which exact graph generation is
> ACTIVE right now?

It had no answer because a generation was identified only by a digest inside a
marker *file*, whose path resolves from the current working directory. The answer
therefore depended on where the asker stood, which is not an answer at all.

The owner is the domain registry (`~/.sensei/domains.yaml`) — the Phase 2 rule:
complete the existing owner rather than invent a competitor. It is the right one
for the same reason it already owns `domain → endpoint`: operator-controlled and
kept **outside** any published repository, so a repository cannot declare its own
graph active any more than it may vouch for its own corpus.

| piece | where |
|---|---|
| the declaration | `RegisteredDomain.ActiveGeneration` (`active_generation:`) |
| the reader | `declaredActiveGeneration` |
| the refusal | `verifyActiveGeneration` → `*activeGenerationMismatchError` |
| the recorder | `recordActiveGeneration`, one call site, at the activation transition |
| the report | the Endpoint block of `sensei metadata` |

Three properties are deliberate:

1. **Inert until declared.** No declaration contradicts nothing, so every existing
   caller behaves exactly as it did. Adding this moves no one's graph today.
2. **Absence is unverifiable, never agreement.** A declared generation with a
   served graph that states none is refused — law 13's reason in another currency:
   a matching triple count is not a matching graph, and neither is a missing digest.
   A prefix is likewise not the generation, which matters because short digest forms
   are printed throughout this codebase.
3. **The refusal is stated as ambiguity, not staleness.** Two claimants for one
   domain is an error and not a choice for the caller (law 12), so the message names
   both values and refuses to prefer one.

Recording happens *after* the marker is written, so the pointer can never name a
generation whose marker was never published, and a failure to record does not fail
a publication that genuinely succeeded — it warns, and the consequence of a stale
pointer is that readers **refuse**. That fails closed, which is the point.

## Law 5, and the third identity

Closed 2026-09-13 in two commits, one per side.

The evaluator behind `awareness_audit_diff` already enforced half the law without
naming it: `graph_commit` must agree across every file of an audit, because "a
divergence means the snapshot shifted mid-audit and the result cannot be trusted."
What it binds, though, is the RULE SNAPSHOT, and the graph server here runs with
`-home-domain github.com/globulario/services` — so that commit belongs to the
**services** repository, and two different Sensei generations built from one
snapshot are indistinguishable by it.

So the audit reported an identity that could not detect the switch it was checking
for. The repair is a third identity, never merged into the other two:

| identity | names | field |
|---|---|---|
| audit target | the repository base the diff applies to | `expected_head` |
| rule snapshot | the commit that produced the rules consulted | `graph_commit` |
| generation | the bytes that answered the queries | `graph_generation_sha256` |

`GraphGenerationReporter` is sampled before the first graph query and after the
last, so an equal pair brackets every query the audit made. A switch is
`cannot_verify` with `graph_generation_switched` — deliberately not
`graph_unavailable`, because "a different graph answered the second half" and "the
graph did not answer" are opposite diagnoses and collapsing them sends a reader to
look for an outage that never happened.

On the consumer side, `verifyPinnedGeneration` refuses a verdict answered by a
generation other than the one `certifiedStart` pinned, before the verdict is
weighed. A graph rebuilt between the certifying preflight and the admitting audit
would otherwise hand the run a well-formed judgement produced by rules it never
certified.

Six fixtures were resting on the silence this closed — a hand-built `AuditResult`,
the evaluator's `fakeChecker`, and five bridge tests whose fake client had no
metadata stub. Every one was completed rather than the guard relaxed.

## G4: the marker contract, and a resolver that fails open to the working directory

Closed 2026-09-13 for the marker half of G4. The plan named the symptom it is
resolving: *"marker paths not following the defaults callers believed they had."*

One flag name carried **four** contracts, and no command said which had answered:

| tier | condition | answer |
|---|---|---|
| 1 | explicit `--graph-marker-file` | that path |
| 2 | `serve` in embedded-seed mode, no flag | **no marker file** — the embedded one is in force |
| 3 | any other command, no flag | `<root>/<statedir>/graph-authority.json` |
| 4 | no resolvable project root | a **relative** path, resolved against the caller's directory |

Tiers 2 and 3 were the same empty string at the call site, so "the embedded marker is
in force" and "the default marker file" were indistinguishable. `resolveGraphMarkerFile`
is now the single owner and returns `(path, source, error)` — the shape G2's
`resolveDomainServiceAddr` established, because a resolution that cannot say where it
came from makes two legitimately different answers indistinguishable from one answer
having changed. `sensei metadata` prints the source beside the path.

### The resolver failed open, and the first refusal watched the wrong door

Tier 4 was built as a refusal: no root means the path would be relative, so refuse.
Measuring the trigger found the actual defect.

**`resolveProjectRoot` never reports "not in a project."** Its walk looks for
`docs/awareness` or `<statedir>/config.yaml` and, finding neither anywhere up the
tree, returns the **current working directory** with a nil error. So `root == ""`
almost never happens, and the cwd-dependent marker arrives as a perfectly ordinary
absolute path through a door the refusal was not watching. Two commands run from two
directories resolve two markers, silently, and `statedir.Name` is then evaluated
against a directory that is not a project either.

Changing that contract is out of scope: 34 non-test callers depend on the fail-open
walk. The invariant goes at the choke point instead — the marker resolution requires
its root to satisfy the *same* definition of a project the walk searches for,
extracted as `looksLikeProjectRoot` so the search and the requirement cannot drift.
An explicit flag and embedded-seed mode are unaffected: neither infers a root.

### Two other things this exposed

- **An orphaned legacy marker.** `statedir.Name` prefers `.sensei` and falls back to
  a pre-existing `.awg`. When both exist the legacy marker is still on disk,
  certifying a generation nothing serves, and reported by nothing. The resolution now
  names it — but only when it actually exists, because a report that always warns
  teaches its reader to skip the line.

- **Four tests over unreachable code.** Routing `serve` through the single resolver
  left `selectServeGraphMarkerFile` reachable only from its own four tests. Four
  passing tests over dead code are worse than none, because they report the contract
  as covered. The helper is deleted and those four cases now call the function serve
  actually runs. A dead branch in the new code went the same way: an "unresolvable
  project root" special case in `resolveServeGraphMarkerFile` that could never
  execute, since `resolveProjectRoot` errors only if `os.Getwd()` fails.

Seven fixtures asserted a bare `.sensei` directory was a project. All corrected.

## What remains before Oxigraph is an implementation detail

**`.sensei/project` cannot publish anywhere.** All three registered domains allow
only `docs/awareness`, so the reconstruction output carrying code-symbol coverage is
unpublishable by construction. Whether the allowlist or the output location is wrong
is an owner's decision; until it is made, coverage cannot reach any graph and the W3
knowledge limit stands.

The `import` half of this is now honest rather than silently destructive — such a run
refuses up front instead of rewriting the checkout and then failing — but refusing
truthfully is not the same as being able to publish, and the owner's decision is
still owed.

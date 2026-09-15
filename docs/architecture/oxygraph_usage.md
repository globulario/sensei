Pause W3.

Treat the recurring RDF/Oxigraph failures as a new architecture front: **Sensei graph identity, resolution, generation lifecycle, and publication must become single-owned and fail-closed.**

Do not repair the latest refresh incident in isolation.

The recurring symptoms now include:

* reading from or writing to the wrong Oxigraph instance/port;
* graph endpoint identity inferred from configuration rather than proven;
* stale graph despite apparently healthy services;
* marker/store disagreement;
* freshness reported CURRENT while relevant files remain unexamined;
* scoped refresh requiring a pre-existing domain slice;
* `build --all` / generation lifetime concerns;
* marker paths not following the defaults callers believed they had;
* failed refresh rebuilding/quarantining `.sensei/project` and modifying repository files even though authoritative graph mutation never began;
* corpus publication correctly refusing dirty bytes;
* multiple service/store instances historically existing at different addresses;
* governed runs pinning graph digests while the machinery that creates/serves those graphs remains comparatively fragile.

The objective is:

**Make selecting the wrong Sensei graph mechanically detectable, make production readers resolve one canonical active graph identity instead of choosing an Oxigraph port, and make failed graph publication leave the currently active graph and canonical checkout unchanged.**

Do not hardcode another port.
Do not solve this by adding more `--store-url` plumbing.
Do not assume localhost + a responding Oxigraph endpoint means "the correct graph."

## Phase 1 — Map the implementation before changing it

Work from a clean dedicated worktree. Preserve the failed-refresh filesystem output separately as evidence.

Do not run live import/build/rebuild/refresh during this audit.

Find every owner and call path involved in:

1. `sensei build`
2. `sensei build --all`
3. `sensei import`
4. `sensei import --refresh`
5. bootstrap/rebuild/derive
6. `.sensei/project`
7. `.sensei/project-invalid-*`
8. `.sensei/project.lock`
9. graph generations and generation destruction
10. Oxigraph store creation/open/load
11. Oxigraph process/service startup
12. raw store URLs and ports
13. `--store-url`
14. `--graph-marker-file`
15. marker creation and marker validation
16. graph digest generation
17. triple-count/freshness reporting
18. domain/repository identity
19. Awareness service graph access
20. Sensei-code graph/preflight/briefing access
21. test/dev graph instances
22. any service discovery or configuration that selects an Awareness/Oxigraph endpoint.

Search all literal/default ports and URLs, including the historically used values such as `7882`, `10120`, `10122`, and any others you find.

For every endpoint classify it as:

* production canonical;
* runtime-configured;
* test-only;
* development;
* legacy;
* embedded;
* temporary/staging.

Do not infer. Cite the owning code/config for each one.

Produce a topology table:

`component | reads/writes | domain | repo revision | corpus revision | generation | store identity | endpoint | marker | digest | activation owner | source of truth`

Then show all paths by which these identities can currently disagree.

## Phase 2 — Find the existing authority before inventing one

Do not immediately create a new `GraphAuthority` abstraction.

First determine whether Sensei already has any component that claims ownership of:

* active generation;
* generation registry;
* served store;
* graph marker;
* publication;
* graph freshness;
* domain → graph mapping.

If an existing owner is intended to provide this function, prefer completing that owner rather than creating a competing one.

Find comments/contracts that claim graph lifecycle properties and compare them against actual behavior.

Specifically identify whether there is currently one authoritative answer to:

> For domain `github.com/globulario/sensei-code`, which exact graph generation is ACTIVE right now?

If the answer requires independently consulting a port, marker, store, project directory, or process, then graph identity is fragmented.

Report that explicitly.

## Phase 3 — Define the graph identity law

Before implementation, write the invariants.

A production graph must have a stable identity independent of its transport endpoint.

The identity should contain or bind, using existing fields where possible:

* domain;
* repository revision;
* awareness/corpus revision;
* corpus cleanliness/certification state;
* generation ID;
* graph digest;
* triple count;
* store identity;
* graph marker identity;
* activation/publication identity;
* schema/version if one already exists.

A URL or port is transport information, not graph identity.

The core laws are:

1. **One active graph identity per governed domain.**

2. **A valid response from the wrong Oxigraph instance is still the wrong graph and must refuse.**

3. **All production readers resolve graph identity through one owner.**
   They must not independently choose an Oxigraph port.

4. **A governed run pins a graph identity, not merely a URL.**

5. **Every graph query used by that run must prove it belongs to the pinned identity.**
   A silent generation switch is forbidden.

6. **Publication is transactional.**
   Build candidate generation G+1 separately.
   Validate it completely.
   Only then atomically activate it.

7. **The existing ACTIVE graph remains usable until G+1 is fully certified.**

8. **A failed build/import/refresh must not mutate the active graph.**

9. **A failed publication must not leave the canonical checkout rewritten merely because staging was attempted.**
   Generation/candidate output belongs in staging until publication is accepted.

10. **A marker cannot certify a different generation/store than the one being served.**

11. **A generation must not be destroyed while an ACTIVE pointer, served store, marker, or governed run still refers to it.**

12. **Ambiguous graph identity fails closed.**
    Two instances claiming to be ACTIVE for the same domain are an error, not a choice for the caller.

13. **Triple count is evidence, not identity.**
    Two graphs with 35,268 triples are not thereby the same graph.

14. **Raw store URL override is test/dev/maintenance authority, not normal production discovery.**
    If retained, it must be explicit and visibly unsafe/non-canonical.

## Phase 4 — Write failing system witnesses before the architecture repair

Build tests that reproduce the failure family, not only unit tests around a helper.

At minimum:

A. A reader configured to endpoint A receives a valid graph for the wrong domain/revision.
Current behavior must demonstrate why this can pass too far.
Repaired behavior must refuse.

B. Writer publishes to graph A while reader queries graph B.
The mismatch must be detected mechanically.

C. Marker belongs to generation A but store serves generation B.
Refuse.

D. Two reachable graph instances claim the same domain but different active generations.
Refuse ambiguity.

E. Failed `import --refresh` leaves the previously active graph byte-for-byte/addressably unchanged.

F. Dirty awareness corpus is rejected before canonical publication mutation.

G. Failed refresh/build must not leave `.sensei/project.lock` as evidence of a completed publication.

H. A successful-looking build may not activate a generation that becomes unavailable when the builder exits.

I. Interrupted G+1 construction leaves G queryable and ACTIVE.

J. Governed run pins G; G+1 becomes active for new runs.
The existing run either continues against G or refuses deterministically.
It must never silently start querying G+1.

K. Production publication without the required marker/identity metadata refuses rather than creating a vaguely "fresh enough" graph.

L. A valid Oxigraph server on the wrong port must fail identity validation even though HTTP/storage health succeeds.

M. Test/dev overrides remain possible but cannot accidentally become production defaults.

N. The latest observed failure shape:
publication refuses with `mutation_started:false`, yet canonical repository/project state is substantially regenerated.
Demonstrate which current writes happen before the true publication boundary and make that witness fail.

Mutation-test the important identity checks. Kill mutants that:

* ignore domain;
* ignore repo revision;
* ignore generation;
* ignore digest;
* trust endpoint alone;
* trust marker alone;
* trust triple count alone;
* allow two ACTIVE generations;
* activate before validation;
* destroy old generation too early.

## Phase 5 — Implement in narrow vertical slices

Do not attempt a giant rewrite.

### Slice G1 — Read-side identity handshake

Highest priority.

Make every production graph consumer able to answer:

`What exact graph am I talking to?`

Expose/consume a canonical graph identity through the existing Awareness/Sensei interface if possible.

Sensei-code should pin and verify that identity.

A wrong graph instance on the "correct-looking" port must become immediately visible.

Do not change publication yet if G1 can be landed independently.

### Slice G2 — Single resolution owner

Remove production callers' need to independently know the Oxigraph endpoint.

Resolve:

`domain → ACTIVE GraphIdentity → store/generation`

through one owner.

Oxigraph port/URL becomes an implementation detail returned by the owner, not the identity callers select.

Reuse an existing registry/manifest/generation owner if the code already has one.

### Slice G3 — Transactional publication

Build G+1 in isolated staging.

Validate:

* repo/corpus revision;
* generation;
* domain;
* graph digest;
* marker;
* store readability;
* required coverage/readiness;
* generation lifetime.

Then perform one activation transition.

If anything fails before activation:

`ACTIVE remains G`

and the canonical checkout does not become the staging area.

### Slice G4 — Unify CLI lifecycle

Make `build`, `import`, `refresh`, bootstrap, rebuild, and served-store handoff use the same publication/identity primitive rather than each owning a slightly different lifecycle.

Resolve the actual `--graph-marker-file` contract.

Raw `--store-url` must not silently select production graph identity.

### Slice G5 — Doctor/status

Provide or strengthen one read-only command/API that reports the entire canonical state in one place, for example:

`domain`
`repo revision`
`corpus revision`
`generation`
`graph digest`
`triples`
`store identity`
`endpoint`
`marker`
`activation state`
`freshness`
`ambiguity/errors`

If an equivalent command already exists, fix that rather than adding another one.

Its answer must come from the same graph authority used by production readers.

## Phase 6 — Do not touch W3 until the foundation is trustworthy

Do not resume the stale W3 session.

Do not run another W3 task merely to exercise this repair.

Do not run live `build --all`, import, refresh, bootstrap, or rebuild until the graph lifecycle tests prove the candidate architecture safely in a disposable environment.

Do not commit failed-refresh generated artifacts merely to make publication succeed.

Do not merge #168–#172 as part of this front unless their normal independent gate requires it.

The W3 knowledge-limit proof is preserved and already useful. It exposed this deeper infrastructure problem.

## Phase 7 — Disposable-store proof before live migration

Before touching the served graph:

* construct a complete graph generation in a disposable store;
* prove identity handshake;
* prove marker/store/generation agreement;
* prove restart/reopen;
* prove the generation remains valid after builder exit;
* attempt wrong-port/wrong-instance access and observe refusal;
* build G+1 and prove G remains active until activation;
* fail G+1 deliberately and prove G remains untouched;
* activate G+1 and prove new readers resolve G+1;
* prove a reader pinned to G cannot silently consume G+1.

Only after those pass should you propose a live migration procedure.

Do not execute the live migration without reporting the exact procedure and rollback first.

## Deliverable

At the end of this first architecture pass report:

1. current graph topology and all graph/store ports/endpoints;
2. every existing graph lifecycle owner;
3. the actual source of ACTIVE graph identity today;
4. concrete ways wrong-instance selection can currently occur;
5. concrete ways marker/store/generation can diverge;
6. why failed refresh modified canonical filesystem state despite `mutation_started:false`;
7. whether a suitable single owner already exists;
8. invariants written;
9. failing witnesses reproduced;
10. proposed slice boundaries G1–G5 adjusted to the real code;
11. the smallest safe first implementation slice;
12. whether you implemented that slice;
13. tests/mutants/results;
14. exact commit/branch/PR if published;
15. what remains before Oxigraph can be treated as an implementation detail rather than something callers choose by port.

The success criterion is not "refresh works again."

The success criterion is:

**A Sensei consumer cannot accidentally read a healthy but wrong graph, and a failed graph publication cannot corrupt, replace, or ambiguously redefine the currently active graph.**


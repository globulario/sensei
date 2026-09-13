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
  into staging. This is the fix for law 9.
- **G4 unify CLI lifecycle.** One publication/identity primitive for build,
  import, refresh, bootstrap, rebuild. Delete the fabricated `--all` diagnosis.
- **G5 doctor/status.** One read-only report; `sensei doctor`/`metadata` already
  exist and should be completed rather than duplicated.

## 7. What must not be done next

`sensei build --all` must not be run against a served store on the strength of
`cmd_import.go:230`'s recommendation. It is a guess, and the operation it
recommends is destructive.

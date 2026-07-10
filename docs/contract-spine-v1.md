# Contract Spine v1

The contract spine connects the two layers AWG used to keep separate: the
**implementation surface** (the gRPC/HTTP/REST contracts a service actually
exposes) and the **architectural authority** (the semantic guarantees those
surfaces must honor, the invariants that constrain them, and the tests that prove
them). It lets a repair agent answer, at edit time:

> *What rule does this endpoint owe? Why does it exist? What proves it? Do I dare
> claim resolution?*

## The model

```
MetaPrinciple / Invariant
        ▲ constrainedByInvariant
ArchitecturalContract  ◀── violatesContract ── FailureMode
        ▲ realizesContract            │ requiresTest
ImplementationContract               ▼
        ▲ exposesContract / implements   Test
Component / SourceFile
```

Edges (all already in the vocabulary — Spine v1 adds **no** new predicates):

| Edge | Meaning | Authority |
|---|---|---|
| `FailureMode --violatesContract--> ArchitecturalContract` | a known failure breaks this contract | authoritative |
| `ImplementationContract --candidateRealizesContract--> ArchitecturalContract` | a *proposed* link from a surface to a guarantee | **review-only** |
| `ImplementationContract --realizesContract--> ArchitecturalContract` | a *promoted* link: the surface is the executable exposure of the guarantee | authoritative |
| `ArchitecturalContract --realizedByContract--> ImplementationContract` | reverse of the above (arch→impl traversal) | authoritative |
| `ArchitecturalContract --constrainedByInvariant--> Invariant/MetaPrinciple` | the law the contract must satisfy | authoritative |
| `ArchitecturalContract --requiresTest--> Test` | the proof the contract requires | authoritative |

## How a surface becomes architecture (the safety ladder)

```
extractors find surfaces        proto-scan (gRPC) · http-scan (HTTP routes)
        ↓
candidate generator proposes    awg suggest-realizations  (conservative, candidates only)
        ↓
human review confirms           awg promote-realization   (explicit, one at a time)
        ↓
realizesContract = authority     briefing speaks it; audit protects it
```

Each step is one of these tools:

- **`http-scan`** turns `mux.Handle("/route", h)` registrations into inferred HTTP
  implementation contracts (`kind: http`). `make http-contracts`.
- **`awg suggest-realizations`** scores `(impl, arch)` pairs on hard evidence
  (shared source file, `coversPath` glob, same directory, shared failure-mode
  context, name-token overlap) and writes `candidateRealizesContract` entries.
  Candidates only — never `realizesContract`. Name overlap alone yields nothing;
  path/dir overlap alone is low confidence; a read-only surface won't realize a
  write-only contract without a strong signal; candidates never cross repo domains.
- **`awg promote-realization --impl <id> [--arch <id>]`** moves one reviewed
  candidate into authoritative `realizations:`. It refuses missing / ambiguous /
  already-authoritative, never bulk-promotes, and never scores. Promotion is a
  review action, not a scoring action.

## Primary demo: `/api/save-config`

The Globular gateway's `/api/save-config` handler (`NewSaveConfig`) requires a
header token and returns `401` without it — config is never mutated without valid
authority. The spine, end to end:

```
HTTP implementation contract  contract.http.api_save_config        (extracted by http-scan)
  --realizesContract-->                                            (promoted from a candidate)
contract.config_mutation_requires_valid_token                      (authored architectural contract)
  --constrainedByInvariant--> meta.authority_must_express_uncertainty
  --requiresTest-----------> internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken
                             (+ TestSaveConfig_InvalidToken401, TestSaveConfig_ValidTokenSaves204)
```

### Briefing renders this as a repair instruction

`awg briefing --file internal/gateway/handlers/config/config.go`:

```
Realized architectural contracts (AUTHORITY — respect or do not claim resolution):
- HTTP /api/save-config realizes contract.config_mutation_requires_valid_token
  - The contract requires: Config mutation requires a valid token
  - Constrained by: meta.authority_must_express_uncertainty
  - Required proof: internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken, …
  - Do not claim resolution if this contract is bypassed, weakened, or left untested.

Candidate realized contracts (REVIEW-ONLY — not authority; promote with `awg promote-realization`):
- HTTP /api/cors-diagnostics ~candidate~> contract.config_mutation_requires_valid_token (unverified — do not treat as a guarantee)
```

## Reproduce it

From the `awareness-graph` repo (with the services + Globular repos as siblings,
and `awg serve` running):

```bash
make http-contracts-check            # HTTP impl contracts are fresh from gateway routes
awg suggest-realizations --check     # candidate set is fresh and conservative

# Promotion is a one-time review action. /api/save-config is already promoted in
# this repo, so re-running it correctly refuses ("no candidate … to promote").
# For an un-promoted candidate you would run, e.g.:
awg promote-realization --impl contract.http.api_save_config \
                        --arch contract.config_mutation_requires_valid_token

awg rebuild                          # emit realizesContract / realizedByContract into the store
awg briefing --file internal/gateway/handlers/config/config.go   # see the authority chain
awg audit --check                    # the gate (below) is green and FAIL-level
```

## Governance guarantee

The spine is designed so the graph cannot quietly grow fake authority:

1. **Candidates are never authority.** `candidateRealizesContract` is rendered in a
   separate, explicitly REVIEW-ONLY briefing section and is never treated as a
   guarantee. The generator emits *only* candidates.
2. **Promotion is explicit.** `realizesContract` is created only by
   `awg promote-realization` (or hand-authoring) — one reviewed pair at a time,
   never in bulk, never from path/name overlap.
3. **Unproven authority fails audit.** The `contract-verification-wiring` audit
   check is **FAIL-level**: a home-domain contract that claims `requiresVerification`
   but wires none of `requiresTest` / `constrainedByInvariant` / `violatedBy` /
   `detect` fails `awg audit --check`. Authority must carry its proof.
4. **Benchmark fixtures are excluded.** Repo-tagged contracts (the Multi-SWE-bench
   `frozen_contract_set` fixtures under `eval/`, e.g. `github.com/example/tinyrepo`)
   have their own verification model (the frozen-contract gate) and are *not*
   policed by this home-domain gate.

The result: *no resolution without a respected contract* becomes something the
agent is told at edit time and the audit enforces over the corpus.

## Open modeling note: "proof" / Evidence is overloaded

The governance guarantee above leans on a single notion — *authority must carry
its proof* — but that word covers two different kinds of grounding, and the spine
does not yet distinguish them:

- **Proof of a *fact*.** For a factual contract (a surface behaves a certain way,
  a route returns `401` without a token), `requiresTest` → Test is genuine proof:
  the test executes and the claim is true or false. This is the case the spine is
  built around and the `/api/save-config` demo shows.
- **Endorsement of a *norm*.** For a behavioral / normative contract — one
  `constrainedByInvariant` a MetaPrinciple like
  `meta.authority_must_express_uncertainty` — the grounding is *not* proof. A
  principle is not true or false; it is good or bad in a context. Its Evidence is
  **endorsement or outcome** ("applied here, paid off"), not a passing assertion.

Today both collapse into the same `Evidence` node and the same "carry your proof"
audit. That is fine operationally — the FAIL-level
`contract-verification-wiring` check still does its job — but it conflates two
senses of grounding that validate differently, and it will matter the moment we
want to ask *why* the graph believes a norm versus *whether* a fact holds.

This is a latent modeling question, not a bug. Resolution options, when it earns
attention: (a) leave `Evidence` overloaded with a documented convention, (b) add
an `evidenceKind: proof | endorsement` discriminator, or (c) split the node type.
The center of gravity of the graph is behavioral (see
[memory-correctness-tradeoff](design/memory-correctness-tradeoff.md), *"Factual
vs behavioral memory"*), so this is more than a corner case — most Evidence in a
mature graph will be endorsement, not proof.

# Contract-first resolution — design

> Foundational meta-principle (authored in the canonical corpus
> `docs/awareness/generic/state_authority_invariants.yaml`, category `perception`):
>
> **`meta.contract_must_be_explicit_before_resolution`**
> Operational rule: **`meta.no_resolution_without_a_respected_contract`**
>
> Sensei's first duty before repair is to identify, infer, or propose the governing
> contract/invariant. A change may only be called **resolved** when the contract
> is explicit, respected by the patch, and supported by evidence. If no contract
> can be identified, the result is not "resolved" — it is **"contract unknown"**
> with a proposed contract or a revision request.

## Why

The SWE-bench oracle (hidden tests) measures *did you match the maintainer's
interpretation*, not *did you resolve the issue in this codebase*. Those coincide
only when the task is well-defined. When the contract is implicit, "resolving"
collapses into *guess the oracle* — playing a game without knowing the rules.
Sensei's job is the step before repair: **make the contract explicit so the task is
well-defined**, then resolution becomes a checkable property (respect), not a
guess.

## The contract-first repair protocol

The full agent-facing operating manual — contract statuses, required output
templates, Sensei tool-usage order, the extraction duty, and forbidden behaviors —
lives in the **services** repo (the agent operating layer, where product repairs
happen) at `docs/design/contract-first-resolution-protocol.md`, wired into the
services `CLAUDE.md`. The summary below is the core loop.

Six steps. No edit happens before step 4; no "resolved" status before step 6.

1. **Retrieve existing contracts.** Query the graph for what already governs the
   candidate files/symbols:
   - `sensei impact --file <f> --domain <repo>` → invariants, failure-modes, intents
     anchored to the file
   - `sensei briefing --file <f> --domain <repo>` → architecture + governing rules
   - `sensei resolve <symbol|node> --domain <repo>` → a specific contract node
   Candidate files come from the issue text + repo search — **never the gold
   patch**.

2. **Infer a candidate contract** when retrieval is thin. Sources, in order of
   strength:
   - prior errors/scars on these files (`cold-bootstrap` revert/regression mining)
   - failure-modes that name this code path
   - intents mined from history/PR review (`intent-mine`)
   - the tests themselves as *partial contract shadows* (what they assert is a
     lower bound on the contract)
   - boundary/ownership: who owns the semantic truth this code touches

3. **Classify confidence** — the contract carries one of four labels, which set
   how much authority it has and how the gate behaves:

   | confidence | meaning | source | gate behaviour |
   |---|---|---|---|
   | `explicit` | authored invariant/contract exists | corpus / promoted | gate = block-eligible |
   | `inferred` | grounded from graph + history + tests | step 2, multi-signal | gate = warn, evidence required |
   | `candidate` | proposed, single-signal, unconfirmed | step 2, weak | advisory only; ask to confirm |
   | `unknown` | no contract derivable | — | **stop**: emit revision request, no fix-as-resolution |

4. **Agent states the contract before editing.** The agent must write down, in
   its own words and grounded in step 1–3 output: *this is the contract this
   change is bound by, at confidence level X, because <evidence>.* If it cannot —
   the honest output is a revision request (step 3 `unknown`), not a patch.

5. **Gate the final patch against the stated contract.** Run the contract's
   detect rule over the diff:
   - `sensei gate --diff <range> --domain <repo>` / `sensei edit-check` for pattern
     contracts
   - for non-pattern contracts, an LLM judge that checks the diff against the
     stated contract (reads the *patch*, not the answer key — leak-proof)
   A violation here means the patch does not respect the contract, regardless of
   tests.

6. **Only then allow "resolved".** `resolved` requires: a contract at `explicit`
   or `inferred` confidence **and** a clean gate **and** supporting evidence.
   Otherwise the status is one of: `respected-but-unfixed`,
   `fixed-but-no-contract` (oracle match), `contract-violation`, or
   `contract-unknown`.

## Benchmark stratification — four measurements, never one number

A result MUST separate these, because they answer different questions and have
different oracles:

| Measurement | Question | Oracle | Leak-proof? |
|---|---|---|---|
| Structural awareness | did Sensei surface the right component/blast-radius? | graph vs. file | n/a |
| Contract discovery | did Sensei identify/infer a contract at all, and at what confidence? | the protocol's step 1–3 | yes (no answer key) |
| Contract respect | does the patch honor the stated contract? | `sensei gate` on the diff | **yes** — reads the patch |
| Hidden-test fix rate | did it match the maintainer's interpretation? | SWE-bench FAIL_TO_PASS | no (can leak) |

The headline Sensei claim lives in *contract discovery* + *contract respect*, not
fix-rate. Fix-rate is reported, but framed as the oracle it is.

## Phasing — applied to the current pilot

- **Phase 1 (running now): structural-impact baseline only.** cli/cli's Mode C
  graph currently has structural context (components, dependencies, file
  anchors) but **no frozen contract set** — no invariants/intents/failure-modes.
  So this pilot can measure structural awareness and the hidden-test fix rate,
  and it is a useful baseline — but it is **NOT** evidence of contract-respecting
  resolution, and must not be presented as such. Under
  `meta.no_resolution_without_a_respected_contract`, most cli/cli tasks here are
  `contract-unknown`.

- **Phase 2: freeze contracts, then score.** Before scoring resolution or
  respect-rate, cli/cli needs an explicit, **frozen** contract set — authored
  and/or scar-mined invariants/intents with detect rules, version-pinned so the
  gate is reproducible. Only then do contract discovery and contract respect
  become measurable, and only then can a task earn `resolved`.

## Status

- Meta-principle: promoted (2026-06-17). Authored in the canonical corpus
  (`docs/awareness/generic/state_authority_invariants.yaml`, category
  `perception`), regenerated into the cold-start pack, baked into the seed, and
  classified in the awareness-graph coverage registry.
- Protocol: documented as the agent operating manual in the **services** repo
  (`docs/design/contract-first-resolution-protocol.md`) and wired into the
  services `CLAUDE.md` — the operating layer governs repairs where they happen.
  Enforced as agent discipline, **not yet a mechanical CI gate** (that is the
  Phase-2 `sensei gate` step against a frozen contract set). Not yet wired into the
  eval harness (Mode C).

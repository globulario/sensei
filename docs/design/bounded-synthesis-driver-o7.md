# Bounded Synthesis Driver O7

Status: implementation checkpoint

## Purpose

O7 is the deterministic orchestration layer that turns the already-accepted O1 through O6C contracts in their permitted order. It does not reason about architecture, choose authority, enlarge budgets, or give a provider mutation outside O3.

The first O7 checkpoint drives one in-process session from `created` through interpretation, planning, generation, evaluation, retry/replan, and a terminal O1 outcome. A successful run stops at `candidate-ready-for-admission`. Admission, candidate application, verification, commit, push, pull-request creation, review, and merge remain a separate final closure checkpoint.

## Implemented surface

- exact O2 interpretation and planning request construction, execution, receipt capture, command mapping, and O1 transition;
- a separate O3 generation factory and content-addressed candidate store;
- a concrete O4 engine with caller-bound policy, materializer, evaluator bindings, evidence resolver, cleanup, composition, and terminal unavailability;
- a current-phase-only dispatcher for created, planning, planned, attempting, retry, replan, succeeded, and failed;
- typed nonterminal provider, runner, and step-limit stops;
- a timestamp-independent, self-digested O7 run receipt binding every O2, O3, O4, O1, and candidate identity;
- explicit refusal to resume an external half-finished evaluating phase while O3's verified handoff remains process-local;
- an end-to-end proof using a real Git object database, exact workspace identity, O2 cognitive providers, the O6C generation bridge, O3 candidate sealing, fresh O4 materialization, deterministic evaluator execution, O1 success, and content-addressed artifact reload.

## Exact owner sequence

1. O1 owns session phase, retry/replan budget, and transitions.
2. O2 owns provider execution documents, observations, receipts, and mapping into O1 commands.
3. O3 owns repository snapshotting, the candidate workspace, candidate evidence, artifact sealing, and the verified generation handoff.
4. O4 owns evaluator policy, candidate materialization, evaluator execution, composition, evaluation recommendation, and the second O1 transition.
5. O7 only selects the next already-legal owner call from the current typed phase.

## Provider separation

O7 receives three explicit capabilities:

- one interpretation `providerport.Provider`;
- one planning `providerport.Provider`;
- one O3 `GenerationProviderFactory`.

The generation factory is never used for interpretation or planning. Interpretation and planning providers never receive `CandidateWorkspace`. No dynamic provider routing, fallback, or capability inference occurs.

## Evaluation engine

O7 includes a concrete O4 engine rather than hiding evaluation behind an untyped callback. The engine receives:

- the exact candidate store used by O3;
- an explicit policy factory bound to the current session, attempt, and candidate;
- a candidate materializer bound to repository domain and object database;
- an explicit evaluator binding for each policy evaluator ID;
- an evidence resolver;
- a caller-supplied clock.

For each evaluator, O7 asks O4 to materialize a fresh surface, construct the closed `EvaluationInput`, execute the evaluator, close the surface, record cleanup truth, and compose through `CompleteEvaluation`. Required materialization or evaluator absence becomes O4's governed unavailability path.

## Driver result

Every run returns:

- the final O1 `SessionState` and ordered events;
- exact O2 executions for interpretation and planning;
- ordered O3 handoffs and runner receipts;
- ordered O4 receipts and evaluations;
- the latest accepted Interpretation and Plan;
- the latest sealed CandidateArtifact when one exists;
- a closed, self-digested O7 run receipt.

The O7 receipt has one of these dispositions:

- `candidate-ready`
- `terminal-failure`
- `provider-stopped`
- `runner-stopped`
- `step-limit-reached`

`provider-stopped` and `runner-stopped` preserve the nonterminal O1 state. O7 never invents an O1 terminal reason merely because an external capability was unavailable.

## Hard laws

1. The current O1 phase is the sole dispatcher.
2. Every transition goes through `synthesis.Transition`.
3. Every completed provider result goes through `providerport.MapToCommand` before O1.
4. Non-completed interpretation or planning output stops the driver with typed O2 evidence and does not mutate O1.
5. Generation always goes through O3 and a fresh workspace-bound provider.
6. Evaluation always goes through O4's accepted handoff, policy, candidate, surfaces, evaluators, and composer.
7. Retry and replan are taken only when O1 enters those phases and only while O1's precommitted budget allows the corresponding start command.
8. The driver has a separate immutable max-step bound. It cannot enlarge O1 budgets.
9. The driver never substitutes a candidate, provider result, evaluator result, or receipt.
10. The driver stops at candidate-ready-for-admission. O5 remains the next owner.
11. No repository application, commit, push, GitHub call, or merge exists in this checkpoint.
12. Contract/programming errors return Go errors. Expected capability stops return a valid O7 receipt.

## Required proof

- complete happy path from Created to candidate-ready through real O2, O3, and O4 owners;
- retry-generation consumes exactly one retry and creates a new attempt;
- replan consumes exactly one replan and creates a new plan generation;
- interpretation/planning provider unavailability preserves the current O1 phase;
- O3 non-verified disposition stops before O4;
- evaluator unavailability follows O4's terminal path;
- max-step exhaustion cannot loop forever;
- stale request, result, plan, handoff, policy, candidate, and evaluator evidence remain rejected by their existing owners;
- O7 receipt identity is timestamp-independent except for explicitly observed fields;
- full repository CI, dogfood, generated import graph, and cold-start smokes pass on the exact accepted head.

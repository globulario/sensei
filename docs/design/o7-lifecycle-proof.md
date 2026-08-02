# O7 Lifecycle Proof Matrix

The O7 implementation is exercised through the same typed driver entry point for each lifecycle outcome:

- real-owner happy path: O2 interpretation and planning, O6C generation, O3 sealing, O4 materialization and evaluation, O1 candidate-ready;
- retry: one mechanical failure consumes exactly one retry and produces attempt two without changing plan generation;
- replan: one plan-level failure consumes exactly one replan, records plan generation two, and produces attempt two;
- provider stop: non-completed interpretation preserves the `created` phase and produces no O1 terminal receipt;
- runner stop: non-verified O3 evidence stops before any O4 call;
- required evaluator absence: O4 terminates through its governed unavailable disposition;
- step limit: the separate O7 bound stops a run without changing O1 retry or replan budgets;
- receipt identity: changing observation timestamps does not change the O7 semantic digest.

All generation and evaluation tests use deterministic local implementations and require no external credentials.

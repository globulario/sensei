# Cold-start demo: payment "paid" state

The smallest possible AWG project. One source file, one rule. It shows the
whole loop: a rule you write becomes a briefing an AI agent (or you) gets
**before** editing the risky file.

No Globular. No cluster. No database to run. About 3 minutes once `awg` is
built (see the repo [INSTALL.md](../../INSTALL.md)).

## The rule

`docs/awareness/invariants.yaml` says: an order is "paid" only after the
payment **processor** confirms it — never from a local cache write. The
source file `src/payment_processor.py` contains both the wrong shape
(`mark_paid`, sets paid from cache) and the right one (`mark_paid_correct`,
confirms first).

## Run it

From this folder, with `awg` and `oxigraph` on your PATH (the repo
`./scripts/install.sh` puts them in `bin/`):

```bash
cd examples/payment-cold-start

awg init                 # adds the 83-principle pack + hooks alongside the rule already here
awg serve -no-seed &     # local store + server; -no-seed = your rules only, not Globular's
awg build                # compile docs/awareness into the graph
awg briefing -file src/payment_processor.py -task "refactor mark_paid"
```

## Expected output

```
Status: BRIEFING_STATUS_OK

Awareness briefing for src/payment_processor.py
Task: refactor mark_paid

Direct invariants:
- [critical] payments.paid_state_requires_processor_confirmation — An order records as paid only after processor confirmation — never from a local cache write

Referenced IDs:
  - invariant:payments.paid_state_requires_processor_confirmation

(generated in 2ms)
```

That's the point: an agent about to touch `payment_processor.py` is told
the rule first. The rule also links to the universal principle behind it —
`awg init` installed the pack, so this resolves:

```bash
awg resolve invariant meta.storage_is_not_semantic_authority
```

## What you committed vs what `awg init` generated

This demo commits only the two files **you** would write:
`docs/awareness/invariants.yaml` and `docs/awareness/high_risk_files.yaml`,
plus the source. `awg init` generates the rest (the principle pack, the
Claude Code hooks, `CLAUDE.md`) — those are `.gitignore`d here so the demo
stays small and obvious.

## Cleanup

```bash
kill %1                   # stop the server
rm -rf .awg .claude docs/awareness/meta_principles.yaml \
       docs/awareness/failure_modes.yaml docs/awareness/incident_patterns.yaml \
       docs/awareness/activation_rules.yaml CLAUDE.md
```

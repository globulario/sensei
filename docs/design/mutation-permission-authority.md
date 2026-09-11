# Mutation permission: one owner, and the bridge that is not built here

**Status:** design record for the G3 repair. Scope-limited on purpose.

## What `permission.modify` means

**Permission to consume a NEW mutation capability.**

It does **not** mean permission to apply an operation whose capability was
already consumed. The two are different questions with different ledger
preconditions, and one string cannot answer both. After consumption the correct
answer is "no new capability" — projected as `waiting`, never as `refused`,
because a spent capability is not a repudiated one and `refused` would assert
that an authorized application was unauthorized.

## The one owner

`tasksession.resolveMutationPermission` is the sole producer of this predicate.
`AdvanceTask`, `projectControlStatusAndClosure` (behind `ControlStatus`,
`ResolveControlAndClosure` and `BuildTaskBriefing`) and the persisted-projection
path all route through it. None re-derives it, and none reads
`decision.MutationCapability` directly.

It returns the governance fold it used, so a caller needing the disposition
reuses that fold rather than folding a second time.

## The protocol boundary

| Chain state | Authority | Behaviour |
|---|---|---|
| typed authority resolved (`gov.Resolved`) | **the verified ledger** | grant only while a typed decision binds and its capability is unconsumed |
| not resolved | the file protocol | preserved, with the rule `AdvanceTask` already applied: the legacy path may not hand out a positive mutation capability on its own |
| governance-integrity error | — | **fail closed**; grant nothing |

Established file-protocol workflows keep working under their own contract. A task
that has resolved typed governance cannot fall back to file authority through any
reader.

## The cache carries the projection, never the permission

A persisted `control/latest.yaml` records what governance said when
`advance-task` last ran. Serving it re-asserts a grant the ledger may since have
withdrawn. Measured on real records before this repair: seven persisted
projections asserted `modify: admitted` for chains carrying no authority
resolution at all. The permission is therefore always re-derived; only the rest
of the projection is reused.

## Compatible semantic vocabulary extension

`taskcontrol.ActionConsumeCapability = "consume_capability"` names the step
between an admitted capability and any edit. The old vocabulary could not express
it, which is why the selector reached for `perform_admitted_edit` and instructed
agents to mutate before consuming. The token matches
`tasksession.AdvanceNextConsumeCapability`, already used for the same step.

**This is compatible, not inert.** It widens the persisted `NextAction`
vocabulary and changes `StateDigest` whenever the action is selected. Audited at
`739133dc`: no validator, no exhaustive switch and no JSON schema constrains the
action vocabulary; the only consumer is one equality comparison against
`ActionCompleteTask` (`control.go:821`), which is unaffected. Serialization,
round trip and digest sensitivity are proven by
`TestConsumeCapabilityIsAPersistableActionThatMovesTheDigest`.

The governance disposition reaches the selector as a *parameter*, not as a field
on `TaskControlState`, so no persisted struct or schema version changes.

## One evaluation per response

Permission, governance disposition, next action and receipt digest are derived
from a single evaluation and refreshed together. Refreshing only the permission
was its own defect: the cached action still described the superseded permission,
and the digest identified a state that was never returned. Both the replay path
and the cache path route through `refreshCachedEvaluation`.

## Valid consumption versus governance unavailable

| Verified condition | Permission | Action |
|---|---|---|
| capability available (unconsumed) | `admitted` | consume the capability |
| capability validly consumed | `waiting` | reconcile and record (`verify-admission`) |
| governance cannot be verified | none | `GovernanceError`; never consume or apply |
| typed authority proven absent | file protocol, unchanged | file protocol, unchanged |

A mismatched consumption receipt is a `GovernanceCodeConsumptionUnbound`
integrity error, never an ordinary spend. `CapabilityConsumed` selects
reconciliation and never an unconditional mutation: a process may have applied
and crashed before recording, and that recovery question belongs to the bridge.

## Semantic limitation: one shape, two meanings

`taskcontrol.TaskControlState` represents **both** a persisted receipt
(`control/latest.yaml`) and a refreshed response returned to a caller. The active
pointer identifies the artifact that actually exists on disk; the response's
`ReceiptDigestSHA256` identifies the evaluation being returned. After a replay
these are legitimately different values, and neither is wrong.

**Consumers must not infer persistence from a response's digest alone.** A
digest returned by `ControlStatus` or `AdvanceTask` says "this is the state I am
giving you", not "these bytes are stored". Aligning them by writing a new
generation would falsely report a publication that did not happen, so the
divergence is deliberate.

Whether that distinction is sufficiently explicit — in the type, the field names,
or only in this note — is an open review question.

## What this repair deliberately does NOT do

It does not connect task-ledger admission to the file-protocol application chain.
That bridge is a separate design, and when it is built:

- **Durable receipt identities:** `TaskBinding` and
  `CapabilityConsumptionDigestSHA256`. Today `candidateapply` carries neither —
  an application joins to a decision by digest, but to no task and to no
  consumption.
- **Runtime authorization comes from a verified ledger snapshot**, not from a
  receipt read in isolation.
- **Consumed operation IDs resolve through the admitted `ChangePlan`** before
  their targets are compared with applied paths. The IDs are not paths, and
  comparing them directly would be a category error.

Recovery for that bridge must distinguish authorization-available,
application-in-progress, applied-but-unrecorded, and verified — returning
unknown when the evidence cannot separate them, and never letting an absent
observation select "not attempted".

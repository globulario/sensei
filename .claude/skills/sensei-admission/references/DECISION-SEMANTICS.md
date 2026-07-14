# Decision Semantics

## `admitted`

The exact action may be attempted inside the envelope. Correctness remains
unproven until the repository's normal proof is run.

## `admitted_with_conditions`

The action may be attempted inside the envelope, but conditions remain visible.
Do not erase pending tests, observations, review, or governance from the report.

## `waiting`

Do not mutate. The work is waiting on architect input, evidence, governance, or
another explicit prerequisite. Route to `sensei-closure` when the missing work is
architectural knowledge.

## `refused`

Do not mutate. The request conflicts with the bundle or policy.

## `uncertifiable`

Do not mutate. The system could not establish the facts needed to issue a
bounded permission.

Scope compliance is not correctness certification. A compliant diff can still be
wrong, incomplete, untested, or rejected in review.

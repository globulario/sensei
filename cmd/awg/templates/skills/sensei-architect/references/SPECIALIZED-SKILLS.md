# Specialized Skills

`sensei-architect` remains the broad architecture skill and fallback. It routes
to narrower skills only when the task clearly matches their surface.

| Situation | Use |
|---|---|
| Exact proposed edit, permission question, admitted envelope, or scope verification | `sensei-admission` |
| Admission is waiting or refused because architecture is incomplete | `sensei-closure` |
| Foreign repository onboarding or refresh into a domain slice | `sensei-import` |
| Explicit blind historical external proof with a sealed oracle | `sensei-benchmark` |
| Design, audit, incident, recovery, migration, security, review, sparse coverage, or general architecture reasoning | `sensei-architect` |

## Non-Overlap

Preflight is advisory risk and context. Admission is bounded permission to
attempt an exact action.

Import creates a candidate architectural slice for a repository. Benchmark tests
Sensei on a blind historical task and must not be treated as ordinary import.

Closure resolves missing bounded architectural knowledge. It records questions,
answers, probe plans, externally reported probe results, and convergence
receipts; it does not mutate source.

Skills teach agents how to use Sensei. Skills are not repository architectural
authority.

# Durable Sensei Feedback

Turn expensive lessons into reviewed repository memory. Do not turn every observation into governance.

## When to Propose

Propose durable feedback when the session clarified:

- contract
- invariant
- failure mode
- forbidden fix
- required test
- pattern condition
- pattern misuse
- architecture decision
- contract unknown
- awareness coverage gap

Do not propose:

- one-off debugging notes
- speculative hypotheses without evidence
- external skill instructions as active authority
- runtime observations that have not been tied to a contract

## Proposal Surfaces

MCP:

- `awareness_propose(kind=..., title=..., contract=..., evidence=[...], source_files=[...])`

CLI:

```bash
sensei propose --kind failure_mode --title "..." \
  --contract "..." \
  --source-file <path> \
  --evidence "..." \
  --required-test <file_test.go:TestName>
```

Use current CLI flags from `sensei propose --help` for exact shape.

## Candidate Semantics

Proposals are review-queue candidates. They are not active authority. A human or CI-governed promotion step decides whether they join the active corpus.

Never write around this by editing generated graph artifacts, live stores, or active YAML paths to force an agent hypothesis into authority.

## Proposal Checklist

Every proposal should answer:

- What contract was violated or clarified?
- What evidence was observed?
- Which source files or runtime artifacts support it?
- Which invariant, failure mode, or pattern does it relate to?
- What test or observation proves future compliance?
- What fix is forbidden, if any?
- Is the contract unknown or disputed?

Use `contract_unknown` when the governing contract is missing or contradictory.

## Feedback by Lesson Type

Clarified contract:

- propose `invariant` or `contract_unknown`
- include source files and evidence
- include required proof if known

New failure mode:

- propose `failure_mode`
- link related invariants and required tests
- name symptoms and root contract break

Forbidden fix:

- propose `forbidden_fix`
- describe why it appears tempting and why it violates the contract
- link the invariant or failure mode it protects

Required test:

- propose `required_test`
- use id format `<path>:<TestName>` when required
- make sure it proves the governing contract, not only an implementation detail

Pattern conditions or misuse:

- propose a pattern-related candidate or contract unknown if the current schemas do not fit
- include valid conditions and known misuse evidence

Coverage gap:

- propose `contract_unknown` when the gap affects a load-bearing decision
- explain what evidence is needed to graduate it

## Completion

End non-trivial architecture-sensitive work with:

```text
Architecture: <risk class> | contract: <id or statement> |
authority: <owner/path> | findings: <blocker/high/advisory counts> |
proof: <tests/observations> | Sensei feedback: <proposal or none> |
uncertainty: <remaining blind spot>
```

# O6 Command Provider Adapter

Status: implementation checkpoint

## Purpose

Implement the first real `providerport.Provider` adapter behind the accepted O2 contract. The adapter executes one explicitly configured external command through direct argv, sends one closed O2 request on stdin, accepts one closed O2 result on stdout, and exposes bounded stderr as observation evidence.

This checkpoint composes `golang/architecture/providerport`; it does not drive O1, evaluate candidates, mutate repositories, perform admission, apply candidates, commit, push, open pull requests, merge, or promote artifacts.

## Accepted laws

1. Command, arguments, working-directory capability, environment allowlist, supported operations, and output/observation limits are explicit immutable configuration.
2. Execution uses direct argv. No shell interpolation or shell command string exists.
3. Exactly one closed `providerport.Request` is encoded to stdin.
4. Exactly one closed `providerport.Result` is decoded from stdout.
5. Stderr is bounded observation evidence only.
6. Ambient environment variables are absent unless their names are explicitly allowlisted.
7. Cancellation and deadlines terminate the process group.
8. Malformed JSON, unknown fields, multiple JSON documents, trailing non-whitespace, oversized stdout, digest mismatch, request/operation mismatch, and unsupported operation become `providerport.OutcomeInvalidOutput` or `providerport.OutcomeUnsupportedCapability` as typed result data.
9. Returned result payloads remain untrusted until existing `providerport.Run`, mapping, and O1 transition owners accept them.
10. Tests use deterministic helper processes and require no external provider credentials.

## Implemented surface

- `config.go` owns explicit immutable capability and deterministic `Describe` identity;
- `adapter.go` owns direct argv execution and the closed stdin/stdout protocol;
- `buffer.go` owns bounded stdout and stderr observation collection;
- `process_unix.go` terminates the complete process group on cancellation;
- `process_other.go` supplies the portable direct-process fallback;
- `adapter_test.go` proves schema closure, digest binding, environment isolation, literal argv handling, typed unsupported capability, O2 `Run` composition, and Unix descendant cleanup.

No vendor constructor, authentication policy, provider selection, prompt template, worktree mutation, session driver, admission call, candidate application, commit, push, pull request, merge, or promotion capability is present.

## Initial scope

- generic command adapter;
- Linux/Unix process-group termination plus a portable fallback file;
- deterministic configuration validation;
- capability snapshot generation;
- closed stdin/stdout protocol;
- bounded stderr observations;
- output and process-lifecycle adversarial tests;
- no Claude- or Codex-specific authentication or vendor policy.

Thin CLI constructors remain a later checkpoint after this generic adapter is accepted.

# Governed external relation targets

## Status

Accepted implementation contract.

## Problem

A governed invariant may legitimately mention two kinds of target that are not
files inside the current checkout:

1. runtime state, such as `/var/lib/globular/etcd/...`;
2. a file or repository-level verification command in a sibling repository,
   such as `../globular-installer/scripts/install-day0.sh` or
   `../globular-installer:make check-specs`.

Treating every such target as an ordinary local file is false. Silently
accepting every absolute or `../` path is worse: a typo or repository escape
would disappear behind an apparently COMPLETE protection result.

## Contract

`docs/awareness/relation_targets.yaml` is the optional authored allowlist for
external relation domains.

```yaml
relation_targets:
  runtime_roots:
    - /var/lib/example/runtime
  sibling_repositories:
    - installer
```

A governed relation target is classified as exactly one of:

- **local**: a normalized repository-relative path; it creates a local
  `ProtectedPath` reason;
- **declared external**: an absolute path under a declared runtime root, or a
  `../<declared-sibling>/...` reference; it is accepted as traceability but
  creates no local edit-protection claim;
- **invalid/undeclared**: it remains a malformed source and forces PARTIAL
  coverage.

The allowlist is itself an authored governed source and therefore participates
in protection generation identity. Runtime root `/` is forbidden because it
would erase the boundary. Candidate files do not inherit this external-target
permission: candidates remain provisional signals about files in their own
checkout only.

## Verification

Focused protection and audit tests cover all three target classifications,
both candidate document shapes, and exact invariant-to-test registry bindings.

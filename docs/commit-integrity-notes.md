# Commit integrity notes

A running log of commits whose contents did not match their stated scope, and
how the record was corrected. The point is honesty over tidiness: we correct
the record forward rather than rewrite pushed history.

This file exists because Sensei's whole premise is that a system should be honest
about what it contains. A commit message that claims one thing while the diff
does another is exactly the drift the tool is built to catch — so when it
happens to us, we name it.

---

## 765037e — "launch kit" commit accidentally bundled two extractor files

**Date:** 2026-06-12
**Commit:** `765037e` — *"launch kit: tester targets, feedback triage, feedback form, release checklist"*
**Stated scope:** public launch / docs packaging — the message says "docs only, no engine changes."

**What actually happened:** the commit was created with `git add -A`, which swept
in two extractor engine files that were sitting uncommitted in the working tree
and were **not** part of the launch kit:

- `golang/extractor/file_annotations_yaml_import.go` (new — a `file_annotations`
  schema importer mapping source files to the invariants they enforce/protect)
- `golang/extractor/yaml_import_dir.go` (registers the `file_annotations` schema)

These had no prior git history; they first appear in `765037e`.

**Assessment:** the two files build and `go test ./golang/extractor/` passes, so
they did not break CI (the launch-kit CI run was green). The only harm is the
inaccurate commit message and an unreviewed engine change landing under a
"docs only" label.

**Correction (this commit):** the code is **preserved** — it builds and passes,
and reverting it would discard in-progress work. Rather than rewrite pushed
history (a force-push on master), the record is corrected forward: this note
documents the discrepancy, and a lightweight scope guard
(`scripts/check-commit-scope.sh`) plus a release-checklist item now catch this
class of drift going forward.

**Resolution of the feature itself:** the `file_annotations` work the two
files belong to was subsequently completed and committed properly in
`047ab26` — *"feat(extractor): add file_annotations, source_patterns,
rendering_groups schemas"* — a correctly-labeled feature commit (not a
docs-only claim). So the two files prematurely bundled into `765037e` are now
part of a landed, properly-scoped feature. The only defect was `765037e`'s
inaccurate message; the code itself is intended and complete.

**Lesson:** docs-only / adoption-package commits must have their changed-file
list checked against engine paths (`golang/`, `cmd/`, `internal/`) before
committing. Prefer `git add <explicit paths>` over `git add -A` when the intent
is narrow. The guard now enforces this when a message claims docs-only scope.

# Release / send-to-tester checklist

Run before sending AWG to testers or tagging a release. Every item is a real
command or a one-line check — no judgment calls. If any fails, stop.

## Automated gates (must be green)

```bash
# 1. CI is green on the latest master commit
gh run list --repo globulario/awareness-graph --branch master --limit 1

# 2. Cold-start smoke (the stranger path, end to end)
scripts/fetch-oxigraph.sh
scripts/smoke-cold-start.sh                      # must print SMOKE PASS, exit 0

# 2b. Local runtime bundle smoke (prebuilt path, extracted)
scripts/smoke-local-bundle.sh                    # must print BUNDLE SMOKE PASS, exit 0

# 2c. Managed-governance publication smoke (separate control plane path)
scripts/smoke-governance-publication.sh          # must print GOVERNANCE SMOKE PASS, exit 0

# 3. Principle pack has not drifted from the canonical corpus
python3 scripts/sync-principle-pack.py --services ../services --check

# 4. Embedded seed is fresh against its YAML sources
SERVICES_REPO=../services scripts/build-awareness-graph.sh --check

# 5. Full test suite
SERVICES_REPO=$(pwd)/../services go test ./... ./cmd/... -count=1 -timeout 120s

# 6. Public/runtime release boundary stays clean
./scripts/check-release-boundary.sh \
  /tmp/awareness-graph-service-root \
  /tmp/awareness-graph-local-root
```

Items 3–6 are enforced in CI and release workflows. Run the three smokes locally
before a send, because they exercise the runtime and managed-governance paths a
tester or operator will hit.

## Honest-claims audit (grep-able)

```bash
# No stale principle count in onboarding docs (CI guards this too)
! grep -rEn "18 (universal|meta|principles)" \
    README.md QUICKSTART.md INSTALL.md docs/quickstart.md docs/meta-principles.md \
    docs/case-study-cold-start.md

# No Windows support claim (only "not validated" / "coming next" allowed)
grep -rin "windows" README.md INSTALL.md QUICKSTART.md docs/first-tester-invite.md
#   → every hit must say NOT validated / coming next, never "supported"

# No "zero dependencies" claim (Oxigraph is a named, fetched dependency)
! grep -rin "zero depend\|no depend\|no external dep" README.md QUICKSTART.md INSTALL.md
```

## Commit-scope audit

```bash
# The commit's stated scope matches what it actually changed. Fails if a
# "docs only" / "no engine change" message touches golang/ cmd/ internal/
# (the 765037e drift class — see docs/commit-integrity-notes.md). Also
# enforced in CI on every push. To acknowledge an intentional engine change
# in an otherwise-docs commit, start a message line with "[engine-ack] <reason>".
scripts/check-commit-scope.sh HEAD
```

- [ ] Commit/message scope matches changed files.
- [ ] No engine files (`golang/`, `cmd/`, `internal/`) in a docs-only /
      adoption-only commit unless a message line starts with `[engine-ack] <reason>`.

## Manual confirmations

- [ ] README "Try it in 15 minutes" commands match the smoke script's path
      (clone → install.sh → init → serve -no-seed → build → briefing).
- [ ] Release artifacts include both:
      `awareness-graph_<X.Y.Z>_linux_amd64.tgz` and
      `awg-local_<X.Y.Z>_linux_amd64.tgz`, each with matching `.sha256`.
- [ ] Managed-governance publication root is bootstrap-able:
      `trusted-publishers.json` is present at the publication root and a fresh
      client can `trust fetch` from that root.
- [ ] The demo (`examples/payment-cold-start`) briefing output in its README
      matches a real run.
- [ ] Invite (`docs/first-tester-invite.md`) links resolve:
      checklist, README quickstart.
- [ ] Limitations stated in the invite: Go required, Linux/macOS first,
      Windows not validated, Oxigraph fetched (not zero-dep).
- [ ] Tester kit complete: invite, checklist, targets, feedback-triage,
      feedback-form all present in `docs/`.

## What this checklist deliberately does NOT gate

Polish that isn't an adoption blocker for the stated first-tester profile:
Windows, a GUI/dashboard, hosted/SaaS. Those are post-first-round decisions,
not send-blockers.

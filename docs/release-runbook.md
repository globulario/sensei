# awareness-graph Release Runbook

This page is the operator contract for cutting a new awareness-graph
release and confirming it is actually live in the cluster. Read it
end-to-end before bumping a version.

It exists because of a recurring gotcha: a v0.0.X bump can deploy
cleanly, the service reports the new version in etcd, the binary is
running — and the new anchors are still invisible because the live
Oxigraph store was not reloaded. See the **"Seed activation gotcha"**
section below.

## Seed activation gotcha — read this first

`golang/server/main.go:seedIfEmpty` loads the embedded `awareness.nt`
into Oxigraph **only when the store is empty**. From its own comment:

> It is idempotent: a store that already has triples is left
> untouched so a restart never overwrites live data.

This is correct, intentional, and load-bearing — a restart must
never clobber accumulated state. The side-effect is that a brand-new
binary will boot, log
`awareness-graph: store has N triples — skipping seed load`,
and serve the **previous** seed indefinitely. The version in etcd
will say `0.0.X+1`. Briefing/Impact/Preflight will silently use
`0.0.X` data.

**Mitigation:** after every awareness-graph release, run the
additive `loadnt` import against the live Oxigraph. The Oxigraph
Graph Store POST semantics merge into the default graph, so re-posting
the new `awareness.nt` adds the new triples without disturbing
anything else. Operators must NOT wipe the live Oxigraph store to
force a fresh seed load — that would destroy any state that
accumulated outside the embedded seed (manual annotations, fixtures,
session-discovered facts under review).

## Release checklist

Run this top-to-bottom for every awareness-graph version bump.
Steps 1–4 happen in the awareness-graph repo. Steps 5–8 happen on
the controller / VIP node.

### 1. Build / regenerate `awareness.nt`

In the awareness-graph repo, against the matching services-repo
checkout:

```bash
SERVICES_REPO=../services scripts/build-awareness-graph.sh
```

This regenerates:

- `golang/server/embeddata/awareness.nt` (the embedded seed)
- `<services-repo>/docs/awareness/generated/*_code_symbols.yaml`
- `<services-repo>/docs/awareness/generated/*_code_edges.yaml`
- `<services-repo>/docs/awareness/generated/*_annotation_report.yaml`

Commit the regenerated artifacts in each repo as separate commits
(see prior commits `71d04e9` in awareness-graph and `69d77d0b` in
services for shape).

### 2. Confirm `build-awareness-graph.sh --check` passes

```bash
SERVICES_REPO=../services scripts/build-awareness-graph.sh --check
echo "exit=$?"   # MUST be 0 before bumping the version
```

`--check` re-runs the build into a temp dir and diffs against the
committed outputs. Exit 1 means a regenerated artifact differs from
the committed copy — fix that before tagging.

### 3. Bump version / tag / release

```bash
# In packaging/specs/awareness_graph_service.yaml, bump metadata.version.
git add packaging/specs/awareness_graph_service.yaml
git commit -m "chore(packaging): bump spec metadata.version to <X.Y.Z>"
git tag v<X.Y.Z>
git push origin master
git push origin v<X.Y.Z>
```

The CI Release workflow runs on tag push and uploads
`awareness-graph_<X.Y.Z>_linux_amd64.tgz` and
`awg-local_<X.Y.Z>_linux_amd64.tgz` to the GitHub Release.
Wait for the workflow to complete green (`gh run watch` or
`gh run list`) before continuing.

Important product-boundary rule:

- these public/runtime artifacts may contain the local runtime only
- they must NOT contain managed-governance packs, trust roots, or activation state

That boundary is enforced by `scripts/check-release-boundary.sh` in the release
workflow. Managed-governance publication is a separate control-plane path, not
part of the public runtime bundle.

### 3b. Publish the managed-governance root separately

The public runtime release is not the governance distribution channel. Publish
governance packs through the managed-governance root so a client can bootstrap
trust from the published root itself:

```bash
./bin/awg governance publish gen-key \
  --out /secure/vendor/signing-key.json \
  --publisher-id core@globular.io \
  --key-id core-2026-q3

./bin/awg governance publish trust-root \
  --signing-key /secure/vendor/signing-key.json \
  --out /secure/vendor/trusted-publishers.json \
  --display-name "Globular Core"

./bin/awg governance publish build \
  --input docs/awareness \
  --out-dir /tmp/governance-pack/core.meta-principles/2026.06.25 \
  --pack-id core.meta-principles \
  --pack-version 2026.06.25 \
  --publisher-id core@globular.io \
  --publisher-name "Globular Core" \
  --issued-at 2026-06-25T12:00:00Z \
  --min-awg-version 0.0.0 \
  --key-id core-2026-q3 \
  --strict

./bin/awg governance publish sign \
  --signing-key /secure/vendor/signing-key.json \
  /tmp/governance-pack/core.meta-principles/2026.06.25

./bin/awg governance publish release \
  --trusted-keys /secure/vendor/trusted-publishers.json \
  --signing-key /secure/vendor/signing-key.json \
  --publication-root /srv/awg-governance \
  --channel stable \
  /tmp/governance-pack/core.meta-principles/2026.06.25
```

Expected publication-root shape after release:

- `/srv/awg-governance/trusted-publishers.json`
- `/srv/awg-governance/governance/index.json`
- `/srv/awg-governance/governance/index.json.sig`
- `/srv/awg-governance/governance/packs/<pack-id>/<pack-version>/...`

That root is what clients should point at for:

```bash
./bin/awg governance trust fetch --source /srv/awg-governance
./bin/awg governance fetch --source /srv/awg-governance --pack-id core.meta-principles --channel stable
```

Run `scripts/smoke-governance-publication.sh` before treating this path as
release-ready.

### 4. Deploy package

The CI release tarball is NOT in Globular publisher-format — it
has `metadata/<name>/package.json` nested, the publisher needs
`package.json` at the archive root. Repackage via `globular pkg
build` (see existing feedback note `ci_release_tgz_not_publish_format`):

```bash
mkdir -p /tmp/aw-pkg<XYZ> && cd /tmp/aw-pkg<XYZ>
gh release download v<X.Y.Z> --repo globulario/awareness-graph \
  --pattern 'awareness-graph_<X.Y.Z>_linux_amd64.tgz' \
  -O awareness-graph_<X.Y.Z>_linux_amd64.tgz
mkdir -p extracted
tar -xzf awareness-graph_<X.Y.Z>_linux_amd64.tgz -C extracted

globular pkg build \
  --spec extracted/specs/awareness_graph_service.yaml \
  --root extracted \
  --version <X.Y.Z> \
  --publisher core@globular.io \
  --platform linux_amd64 \
  --out /tmp/aw-pkg<XYZ>/pkgout \
  --skip-missing-config=true --skip-missing-systemd=true

globular pkg publish \
  --file /tmp/aw-pkg<XYZ>/pkgout/awareness-graph_<X.Y.Z>_linux_amd64.tgz \
  --repository repository.globular.internal \
  --token "$(cat ~/.config/globular/token)"

globular services desired set awareness-graph <X.Y.Z> \
  --token "$(cat ~/.config/globular/token)"
```

### 5. Confirm binary / config version

After convergence (typically under a minute), verify etcd reports
the new version:

```bash
# Via MCP:
mcp__globular__service_config_get awareness-graph
# Look for: "Version": "<X.Y.Z>"
```

This confirms the controller and node-agent finished installing the
new binary. It does NOT confirm the new awareness data is live —
see the next step.

### 6. Run `loadnt` additive import against Oxigraph

On the node hosting awareness-graph (Oxigraph is bound to
`localhost:7878` on the same node):

```bash
cd <awareness-graph repo>
go build -o /tmp/loadnt ./cmd/loadnt
/tmp/loadnt \
  -input golang/server/embeddata/awareness.nt \
  -oxigraph-url http://localhost:7878/store
```

`loadnt` validates the N-Triples content before any HTTP request
reaches Oxigraph (see `loadnt: %d N-Triples validation errors`
error path). The endpoint defaults to `/store?default` which is
the Oxigraph Graph Store HTTP Protocol target for the default
graph. `loadnt` uses `PUT`, so reloading replaces the default graph
deterministically; removed triples do not linger from older loads.

A Make target carries the same invocation:

```bash
make load-release-seed
```

(Same command, written once.)

Then verify the committed seed, transaction stamp, and live store all align:

```bash
./bin/awg seed-status \
  --seed golang/server/embeddata/awareness.nt \
  --oxigraph-url http://localhost:7878/query \
  --require-current
```

**Do NOT** wipe Oxigraph (`rm -rf` on the data directory, or POST
with an empty body to `?default`) just to force a fresh seed load.
That destroys any state accumulated outside the embedded seed and
violates the
`awareness.oxigraph_is_external_runtime_state` intent.

### 7. Confirm triple count increased or expected IDs are queryable

A new release should add triples to the store. Quick checks:

```bash
# Direct via Oxigraph SPARQL endpoint (cheap):
curl -s -G http://localhost:7878/query \
  --data-urlencode 'query=SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }' \
  -H 'Accept: text/csv'

# Or via the MCP query tool (proves the bridge sees the data too):
mcp__globular__awareness_query mode=by_id \
  id=invariant:<one_of_the_new_invariant_ids>
```

Triple count after a normal version bump should be **higher** than
before. A by_id lookup for any newly-added invariant should return
1 row. If the count is unchanged AND a new-invariant lookup returns
0 rows, step 6 did not actually run — re-run it and check the
`loadnt: loaded <N> bytes` output.

### 8. Probe Briefing / Preflight on newly anchored files

Final smoke. Pick 1–3 files whose annotations or anchors are new in
this release and verify they no longer return EMPTY:

```bash
mcp__globular__awareness_briefing file=<repo-relative path of newly anchored file>
# Expected: status: ok (NOT empty)
# Expected: referenced_ids includes the new invariant/failure_mode/intent id

mcp__globular__awareness_preflight files='["<same path>"]'
# Expected: status: ok, NOT degraded
# Expected: coverage.sufficient: true (with the right direct_anchor_count)
```

If a file still returns `status: empty` after step 6 succeeded,
the file's annotation block may have a typo (the annotation
scanner ran but found nothing parseable). Re-check the scanner
output in `docs/awareness/generated/<group>_annotation_report.yaml`
in the services repo — it logs which annotations were and were
not recognized.

## Verification probes — quick reference

| Probe | Tool | Expected after a clean release |
|---|---|---|
| Etcd version | `mcp__globular__service_config_get awareness-graph` | `Version: <X.Y.Z>` |
| Triple count | `curl … COUNT(*) … /query` | Higher than before this release |
| New invariant by ID | `awareness.query by_id id=invariant:<new>` | 1 row |
| Newly anchored file | `awareness.briefing file=<path>` | `status: ok`, referenced_ids non-empty |
| Preflight clears DEGRADED | `awareness.preflight files=[<path>]` | `status: ok`, not `degraded` |
| --check on a clean checkout | `scripts/build-awareness-graph.sh --check` | Exit `0` |

## Known limitation

The seed activation gotcha exists because Oxigraph is treated as
external runtime state (intent
`awareness.oxigraph_is_external_runtime_state`) — the binary cannot
assume the store should reflect the embedded seed and cannot
clobber accumulated data. A long-term improvement would be a
versioned-seed mechanism (e.g. include the seed sha256 in a
sentinel triple; on startup, compare to the embedded seed sha256
and additively load the diff if they differ). Until that ships,
step 6 is the operator contract.

## Related

- `docs/globular-packaging.md` — package build, runtime config, ports
- `docs/awareness/invariants.yaml` — the rules the seed encodes
- `cmd/loadnt/main.go` — the validating N-Triples uploader
- `golang/server/main.go:seedIfEmpty` — the load-once-when-empty logic

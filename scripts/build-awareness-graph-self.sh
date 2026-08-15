#!/usr/bin/env bash
# build-awareness-graph-self.sh — standalone (services-free) awareness seed build.
#
# Rebuilds golang/server/embeddata/awareness.nt + awareness.transaction.tsv from
# THIS repo's corpus alone. Unlike scripts/build-awareness-graph.sh (the combined
# awareness-graph + services build), this needs no sibling services repo — it is
# the seed-build path for the open-source / standalone awareness-graph.
#
#   Normal   : regenerate this repo's code symbols, rebuild the seed + stamp.
#   --check  : rebuild into a temp dir; fail (exit 1) if the committed seed drifted.
#   --bootstrap-unadmitted : ONE-TIME initialization bridge. See below.
#
# The bootstrap bridge exists because the promotion and contradiction validators
# require an authoritative admission scope, and that scope comes from a signed
# baseline bound to a GRAPH DIGEST — which only a completed seed build produces:
#
#   validator -> seed producer -> graph digest -> admission activation -> validator
#
# Each link is individually correct and fail-closed. Together they deadlock an
# uninitialized repository. --bootstrap-unadmitted breaks that loop exactly once,
# by omitting ONLY the two admission-dependent validators. -strict, -validate-refs
# and the dangling-ref baseline all still apply, and the mode REFUSES to run once
# a signed admission baseline exists, so it cannot become a forgotten bypass.
#
# The server embeds BOTH the seed and the transaction stamp and cross-checks that
# the stamp certifies the seed (golang/server/graph_authority.go). This script
# writes a matching stamp so a standalone server reports an authoritative graph.
#
# Exit codes: 0 success; 1 --check found drift; 2 configuration/tool error.
set -euo pipefail

AG="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$AG"

REGISTRY="$AG/docs/awareness/namespaces.yaml"
BASELINE="$AG/docs/awareness/dangling_refs_baseline.tsv"
AG_GENERATED="$AG/docs/awareness/generated"
SEED="$AG/golang/server/embeddata/awareness.nt"
STAMP="$AG/golang/server/embeddata/awareness.transaction.tsv"

CHECK_MODE=false
BOOTSTRAP_UNADMITTED=false
case "${1:-}" in
    --check)                CHECK_MODE=true ;;
    --bootstrap-unadmitted) BOOTSTRAP_UNADMITTED=true ;;
    "")                     ;;
    *) echo "usage: $0 [--check | --bootstrap-unadmitted]" >&2; exit 2 ;;
esac

ADMISSION_BASELINE="$AG/governance/knowledge-admission-baseline.yaml"
ADMISSION_SIGNATURE="$AG/governance/knowledge-admission-baseline.sig"

if [[ "$BOOTSTRAP_UNADMITTED" == true ]]; then
    # Self-destruct guard: the bridge is only for an UNINITIALIZED repository.
    # Once a signature exists the normal path must be used forever, so today's
    # escape hatch cannot become next year's silent bypass.
    if [[ -f "$ADMISSION_SIGNATURE" && -f "$ADMISSION_BASELINE" ]]; then
        echo "error: --bootstrap-unadmitted refused: a signed knowledge-admission baseline already exists." >&2
        echo "       $ADMISSION_SIGNATURE" >&2
        echo "       Admission is active; run this script with no flags so the promotion and" >&2
        echo "       contradiction validators are enforced." >&2
        exit 2
    fi
    cat >&2 <<'BANNER'

================================================================================
BOOTSTRAP MODE: knowledge admission is not yet active.
Promotion and contradiction verdicts were NOT produced.
This seed exists only to derive the graph digest required to activate admission.
It is NOT a certified build. Do not treat its success as validation.
================================================================================

BANNER
fi

command -v go >/dev/null 2>&1 || { echo "error: go toolchain required" >&2; exit 2; }
[[ -f "$REGISTRY" ]] || { echo "error: missing namespace registry $REGISTRY" >&2; exit 2; }

SCANNER=/tmp/_aw_self_scanner
YAML2NT=/tmp/_aw_self_yaml2nt
echo "Building tools from source..."
go build -o "$SCANNER" ./cmd/annotation-scanner
go build -o "$YAML2NT" ./cmd/yaml2nt
echo "  annotation-scanner: OK"
echo "  yaml2nt:            OK"

# Regenerate this repo's code symbols/edges from its own @awareness annotations.
run_scan() {
    local out_dir="$1"
    mkdir -p "$out_dir"
    "$SCANNER" --registry "$REGISTRY" --source "$AG" --repo-root "$AG" \
        --output "$out_dir" --prefix awareness_graph --strict
}

# Build the seed from this repo's corpus alone (docs/awareness includes the
# generated/ subdir). Strict + ref/promotion/contradiction validation stays on;
# the committed baseline allowlists the known aspirational forbidden-fix / test
# citations so only NEW dangling references fail the build.
run_yaml2nt() {
    local out_path="$1"
    local -a cmd=(
        "$YAML2NT"
        -input "$AG/docs/awareness"
        -path-prefix "$AG"
        -strict
        -validate-refs
        -output "$out_path"
    )
    # The two admission-dependent validators. Omitted ONLY by the one-time
    # bootstrap bridge; every normal build requires them, and "requested but
    # unavailable" stays a failure rather than becoming a warning. A caller that
    # asked for a verdict must not receive exit 0 when no verdict was produced.
    if [[ "$BOOTSTRAP_UNADMITTED" != true ]]; then
        cmd+=(-validate-promotion -validate-contradictions)
    fi
    [[ -f "$BASELINE" ]] && cmd+=(-allowed-dangling-refs "$BASELINE")
    "${cmd[@]}"
}

# Extract the seed's self-describing marker so the stamp certifies THIS seed.
seed_marker_field() {
    local seed="$1" predicate="$2"
    grep -oE "$predicate> \"[^\"]+\"" "$seed" | grep -oE '"[^"]+"' | tr -d '"' | head -1
}

write_stamp() {
    local seed="$1" out="$2"
    local digest count head tool
    digest="$(seed_marker_field "$seed" 'seedDigestSha256')"
    count="$(seed_marker_field "$seed" 'seedTripleCount')"
    head="$(git -C "$AG" rev-parse HEAD 2>/dev/null || echo unknown)"
    tool="$(sha256sum "$YAML2NT" | awk '{print $1}')"
    [[ -n "$digest" ]] || { echo "error: seed carries no digest marker" >&2; exit 2; }
    {
        printf 'format\tv1\n'
        printf 'seed\tdigest_sha256\t%s\n' "$digest"
        printf 'seed\ttriple_count\t%s\n' "$count"
        printf 'repo\tawareness-graph\t%s\n' "$head"
        printf 'repo\tservices\tstandalone\n'
        printf 'tool\tyaml2nt\t%s\n' "$tool"
    } >"$out"
}

if ! $CHECK_MODE; then
    echo ""
    echo "Regenerating code symbols..."
    run_scan "$AG_GENERATED"
    echo ""
    echo "Building awareness.nt (self-only)..."
    run_yaml2nt "$SEED"
    write_stamp "$SEED" "$STAMP"
    TRIPLES=$(grep -c . "$SEED")
    echo ""
    echo "Done (standalone)."
    echo "  seed:    $SEED ($TRIPLES lines)"
    echo "  stamp:   $STAMP"
    exit 0
fi

# ── Check mode ────────────────────────────────────────────────────────────────
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
echo ""
echo "Rebuilding into temp for freshness check..."
run_scan "$TMP/generated"
# Mirror the corpus with fresh generated/ so yaml2nt sees the same layout.
run_yaml2nt "$TMP/awareness.nt"

if go run ./cmd/awg seed-freshness -committed "$SEED" -generated "$TMP/awareness.nt" -ag-repo "$AG"; then
    echo "awareness.nt: fresh (standalone)."
    exit 0
fi
echo "STALE: run scripts/build-awareness-graph-self.sh and commit the seed." >&2
exit 1

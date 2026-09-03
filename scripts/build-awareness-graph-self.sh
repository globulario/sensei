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
[[ "${1:-}" == "--check" ]] && CHECK_MODE=true
[[ "${1:-}" =~ ^(|--check)$ ]] || { echo "usage: $0 [--check]" >&2; exit 2; }

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
        -validate-promotion
        -validate-contradictions
        -output "$out_path"
    )
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

# ── Committed generated artifacts ─────────────────────────────────────────────
#
# CHECK WHAT IS COMMITTED, not only what this run just produced.
#
# Both sides of the seed comparison below are built from the FRESH scan above, so
# the committed docs/awareness/generated/* never entered any comparison. It could
# drift arbitrarily while this gate printed "fresh" and exited 0. Reproduced at
# 58c055fb twice: an annotation report reading `discovered_tests: 999999`, and
# code_symbols.yaml with a real symbol deleted, both passed.
#
# That is not cosmetic. `sensei build` reads `-input docs/awareness` recursively,
# so the COMMITTED generated files are published into the served graph -- a
# drifted one publishes wrong knowledge, and the gate that exists to prevent it
# could not see it.
#
# The comparison set and its exclusion are the repository's own, already decided
# in scripts/build-awareness-graph.sh: _code_symbols.yaml and _code_edges.yaml
# are load-bearing; the annotation report is "informational diagnostics" and is
# deliberately NOT compared. This script simply never consumed that decision.
GENERATED_STALE=false
check_generated() {
    local name="$1"
    local committed="$AG_GENERATED/$name" fresh="$TMP/generated/$name"
    if [[ ! -f "$fresh" ]]; then
        # A file the scanner did not produce cannot be judged. Say so rather
        # than passing silently: an absent comparison is not a clean one.
        echo "  UNCHECKED: $name (scanner produced no fresh copy)" >&2
        GENERATED_STALE=true
        return
    fi
    if [[ ! -f "$committed" ]]; then
        echo "  MISSING:   $name is not committed" >&2
        GENERATED_STALE=true
        return
    fi
    if diff -q "$fresh" "$committed" >/dev/null 2>&1; then
        echo "  ok:        $name"
    else
        echo "  STALE:     $name" >&2
        diff --unified=3 "$committed" "$fresh" >&2 || true
        GENERATED_STALE=true
    fi
}

echo ""
echo "Checking committed generated files..."
check_generated "awareness_graph_code_symbols.yaml"
check_generated "awareness_graph_code_edges.yaml"

if $GENERATED_STALE; then
    echo "STALE: committed docs/awareness/generated/ does not match a fresh scan." >&2
    echo "       Run scripts/build-awareness-graph-self.sh and commit the result." >&2
    exit 1
fi

if go run ./cmd/awg seed-freshness -committed "$SEED" -generated "$TMP/awareness.nt" -ag-repo "$AG"; then
    echo "awareness.nt: fresh (standalone)."
    exit 0
fi
echo "STALE: run scripts/build-awareness-graph-self.sh and commit the seed." >&2
exit 1

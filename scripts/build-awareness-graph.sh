#!/usr/bin/env bash
# build-awareness-graph.sh — deterministic awareness graph build pipeline.
#
# Normal mode  : regenerates generated YAML and rebuilds awareness.nt in place.
# Check mode   : regenerates into temp, diffs against committed files, exits 1 if stale.
# Warn-stale   : like --check, but STALENESS is advisory (exit 0 + ::warning::).
#                Corpus-CORRECTNESS still hard-fails — yaml2nt's -strict /
#                -validate-refs / -validate-promotion / -validate-contradictions
#                and the --strict scanner exit non-zero EARLY (under set -e),
#                before the staleness comparison is ever reached. This is the
#                PR-side mode for GC-2: master's seed-rebuild workflow
#                auto-commits the refreshed seed post-merge, so a PR must not be
#                blocked merely because the committed seed lags — only because
#                the corpus is actually broken.
#
# Usage:
#   scripts/build-awareness-graph.sh [--check | --warn-stale]
#
# Environment:
#   SERVICES_REPO  path to globulario/services checkout.
#                  Default: ../services relative to awareness-graph root.
#
# Exit codes:
#   0  success (normal: regenerated; check: all fresh; warn-stale: corpus valid)
#   1  check mode: one or more files are stale (run without --check to fix)
#   2  configuration error (missing services repo, tool build failure, etc.)
set -euo pipefail

AG="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$AG/scripts/build-awareness-graph-lib.sh"

# ── Locate services repo ──────────────────────────────────────────────────────
SVC="${SERVICES_REPO:-}"
if [[ -z "$SVC" ]]; then
    candidate="$(cd "$AG/../services" 2>/dev/null && pwd)" || true
    if [[ -n "$candidate" && -f "$candidate/docs/awareness/namespaces.yaml" ]]; then
        SVC="$candidate"
    fi
fi
if [[ -z "$SVC" || ! -f "$SVC/docs/awareness/namespaces.yaml" ]]; then
    echo "error: services repo not found." >&2
    echo "  Set SERVICES_REPO=/path/to/globulario/services" >&2
    echo "  or clone it as a sibling of the awareness-graph repo." >&2
    exit 2
fi

GLOB="${GLOBULAR_REPO:-}"
if [[ -z "$GLOB" ]]; then
    candidate="$(cd "$AG/../Globular" 2>/dev/null && pwd)" || true
    if [[ -n "$candidate" && -f "$candidate/internal/globule/banner_test.go" ]]; then
        GLOB="$candidate"
    fi
fi
GLOB_PARENT=""
if [[ -n "$GLOB" ]]; then
    GLOB_PARENT="$(cd "$GLOB/.." && pwd)"
fi

# ── Parse flags ───────────────────────────────────────────────────────────────
CHECK_MODE=false
FAIL_ON_STALE=true
for arg in "$@"; do
    case "$arg" in
        --check) CHECK_MODE=true ;;
        --warn-stale) CHECK_MODE=true; FAIL_ON_STALE=false ;;
        *) echo "error: unknown flag $arg" >&2; exit 2 ;;
    esac
done

# ── Build tools ───────────────────────────────────────────────────────────────
echo "Building tools from source..."
cd "$AG"
go build -o /tmp/_aw_annotation_scanner ./cmd/annotation-scanner
go build -o /tmp/_aw_yaml2nt ./cmd/yaml2nt
echo "  annotation-scanner: OK"
echo "  yaml2nt:            OK"

REGISTRY="$SVC/docs/awareness/namespaces.yaml"
AG_GENERATED="$AG/docs/awareness/generated"
SVC_GENERATED="$SVC/docs/awareness/generated"
SCAN_TARGETS_FILE="$AG/scripts/awareness-scan-targets.tsv"
SEED="$AG/golang/server/embeddata/awareness.nt"
TRANSACTION_STAMP="$AG/golang/server/embeddata/awareness.transaction.tsv"
SCAN_CACHE_DIR="$AG/.cache/build-awareness-graph"
SEED_CACHE_META="$SCAN_CACHE_DIR/seed.meta"
BENCH_CONTRACTS_DIR="$AG/eval/multi-swe-bench/contracts"
LEARNING_EVENTS_DIR="$AG/eval/multi-swe-bench/notes/learning_events"
SCANNER_SHA="$(sha256sum /tmp/_aw_annotation_scanner | awk '{print $1}')"
YAML2NT_SHA="$(sha256sum /tmp/_aw_yaml2nt | awk '{print $1}')"
BUILD_SCRIPT_SHA="$(sha256sum "$AG/scripts/build-awareness-graph.sh" | awk '{print $1}')"
REGISTRY_SHA="$(sha256sum "$REGISTRY" | awk '{print $1}')"
SCAN_TARGETS_SHA="$(sha256sum "$SCAN_TARGETS_FILE" | awk '{print $1}')"

# ── Annotation scanner invocations ───────────────────────────────────────────
# One invocation per annotated project registered in namespaces.yaml.
run_scanner() {
    local source="$1" repo_root="$2" prefix="$3" output_dir="$4"
    /tmp/_aw_annotation_scanner \
        --registry "$REGISTRY" \
        --source   "$source" \
        --repo-root "$repo_root" \
        --output   "$output_dir" \
        --prefix   "$prefix" \
        --strict
}

resolve_scan_template() {
    local value="$1"
    value="${value//@AG@/$AG}"
    value="${value//@SVC@/$SVC}"
    value="${value//@GLOB@/$GLOB}"
    value="${value//@GLOB_PARENT@/$GLOB_PARENT}"
    value="${value//@AG_GENERATED@/$AG_GENERATED}"
    value="${value//@SVC_GENERATED@/$SVC_GENERATED}"
    printf '%s\n' "$value"
}

scan_targets() {
    local source repo_root prefix output_dir cache_root label probe
    while IFS='|' read -r source repo_root prefix output_dir cache_root label probe; do
        [[ -n "$source" ]] || continue
        [[ "${source:0:1}" == "#" ]] && continue
        source="$(resolve_scan_template "$source")"
        repo_root="$(resolve_scan_template "$repo_root")"
        output_dir="$(resolve_scan_template "$output_dir")"
        cache_root="$(resolve_scan_template "$cache_root")"
        label="$(resolve_scan_template "$label")"
        probe="$(resolve_scan_template "${probe:-}")"
        if [[ -n "$probe" && ! -f "$probe" && ! -d "$probe" ]]; then
            continue
        fi
        printf '%s|%s|%s|%s|%s|%s\n' "$source" "$repo_root" "$prefix" "$output_dir" "$cache_root" "$label"
    done <"$SCAN_TARGETS_FILE"
}

scanner_outputs_exist() {
    local output_dir="$1" prefix="$2"
    [[ -f "$output_dir/${prefix}_code_symbols.yaml" && -f "$output_dir/${prefix}_code_edges.yaml" ]]
}

repo_head() {
    git -C "$1" rev-parse HEAD 2>/dev/null || true
}

repo_has_relevant_changes() {
    local repo_root="$1" old_head="$2" new_head="$3" source_rel="$4"
    if repo_has_relevant_worktree_changes "$repo_root" "$source_rel"; then
        return 0
    fi
    if [[ -z "$old_head" || -z "$new_head" ]]; then
        return 1
    fi
    if [[ -z "$source_rel" ]]; then
        [[ -n "$(git -C "$repo_root" diff --name-only "$old_head" "$new_head" 2>/dev/null || true)" ]]
        return
    fi
    [[ -n "$(git -C "$repo_root" diff --name-only "$old_head" "$new_head" -- "$source_rel" 2>/dev/null || true)" ]]
}

repo_has_relevant_worktree_changes() {
    local repo_root="$1" source_rel="$2"
    if [[ -z "$source_rel" ]]; then
        [[ -n "$(git -C "$repo_root" status --porcelain 2>/dev/null || true)" ]]
        return
    fi
    [[ -n "$(git -C "$repo_root" status --porcelain -- "$source_rel" 2>/dev/null || true)" ]]
}

run_scanner_cached() {
    local source="$1" repo_root="$2" prefix="$3" output_dir="$4" cache_root="${5:-$2}"
    local meta="$SCAN_CACHE_DIR/$prefix.meta"
    local current_head cached_head cached_scanner_sha cached_registry_sha cached_scan_targets_sha cached_source_rel source_rel

    mkdir -p "$output_dir" "$SCAN_CACHE_DIR"
    current_head="$(repo_head "$cache_root")"
    source_rel="${source#$cache_root/}"
    [[ "$source_rel" = "$source" ]] && source_rel=""

    if scanner_outputs_exist "$output_dir" "$prefix" && [[ -f "$meta" ]]; then
        # shellcheck disable=SC1090
        source "$meta"
        cached_head="${CACHED_REPO_HEAD:-}"
        cached_scanner_sha="${CACHED_SCANNER_SHA:-}"
        cached_registry_sha="${CACHED_REGISTRY_SHA:-}"
        cached_scan_targets_sha="${CACHED_SCAN_TARGETS_SHA:-}"
        cached_source_rel="${CACHED_SOURCE_REL:-}"
        if [[ "$cached_scanner_sha" = "$SCANNER_SHA" && "$cached_registry_sha" = "$REGISTRY_SHA" && "$cached_scan_targets_sha" = "$SCAN_TARGETS_SHA" && "$cached_source_rel" = "$source_rel" ]]; then
            if [[ "$cached_head" = "$current_head" ]]; then
                return 0
            fi
            if ! repo_has_relevant_changes "$cache_root" "$cached_head" "$current_head" "$source_rel"; then
                return 0
            fi
        fi
    fi

    run_scanner "$source" "$repo_root" "$prefix" "$output_dir"
    cat >"$meta" <<EOF
CACHED_REPO_HEAD='$current_head'
CACHED_SCANNER_SHA='$SCANNER_SHA'
CACHED_REGISTRY_SHA='$REGISTRY_SHA'
CACHED_SCAN_TARGETS_SHA='$SCAN_TARGETS_SHA'
CACHED_SOURCE_REL='$source_rel'
EOF
}

run_scan_plan() {
    local mode="$1" override_output_dir="${2:-}"
    while IFS='|' read -r source repo_root prefix output_dir cache_root label; do
        [[ -n "$source" ]] || continue
        [[ -n "$cache_root" ]] || cache_root="$repo_root"
        if [[ -n "$override_output_dir" ]]; then
            output_dir="$override_output_dir"
        fi
        if [[ "$mode" = cached ]]; then
            if run_scanner_cached "$source" "$repo_root" "$prefix" "$output_dir" "$cache_root"; then
                echo "  $(printf '%-25s' "${label}:") cached/ok"
            fi
        else
            run_scanner "$source" "$repo_root" "$prefix" "$output_dir"
            echo "  $(printf '%-25s' "${label}:") OK"
        fi
    done < <(scan_targets)
}

# ── yaml2nt invocation ───────────────────────────────────────────────────────
run_yaml2nt() {
    local output_path="$1" svc_generated_input="$2" staged_prefix="${3:-}"
    # Per-repo domain tagging (opt-in): when AG_REPO_KEY / SVC_REPO_KEY are set,
    # each repo's awareness nodes are tagged to that repo (aw:repo) so the graph
    # is filterable per repo. Unset → the default single home-domain behaviour
    # (keeps the public self-build unchanged).
    local -a ag_inputs svc_inputs
    if [[ -n "${AG_REPO_KEY:-}" ]]; then
        ag_inputs=(-input-repo "$AG/docs/awareness=$AG_REPO_KEY" -input-repo "$AG_GENERATED=$AG_REPO_KEY")
    else
        ag_inputs=(-input "$AG/docs/awareness" -input "$AG_GENERATED")
    fi
    if [[ -n "${SVC_REPO_KEY:-}" ]]; then
        svc_inputs=(-input-repo "$SVC/docs/awareness=$SVC_REPO_KEY" -input-repo "$svc_generated_input=$SVC_REPO_KEY")
    else
        svc_inputs=(-input "$SVC/docs/awareness" -input "$svc_generated_input")
    fi
    # Intents come from the services repo; tag them to SVC_REPO_KEY when set so
    # they are filterable per repo (else home-domain, as before).
    local -a intent_args=(-intent "$SVC/docs/intent")
    if [[ -n "${SVC_REPO_KEY:-}" ]]; then
        intent_args+=(-intent-repo "$SVC_REPO_KEY")
    fi
    local -a cmd=(
        /tmp/_aw_yaml2nt
        "${ag_inputs[@]}"
        "${svc_inputs[@]}"
        "${intent_args[@]}"
        -path-prefix "$AG"
        -path-prefix "$SVC"
        -strict
        -validate-refs
        -allowed-dangling-refs "$SVC/docs/awareness/dangling_refs_baseline.tsv"
        -validate-promotion
        -validate-contradictions
        -output "$output_path"
    )
    if [[ -d "$BENCH_CONTRACTS_DIR" ]]; then
        cmd+=(-input "$BENCH_CONTRACTS_DIR")
    fi
    if [[ -d "$LEARNING_EVENTS_DIR" ]]; then
        cmd+=(-input "$LEARNING_EVENTS_DIR")
    fi
    if [[ -n "$GLOB_PARENT" ]]; then
        cmd+=(-path-prefix "$GLOB_PARENT")
    fi
    if [[ -n "$staged_prefix" ]]; then
        cmd+=(-path-prefix "$staged_prefix")
    fi
    "${cmd[@]}"
}

# Stage the services-generated YAML under the repo-relative subpath
# (docs/awareness/generated) MIRRORED inside the temp root passed as $1, and echo
# the dir to feed yaml2nt. Combined with run_yaml2nt's staged_prefix strip, this
# yields a deterministic repo-relative authoredIn instead of a per-run /tmp path.
prepare_services_generated_input() {
    local output_root="$1"
    local output_dir="$output_root/docs/awareness/generated"
    mkdir -p "$output_dir"
    shopt -s nullglob
    for path in "$SVC_GENERATED"/*.yaml; do
        local base
        base="$(basename "$path")"
        if [[ "$base" == awareness_graph_* ]]; then
            continue
        fi
        ln -sf "$path" "$output_dir/$base"
    done
    shopt -u nullglob
    printf '%s\n' "$output_dir"
}

append_tree_manifest() {
    local label="$1" dir="$2"
    local path rel sha
    if [[ ! -d "$dir" ]]; then
        printf 'tree\t%s\tmissing\n' "$label"
        return
    fi
    while IFS= read -r path; do
        rel="${path#$dir/}"
        sha="$(sha256sum "$path" | awk '{print $1}')"
        printf 'tree\t%s\t%s\t%s\n' "$label" "$rel" "$sha"
    done < <(find -L "$dir" -type f \( -name '*.yaml' -o -name '*.yml' \) | LC_ALL=C sort)
}

append_file_manifest() {
    local label="$1" path="$2"
    if [[ ! -f "$path" ]]; then
        printf 'file\t%s\tmissing\n' "$label"
        return
    fi
    printf 'file\t%s\t%s\n' "$label" "$(sha256sum "$path" | awk '{print $1}')"
}

extract_seed_marker_field() {
    local path="$1" predicate="$2"
    awk -v pred="$predicate" '
        index($0, pred) > 0 {
            line = $0
            start = index(line, "\"")
            if (start == 0) {
                next
            }
            line = substr(line, start + 1)
            stop = index(line, "\"")
            if (stop == 0) {
                next
            }
            line = substr(line, 1, stop - 1)
            print line
            exit
        }
    ' "$path"
}

write_transaction_stamp() {
    local output_path="$1" seed_path="$2" svc_generated_input="$3"
    local seed_digest seed_triple_count ag_head svc_head
    seed_digest="$(extract_seed_marker_field "$seed_path" "seedDigestSha256")"
    seed_triple_count="$(extract_seed_marker_field "$seed_path" "seedTripleCount")"
    ag_head="$(repo_head "$AG")"
    svc_head="$(repo_head "$SVC")"
    {
        printf 'format\tv1\n'
        printf 'seed\tdigest_sha256\t%s\n' "$seed_digest"
        printf 'seed\ttriple_count\t%s\n' "$seed_triple_count"
        if [[ -n "$ag_head" ]]; then
            printf 'repo\tawareness-graph\t%s\n' "$ag_head"
        else
            printf 'repo\tawareness-graph\tmissing\n'
        fi
        if [[ -n "$svc_head" ]]; then
            printf 'repo\tservices\t%s\n' "$svc_head"
        else
            printf 'repo\tservices\tmissing\n'
        fi
        printf 'tool\tyaml2nt\t%s\n' "$YAML2NT_SHA"
        printf 'file\tbuild_script\t%s\n' "$BUILD_SCRIPT_SHA"
        append_file_manifest "namespace_registry" "$REGISTRY"
        append_file_manifest "allowed_dangling_refs" "$SVC/docs/awareness/dangling_refs_baseline.tsv"
        append_tree_manifest "ag_awareness" "$AG/docs/awareness"
        append_tree_manifest "ag_generated" "$AG_GENERATED"
        append_tree_manifest "svc_awareness" "$SVC/docs/awareness"
        append_tree_manifest "svc_generated_filtered" "$svc_generated_input"
        append_tree_manifest "svc_intent" "$SVC/docs/intent"
        append_tree_manifest "bench_contracts" "$BENCH_CONTRACTS_DIR"
        append_tree_manifest "learning_events" "$LEARNING_EVENTS_DIR"
    } >"$output_path"
}

write_seed_manifest() {
    local manifest_path="$1" svc_generated_input="$2"
    {
        printf 'tool\tyaml2nt\t%s\n' "$YAML2NT_SHA"
        printf 'tool\tbuild_script\t%s\n' "$BUILD_SCRIPT_SHA"
        printf 'registry\tnamespaces\t%s\n' "$REGISTRY_SHA"
        printf 'registry\tscan_targets\t%s\n' "$SCAN_TARGETS_SHA"
        append_tree_manifest "ag_awareness" "$AG/docs/awareness"
        append_tree_manifest "ag_generated" "$AG_GENERATED"
        append_tree_manifest "svc_awareness" "$SVC/docs/awareness"
        append_tree_manifest "svc_generated_filtered" "$svc_generated_input"
        append_tree_manifest "svc_intent" "$SVC/docs/intent"
        append_tree_manifest "bench_contracts" "$BENCH_CONTRACTS_DIR"
        append_tree_manifest "learning_events" "$LEARNING_EVENTS_DIR"
        append_file_manifest "allowed_dangling_refs" "$SVC/docs/awareness/dangling_refs_baseline.tsv"
    } >"$manifest_path"
}

seed_manifest_matches() {
    local candidate="$1"
    [[ -f "$SEED" && -f "$SEED_CACHE_META" ]] || return 1
    cmp -s "$candidate" "$SEED_CACHE_META"
}

# ── Normal mode ───────────────────────────────────────────────────────────────
if ! $CHECK_MODE; then
    echo ""
    echo "Running annotation scanner..."
    mkdir -p "$AG_GENERATED"
    run_scan_plan cached

    echo ""
    echo "Building awareness.nt..."
    TMP_SVC_GENERATED_ROOT=$(mktemp -d)
    TMP_SVC_GENERATED=$(prepare_services_generated_input "$TMP_SVC_GENERATED_ROOT")
    TMP_SEED_META=$(mktemp)
    trap 'rm -rf "$TMP_SVC_GENERATED_ROOT"; rm -f "$TMP_SEED_META"' EXIT
    mkdir -p "$SCAN_CACHE_DIR"
    write_seed_manifest "$TMP_SEED_META" "$TMP_SVC_GENERATED"
    if seed_manifest_matches "$TMP_SEED_META"; then
        echo "  awareness.nt: up to date (input manifest unchanged)"
    else
        run_yaml2nt "$SEED" "$TMP_SVC_GENERATED" "$TMP_SVC_GENERATED_ROOT"
        mv "$TMP_SEED_META" "$SEED_CACHE_META"
        TMP_SEED_META=""
    fi
    TMP_TRANSACTION=$(mktemp)
    trap 'rm -rf "$TMP_SVC_GENERATED_ROOT"; rm -f "$TMP_SEED_META" "$TMP_TRANSACTION"' EXIT
    write_transaction_stamp "$TMP_TRANSACTION" "$SEED" "$TMP_SVC_GENERATED"
    if ! cmp -s "$TMP_TRANSACTION" "$TRANSACTION_STAMP" 2>/dev/null; then
        mv "$TMP_TRANSACTION" "$TRANSACTION_STAMP"
        TMP_TRANSACTION=""
        echo "  transaction stamp: yes ($TRANSACTION_STAMP)"
    else
        echo "  transaction stamp: no (already current)"
    fi

    TRIPLES=$(wc -l < "$SEED" | tr -d ' ')
    SIZE=$(wc -c < "$SEED" | tr -d ' ')
    echo ""
    echo "Done."
    echo "  seed:    $SEED"
    echo "  triples: $TRIPLES"
    echo "  bytes:   $SIZE"
    exit 0
fi

# ── Check mode ────────────────────────────────────────────────────────────────
TMPDIR_WORK=$(mktemp -d)
trap 'rm -rf "$TMPDIR_WORK"' EXIT

echo ""
echo "Regenerating into temp dir..."
mkdir -p "$AG_GENERATED"
run_scan_plan fresh "$TMPDIR_WORK"

echo ""
echo "Building awareness.nt into temp..."
TMP_SVC_GENERATED_ROOT=$(mktemp -d)
TMP_SVC_GENERATED=$(prepare_services_generated_input "$TMP_SVC_GENERATED_ROOT")
trap 'rm -rf "$TMPDIR_WORK" "$TMP_SVC_GENERATED_ROOT"' EXIT
run_yaml2nt "$TMPDIR_WORK/awareness.nt" "$TMP_SVC_GENERATED" "$TMP_SVC_GENERATED_ROOT"
write_transaction_stamp "$TMPDIR_WORK/awareness.transaction.tsv" "$TMPDIR_WORK/awareness.nt" "$TMP_SVC_GENERATED"

# Compare generated YAML files (annotation report files are excluded — they
# are informational diagnostics, not load-bearing generated artifacts).
STALE=false

check_file() {
    local name="$1" committed="$2" fresh="$3" owner="$4"
    if ! diff -q "$fresh" "$committed" >/dev/null 2>&1; then
        if [[ "$owner" == "awareness-graph" ]]; then
            echo "  STALE: $name" >&2
            diff --unified=3 "$committed" "$fresh" >&2 || true
            STALE=true
            return
        fi
        echo "  external/context drift tolerated ($owner): $name" >&2
        diff --unified=3 "$committed" "$fresh" >&2 || true
    else
        echo "  ok:    $name"
    fi
}

echo ""
echo "Checking generated files..."
while IFS='|' read -r _ _ prefix output_dir _ _; do
    [[ -n "$prefix" ]] || continue
    owner="$(generated_output_owner "$output_dir" "$AG_GENERATED" "$SVC_GENERATED")"
    check_file "${prefix}_code_symbols.yaml" \
        "$output_dir/${prefix}_code_symbols.yaml" \
        "$TMPDIR_WORK/${prefix}_code_symbols.yaml" \
        "$owner"
    check_file "${prefix}_code_edges.yaml" \
        "$output_dir/${prefix}_code_edges.yaml" \
        "$TMPDIR_WORK/${prefix}_code_edges.yaml" \
        "$owner"
done < <(scan_targets)
# The seed is a single artifact generated from BOTH this repo's corpus and the
# services awareness YAML. A whole-file diff deadlocks paired cross-repo changes
# (a seed PR carrying services-PR triples that services master lacks would always
# look "stale"). Use the ownership-aware comparator instead: it fails only when
# THIS repo's owned triples drift; services-authored triples that lead/lag
# services master are tolerated cross-repo context. Owned drift, dangling refs,
# and N-Triples validity are still enforced (here and by `sensei audit`).
echo "Checking awareness.nt (ownership-aware seed freshness)..."
if ! go run ./cmd/awg seed-freshness \
    -committed "$SEED" \
    -generated "$TMPDIR_WORK/awareness.nt" \
    -ag-repo "$AG"; then
    STALE=true
fi
# awareness.transaction.tsv is informational provenance, NOT a load-bearing
# generated artifact, so it is EXCLUDED from the freshness gate (like the
# annotation reports above). It records the yaml2nt *binary* hash — which is not
# byte-reproducible across Go builds — and volatile cross-repo HEAD SHAs, so a
# whole-file diff here can never pass even when the seed itself is deterministic
# (verified: awareness.nt is byte-identical across regens; only the tool-binary
# line differed). The seed's real freshness is enforced above by the
# ownership-aware awareness.nt comparator; the stamp is still written in normal
# mode as a provenance record, it just no longer gates CI.

echo ""
if $STALE; then
    if $FAIL_ON_STALE; then
        echo "Stale generated files detected." >&2
        echo "Run scripts/build-awareness-graph.sh and commit the generated files." >&2
        exit 1
    fi
    # --warn-stale (GC-2): reaching here means corpus GENERATION succeeded
    # (yaml2nt -strict / -validate-refs / -validate-promotion /
    # -validate-contradictions and the --strict scanner all passed — otherwise
    # set -e would have exited above), so the corpus is correct; only the
    # committed seed/generated artifacts lag. master's seed-rebuild workflow
    # auto-commits the refreshed seed post-merge, so this must not block the PR.
    echo "::warning::awareness seed/generated artifacts are stale; corpus is valid. master will auto-reconcile the seed post-merge (GC-2) — not blocking this PR." >&2
    echo "Stale (advisory): corpus valid; seed auto-rebuilt on master." >&2
    exit 0
fi
echo "All generated files are fresh."

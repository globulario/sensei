#!/usr/bin/env bash
# Compare COMMITTED generated awareness artifacts against a freshly scanned copy.
#
# Extracted so the comparison is EXECUTABLE, not merely inspectable. The first
# version of this check lived inline in build-awareness-graph-self.sh and was
# proven only by a test that scanned the script's source for strings. Review
# pointed out the obvious hole: the comparison could be disabled while every
# string it searched for remained. A source scan cannot distinguish a mechanism
# from a mention of one.
#
# As its own entry point it can be driven directly with two directories, so the
# regression test exercises the real behaviour without building tools or
# scanning ~1700 files.
#
# usage: check-generated-freshness.sh <committed_dir> <fresh_dir>
#   exit 0  every load-bearing artifact matches
#   exit 1  any drifted, missing, or unavailable for comparison
#
# The load-bearing set and the annotation-report exclusion are the repository's
# own, already decided in scripts/build-awareness-graph.sh: _code_symbols.yaml
# and _code_edges.yaml are compared; the annotation report is "informational
# diagnostics, not load-bearing" and is deliberately NOT compared here.
set -uo pipefail

COMMITTED="${1:?usage: check-generated-freshness.sh <committed_dir> <fresh_dir>}"
FRESH="${2:?usage: check-generated-freshness.sh <committed_dir> <fresh_dir>}"

LOAD_BEARING=(
    awareness_graph_code_symbols.yaml
    awareness_graph_code_edges.yaml
)

stale=false
for name in "${LOAD_BEARING[@]}"; do
    committed="$COMMITTED/$name"
    fresh="$FRESH/$name"
    if [[ ! -f "$fresh" ]]; then
        # An artifact the scanner did not produce cannot be judged. Report it
        # rather than passing: an absent comparison is not a clean one.
        echo "  UNCHECKED: $name (no fresh copy to compare against)" >&2
        stale=true
    elif [[ ! -f "$committed" ]]; then
        echo "  MISSING:   $name is not committed" >&2
        stale=true
    elif diff -q "$fresh" "$committed" >/dev/null 2>&1; then
        echo "  ok:        $name"
    else
        echo "  STALE:     $name" >&2
        diff --unified=3 "$committed" "$fresh" >&2 || true
        stale=true
    fi
done

if $stale; then
    echo "STALE: committed generated artifacts do not match a fresh scan." >&2
    exit 1
fi
exit 0

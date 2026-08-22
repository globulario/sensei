#!/usr/bin/env bash
# eval-phase10-worlds.sh — run the #131 evaluation worlds from pinned inputs.
#
# #131 asks for the worlds to run from EXACT pinned inputs, and records that
# worlds 2 and 3 had only ever been run by hand. A measurement that exists only
# as something somebody typed once is not reproducible evidence, so the
# invocation lives here rather than in a shell history.
#
# Usage:
#   scripts/eval-phase10-worlds.sh <out-dir> <captured-at RFC3339> [selection-seed]
#
# Without a seed the run measures the worlds and draws no sample, which is a
# legitimate run: the protocol requires the seed to be committed before labels
# exist, so eval-arms refuses to draw from one nobody fixed in advance.
#
# Environment:
#   GLOBULAR_SRC   checkout to clone world 2 from
#   GLOBULAR_REV   the exact commit world 2 is pinned to (required to run it)
#   WORLD3_SRC / WORLD3_REV / WORLD3_DOMAIN / WORLD3_NAME
#                  an operator-bound world 3 — see "World 3" below before using
#   PROTOCOL_FILE / PROTOCOL_ID
#                  the protocol the sample is stamped with, and its identity.
#                  Both or neither. REQUIRED when a seed is given alongside an
#                  operator-bound world
set -euo pipefail

AG="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$AG"

OUT="${1:-}"
CAPTURED_AT="${2:-}"
SEED="${3:-}"
if [[ -z "$OUT" || -z "$CAPTURED_AT" ]]; then
  echo "usage: $0 <out-dir> <captured-at RFC3339> [selection-seed]" >&2
  exit 2
fi

# The output directory must live OUTSIDE this checkout.
#
# eval-arms materializes synthetic Go mutant repositories under <out>/mutants,
# and world 1 measures this repository by recursively scanning its .go files.
# An output directory inside the tree therefore feeds the harness's own
# generated mutants into the self-evaluation and dirties the tree world 1 is
# trying to bind — the run would measure its own output and then report a tree
# digest for the mess. Refused rather than silently relocated, because a run
# that quietly writes somewhere other than where it was told is its own defect.
mkdir -p "$OUT"
# pwd -P, not pwd: a logical path keeps the symlink, so an output directory
# that merely LOOKS external — a symlink pointing back into the checkout —
# would pass the containment test below and then have eval-arms write mutants
# through it into the tree world 1 scans. Canonicalize before comparing.
OUT_ABS="$(cd "$OUT" && pwd -P)"
AG_P="$(cd "$AG" && pwd -P)"
case "$OUT_ABS/" in
  "$AG_P"/*)
    echo "$0: --out ($OUT_ABS) is inside the evaluated checkout ($AG_P)." >&2
    echo "  world 1 scans this tree, so the harness's own mutants and reports would" >&2
    echo "  become part of what it measures. Choose a path outside the repository." >&2
    exit 2
    ;;
esac

# The output directory must be EMPTY.
#
# eval-arms writes only the artifacts this run produced, and leaves whatever a
# previous run left. A rerun without a seed keeps the old sample/ tree beside an
# index recording the sample as not_run; a rerun omitting a world keeps that
# world's stale report beside the new one. Either way an operator can end up
# adjudicating blinded views that this command did not produce, against an index
# that does not describe them — and nothing in the artifacts would say so.
if [[ -n "$(ls -A "$OUT_ABS" 2>/dev/null)" ]]; then
  echo "$0: --out ($OUT_ABS) is not empty." >&2
  echo "  Artifacts from an earlier run would survive beside this one's index," >&2
  echo "  so an operator could adjudicate views this command never produced." >&2
  echo "  Use a fresh directory, or clear this one deliberately." >&2
  exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

args=(-out "$OUT_ABS" -captured-at "$CAPTURED_AT")
[[ -n "$SEED" ]] && args+=(-selection-seed "$SEED")

# World 1 is this repository, measured in place: it is the tree the run is
# already bound to, and cloning it would measure a different checkout than the
# one whose revision the index records.
args+=(-world "world1_sensei_self=github.com/globulario/sensei=$AG")

# clone_world materializes one external world at an EXACT commit.
#
# The revision is required and verified. `git clone` alone takes whatever the
# source HEAD happens to name, so the same command and the same seed would
# silently evaluate a different world once that checkout advanced — and
# recording the revision after the fact does not make it a pinned input, it
# only makes the drift legible afterwards.
clone_world() {
  local name="$1" domain="$2" src="$3" rev="$4" var="$5"
  if [[ -z "$src" ]]; then
    echo "$0: skipping $name — set $var to a checkout to run it" >&2
    return
  fi
  # A linked checkout made by `git worktree add` has a .git FILE pointing at the
  # shared git directory, so testing for a directory would skip a perfectly
  # cloneable source and record a world the caller did supply as not_run.
  if ! git -C "$src" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "$0: skipping $name — $src is not a git checkout" >&2
    return
  fi
  if [[ -z "$rev" ]]; then
    echo "$0: refusing to run $name — no pinned revision given." >&2
    echo "  Cloning whatever HEAD names would make this world unpinned, and an" >&2
    echo "  unpinned world cannot be re-measured. Set ${var%_SRC}_REV." >&2
    exit 2
  fi
  git clone -q "$src" "$WORK/$name"
  if ! git -C "$WORK/$name" checkout -q "$rev" 2>/dev/null; then
    echo "$0: $name — $src does not contain revision $rev" >&2
    exit 2
  fi
  local got
  got="$(git -C "$WORK/$name" rev-parse HEAD)"
  if [[ "$got" != "$rev"* ]]; then
    echo "$0: $name — asked for $rev, checkout resolved to $got" >&2
    exit 2
  fi
  args+=(-world "$name=$domain=$WORK/$name")
}

clone_world world2_globular github.com/globulario/Globular \
  "${GLOBULAR_SRC:-}" "${GLOBULAR_REV:-}" GLOBULAR_SRC

# World 3.
#
# The frozen protocol names world 3 as the independent SQLite calibration, and
# this script does NOT substitute another repository for it on its own
# authority. That matters more than the inconvenience it causes: eval-arms
# stamps the v1 protocol digest into every sample manifest, so a run that
# quietly swapped the world would produce samples claiming compliance with a
# world definition they did not follow. Protocol section 3 is explicit that a
# correction creates a new version rather than a silent amendment.
#
# There is a measured obstacle to running it as written. The extraction lane is
# Go-only: pointed at a C repository it runs and honestly reports zero
# observations, so SQLite yields a world that measures nothing rather than an
# independent calibration. See docs/evaluation/phase10-world-runs.md.
#
# Resolving that is the evaluation owner's decision, not this script's. Until a
# protocol version names a reachable world 3, eval-arms records world 3 as
# not_run with a reason, which is the honest state. An operator who has made
# that decision can bind one explicitly, and should give it a name that does
# not claim the v1 slot unless the protocol has been amended to match.
# Renaming the world is NOT sufficient, and an earlier version of this script
# stopped there. eval-arms samples every world that ran and stamps the protocol
# digest into the manifest, so a bound replacement would still be sampled under
# a protocol specifying a different world — the exact claim this script exists
# to avoid making. The name only tells a reader; the digest is what the artifact
# asserts.
#
# So binding a world AND drawing a sample requires the operator to name the
# protocol that governs it. The script cannot check that a protocol's text says
# what its binder believes, but it can refuse to stamp v1 over a world v1 does
# not define.
if [[ -n "${WORLD3_SRC:-}" && -n "$SEED" && -z "${PROTOCOL_FILE:-}" ]]; then
  echo "$0: refusing to draw a sample over an operator-bound world under the default protocol." >&2
  echo "  eval-arms stamps the protocol digest into the manifest, so the sample would" >&2
  echo "  claim compliance with a protocol that does not define this world." >&2
  echo "  Either set PROTOCOL_FILE to the amended protocol that does define it," >&2
  echo "  or run without a selection seed to measure the world and draw nothing." >&2
  exit 2
fi
# The ID travels WITH the file, or neither moves. A new protocol document under
# the old identity produces a manifest naming v1 while carrying another
# protocol's digest — a ruler that misstates what it is, which is worse than one
# that refuses to be built.
if [[ -n "${PROTOCOL_FILE:-}" && -z "${PROTOCOL_ID:-}" ]]; then
  echo "$0: PROTOCOL_FILE was given without PROTOCOL_ID." >&2
  echo "  The manifest records both. A new protocol document under the old identity" >&2
  echo "  would name one protocol while carrying another's digest." >&2
  exit 2
fi
if [[ -n "${PROTOCOL_ID:-}" && -z "${PROTOCOL_FILE:-}" ]]; then
  echo "$0: PROTOCOL_ID was given without PROTOCOL_FILE — change both together or neither." >&2
  exit 2
fi
if [[ -n "${PROTOCOL_FILE:-}" ]]; then
  args+=(-protocol-file "$PROTOCOL_FILE" -protocol-id "$PROTOCOL_ID")
fi

clone_world "${WORLD3_NAME:-world3_operator_bound}" "${WORLD3_DOMAIN:-}" \
  "${WORLD3_SRC:-}" "${WORLD3_REV:-}" WORLD3_SRC

# A world that was not supplied is recorded by eval-arms as not_run with a
# reason, so a partial run never reads as a complete protocol.
#
# NOT exec: exec replaces this shell, so the EXIT trap never fires and every run
# would leave a full clone of each external world behind in /tmp. The status is
# forwarded instead, so a caller still sees the evaluator's own exit code.
set +e
go run ./cmd/eval-arms "${args[@]}"
status=$?
set -e
exit "$status"

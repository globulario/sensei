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
#   SENSEI_REV     the exact commit world 1 is pinned to (required to run it);
#                  usually $(git rev-parse HEAD)
#   GLOBULAR_SRC   checkout to clone world 2 from
#   GLOBULAR_REV   the exact commit world 2 is pinned to (required to run it)
#   GIN_SRC        checkout to clone world 3 (gin) from
#   GIN_REV        the exact commit protocol v2 pins world 3 to
#   WORLD3_SRC / WORLD3_REV / WORLD3_DOMAIN / WORLD3_NAME
#                  an EXTRA operator-bound world, for protocols this harness
#                  does not register; it may not claim a protocol-named slot
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

# clone_world materializes one external world at an EXACT commit.
#
# The revision is required and verified. `git clone` alone takes whatever the
# source HEAD happens to name, so the same command and the same seed would
# silently evaluate a different world once that checkout advanced — and
# recording the revision after the fact does not make it a pinned input, it
# only makes the drift legible afterwards.
clone_world() {
  local name="$1" domain="$2" src="$3" rev="$4" var="$5"
  # The name becomes a directory under $WORK and a filename in the report, so a
  # path-like name escapes both. Checked before the clone rather than after,
  # because by the time anything downstream looks the tree is already there.
  case "$name" in
    */*|*\\*|*..*)
      echo "$0: world name '$name' is a path, not a name." >&2
      exit 2
      ;;
  esac
  if [[ -z "$src" ]]; then
    echo "$0: skipping $name — set $var to a checkout to run it" >&2
    return
  fi
  # Past this point the caller SUPPLIED this world, so a bad source is an error
  # rather than an omission. Returning here would let a mistyped or inaccessible
  # path be recorded as not_run and the command still succeed, so automation
  # could accept a run that never executed the world it was explicitly told to
  # measure. Only an unset variable means "not supplied".
  #
  # A linked checkout made by `git worktree add` has a .git FILE pointing at the
  # shared git directory, so the check is a git query rather than a directory
  # test, which would reject a perfectly cloneable source.
  if ! git -C "$src" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "$0: $name — $src is not a git checkout (set via $var)." >&2
    echo "  You asked for this world, so this is a failure rather than an omission." >&2
    exit 2
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

# World 1 is pinned and cloned like every other world.
#
# An earlier version passed the live $AG tree straight through, on the reasoning
# that world 1 IS this repository and cloning would measure a different
# checkout. That reasoning was wrong in the way that matters: worldBinding
# records the revision AFTER extraction, so the tree could be anything and the
# report would simply describe whatever it found. The same documented command
# and seed evaluated a different world whenever the working tree moved — the
# run notes demonstrated exactly that, with world 1's digest changing between
# runs while the pinned worlds stayed byte-identical.
#
# Requiring the revision is an explicit act: SENSEI_REV=$(git rev-parse HEAD)
# says which tree this run means, instead of the run finding out afterwards.
clone_world world1_sensei_self github.com/globulario/sensei \
  "$AG" "${SENSEI_REV:-}" SENSEI_SRC

clone_world world2_globular github.com/globulario/Globular \
  "${GLOBULAR_SRC:-}" "${GLOBULAR_REV:-}" GLOBULAR_SRC

# World 3 is gin, under protocol v2.
#
# v1 named SQLite. Running it produced a measured negative result — the
# extraction lane is Go-only, so a C repository yields zero observations and a
# world that measures nothing. Section 0 of v2 records that, and records why
# Caddy was rejected (its history carries an `awareness: onboard AWG` commit,
# so Sensei had already shaped the repository meant to calibrate it).
#
# The binding is exact: repository AND revision. eval-arms enforces both under
# v2, so a gin checkout cannot be filed under v1 and SQLite cannot be filed
# under v2. There is deliberately no compatibility alias.
GIN_SRC="${GIN_SRC:-$HOME/Documents/github.com/gin-gonic/gin}"
GIN_REV="${GIN_REV:-34dac209ffb6ef85cc78c5d217bbb7ad001d68fd}"
clone_world world3_independent_calibration github.com/gin-gonic/gin \
  "$GIN_SRC" "$GIN_REV" GIN_SRC

# An operator-bound extra world remains available for protocols this harness
# does not register. It must not claim a protocol-named slot.
if [[ -n "${WORLD3_SRC:-}" ]]; then
  clone_world "${WORLD3_NAME:-world3_operator_bound}" "${WORLD3_DOMAIN:-}" \
    "${WORLD3_SRC:-}" "${WORLD3_REV:-}" WORLD3_SRC
fi

# The EVALUATOR is pinned too, not just the worlds it measures.
#
# Pinning world 1 and then compiling eval-arms from the live tree left the same
# asymmetry one level up: the measured tree was fixed while the measuring binary
# was whatever happened to be checked out, so the same SENSEI_REV and seed could
# produce different reports with nothing recording why. A self-evaluation whose
# instrument is unpinned is not re-measurable.
#
# Built from its own clone rather than from the world-1 clone, so compiling
# cannot touch the tree being measured.
if [[ -z "${SENSEI_REV:-}" ]]; then
  echo "$0: SENSEI_REV is required — it pins both world 1 and the evaluator itself." >&2
  exit 2
fi
git clone -q "$AG" "$WORK/_runner"
if ! git -C "$WORK/_runner" checkout -q "$SENSEI_REV" 2>/dev/null; then
  echo "$0: this checkout does not contain SENSEI_REV $SENSEI_REV" >&2
  exit 2
fi
echo "$0: evaluator pinned at $(git -C "$WORK/_runner" rev-parse HEAD)" >&2
( cd "$WORK/_runner" && go build -o "$WORK/eval-arms" ./cmd/eval-arms )

# A world that was not supplied is recorded by eval-arms as not_run with a
# reason, so a partial run never reads as a complete protocol.
#
# NOT exec: exec replaces this shell, so the EXIT trap never fires and every run
# would leave a full clone of each external world behind in /tmp. The status is
# forwarded instead, so a caller still sees the evaluator's own exit code.
#
# Run from the pinned clone so the default --protocol-file resolves against the
# revision the evaluator was built from rather than the live tree.
set +e
( cd "$WORK/_runner" && "$WORK/eval-arms" "${args[@]}" )
status=$?
set -e
exit "$status"

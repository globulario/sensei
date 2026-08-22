#!/usr/bin/env bash
# eval-phase10-worlds.sh — run every #131 evaluation world from pinned inputs.
#
# #131 asks for all four worlds to run from EXACT pinned inputs, and records
# that worlds 2 and 3 had only ever been run by hand. A measurement that exists
# only as something somebody typed once is not reproducible evidence, so the
# command lives here rather than in a shell history.
#
# The worlds are cloned into a scratch directory rather than measured in place.
# That is deliberate and load-bearing twice over:
#
#   - a working checkout carries untracked tooling (.sensei/, .claude/, an
#     authored docs/awareness/), which makes the tree dirty. eval-arms then
#     falls back to a tree digest, so the run identifies itself by a hash
#     nobody can look up instead of by a revision anybody can check out.
#   - for world 3 those untracked artifacts are worse than untidy. World 3 is
#     the INDEPENDENT calibration, and Sensei-authored awareness sitting in the
#     tree is exactly the "Sensei-specific ontology becoming the hidden answer
#     key" the issue forbids. A clean clone carries only what upstream tracks.
#
# Usage:
#   scripts/eval-phase10-worlds.sh <out-dir> <captured-at> [selection-seed]
#
# Without a seed the run measures the worlds and draws no sample, which is a
# legitimate run: the protocol requires the seed to be committed before labels
# exist, so eval-arms refuses to draw from one nobody fixed in advance.
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

# Where the sibling checkouts live. Override to point at your own clones.
GLOBULAR_SRC="${GLOBULAR_SRC:-$HOME/Documents/github.com/globulario/Globular}"
GIN_SRC="${GIN_SRC:-$HOME/Documents/github.com/gin-gonic/gin}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

args=(-out "$OUT" -captured-at "$CAPTURED_AT")
[[ -n "$SEED" ]] && args+=(-selection-seed "$SEED")

# World 1 is this repository, measured in place: it is the tree the run is
# already bound to, and cloning it would measure a different checkout than the
# one whose revision the index records.
args+=(-world "world1_sensei_self=github.com/globulario/sensei=$AG")

clone_world() {
  local name="$1" domain="$2" src="$3"
  if [[ ! -d "$src/.git" ]]; then
    echo "$0: skipping $name — no checkout at $src (set ${4} to override)" >&2
    return
  fi
  git clone -q "$src" "$WORK/$name"
  args+=(-world "$name=$domain=$WORK/$name")
}

clone_world world2_globular github.com/globulario/Globular "$GLOBULAR_SRC" GLOBULAR_SRC
# World 3 is gin: Go, independently maintained, and NOT a repository Sensei has
# onboarded. Caddy was rejected as a candidate for precisely that reason — its
# checkout carries an "awareness: onboard AWG" commit, so Sensei has already
# shaped the thing that was supposed to calibrate it independently.
clone_world world3_independent_calibration github.com/gin-gonic/gin "$GIN_SRC" GIN_SRC

# A world that has no checkout here is recorded by eval-arms as not_run with a
# reason, so a partial run never reads as a complete protocol.
exec go run ./cmd/eval-arms "${args[@]}"

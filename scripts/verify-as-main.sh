#!/usr/bin/env bash
# verify-as-main.sh — run the suite in the state a branch will have AFTER it lands.
#
# WHY THIS EXISTS
#
# CI before a merge exercises exactly one branch state, and it is not the state
# that ships. A pull request is never on the admitted branch; main always is.
# So a defect reachable only when HEAD == origin/main is invisible to every
# pull-request run, and green on the PR says nothing about the merge.
#
# That is not hypothetical. This repository's main failed CI on the merges of
# #318, #320 and #321 -- the cmd/awg promotion-gate family passes on every
# branch and fails only on the admitted branch -- and each PR was green when
# reviewed.
#
# The landing rule proves the merged composition is the reviewed composition,
# which is a property of the TREE. This answers the other question: is the
# RESULT green.
#
#   usage: scripts/verify-as-main.sh [branch]     (default: current branch)
#
# Exit: 0 green as main; 1 red as main; 2 the state could not be constructed.
#
# NOTE: a green result here does not make watching main unnecessary. The durable
# fix for a main-only defect is to CONSTRUCT its state in a fixture so a branch
# can falsify it -- see TestOnTheAdmittedBranchThePromotionBaseIsHeadItself.
# This script is the safety net, not the repair.
set -u

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
branch="${1:-$(git -C "$repo" rev-parse --abbrev-ref HEAD)}"

command -v go >/dev/null 2>&1 || { echo "error: go toolchain required" >&2; exit 2; }

tip="$(git -C "$repo" rev-parse "$branch" 2>/dev/null)" || {
  echo "error: no such branch: $branch" >&2; exit 2; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git clone -q "$repo" "$work" 2>/dev/null || { echo "error: clone failed" >&2; exit 2; }
cd "$work" || exit 2
git checkout -q --detach "$tip" 2>/dev/null || { echo "error: cannot check out $tip" >&2; exit 2; }

# CONSTRUCT the admitted-branch state: point origin/main at this tip, so the
# checkout is in the shape it will have once the branch has landed.
git update-ref refs/remotes/origin/main "$tip"

# ASSERT the state is the case under test. Without this the script silently
# measures the ordinary branch shape and reports a green that means nothing --
# which is the exact defect class it was written to catch.
if ! git merge-base --is-ancestor HEAD origin/main; then
  echo "error: the admitted-branch state was not constructed; refusing to report a result" >&2
  exit 2
fi

echo "verify-as-main: $branch @ ${tip:0:12}, with origin/main pointed at it"
if go test ./... -count=1; then
  echo "verify-as-main: GREEN as main"
  exit 0
fi
echo "verify-as-main: RED as main -- this branch would break main if it landed" >&2
exit 1

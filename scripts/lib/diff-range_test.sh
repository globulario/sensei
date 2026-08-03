#!/usr/bin/env bash
# diff-range_test.sh — regression tests for resolve_diff_range.
#
# Usage: bash scripts/lib/diff-range_test.sh
set -euo pipefail

lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=diff-range.sh
source "$lib_dir/diff-range.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

repo="$tmp/repo"
mkdir -p "$repo"
git -C "$repo" init --quiet -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name "Test"

echo one > "$repo/a.txt"
git -C "$repo" add a.txt
git -C "$repo" commit --quiet -m "first"
first_sha=$(git -C "$repo" rev-parse HEAD)

echo two >> "$repo/a.txt"
git -C "$repo" commit --quiet -am "second"
second_sha=$(git -C "$repo" rev-parse HEAD)

echo three >> "$repo/a.txt"
git -C "$repo" commit --quiet -am "third (checkout HEAD)"
head_sha=$(git -C "$repo" rev-parse HEAD)

failures=0

assert_ok() {
  local name=$1 range=$2 want_base=$3 want_head=$4
  local out
  if ! out=$(resolve_diff_range "$repo" "$range" 2>&1); then
    echo "FAIL $name: expected success, resolve_diff_range failed: $out" >&2
    failures=$((failures + 1))
    return
  fi
  local got_base got_head
  got_base=$(sed -n '1p' <<<"$out")
  got_head=$(sed -n '2p' <<<"$out")
  if [[ "$got_base" != "$want_base" || "$got_head" != "$want_head" ]]; then
    echo "FAIL $name: got base=$got_base head=$got_head, want base=$want_base head=$want_head" >&2
    failures=$((failures + 1))
  else
    echo "ok $name"
  fi
}

assert_rejected() {
  local name=$1 range=$2
  if resolve_diff_range "$repo" "$range" >/tmp/diff-range-test-stdout 2>/tmp/diff-range-test-stderr; then
    echo "FAIL $name: expected rejection, got success: $(cat /tmp/diff-range-test-stdout)" >&2
    failures=$((failures + 1))
  else
    echo "ok $name ($(cat /tmp/diff-range-test-stderr))"
  fi
}

# The real, valid shape the workflow always constructs.
assert_ok valid_triple_dot_range "$first_sha...$head_sha" "$first_sha" "$head_sha"
assert_ok valid_triple_dot_range_symbolic_head "$first_sha...HEAD" "$first_sha" "$head_sha"

# Two-dot ranges must be rejected, not silently misparsed.
assert_rejected two_dot_range "$first_sha..$head_sha"

# A right side that resolves to a real commit, but not the current checkout
# HEAD, must be rejected -- this is the exact defect: computing head_revision
# as `git rev-parse HEAD` regardless of what the range asked for.
assert_rejected non_head_right_side "$first_sha...$second_sha"

# Malformed ranges: no separator at all, and more than one "...".
assert_rejected malformed_no_separator "$head_sha"
assert_rejected malformed_double_triple_dot "$first_sha...$second_sha...$head_sha"

# An unresolvable base must fail closed, never fall back to a guess like HEAD^.
assert_rejected unresolvable_base "not-a-real-ref...$head_sha"

if ((failures > 0)); then
  echo "$failures resolve_diff_range regression(s) failed" >&2
  exit 1
fi
echo "all resolve_diff_range regressions passed"

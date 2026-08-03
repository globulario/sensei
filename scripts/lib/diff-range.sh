#!/usr/bin/env bash
# diff-range.sh — validate and resolve one exact <base>...<head> diff range.
#
# The activation script's job is to bind a whole evidence bundle (changed
# files, preflight, gate, HOW/WHY, Codex review) to one exact revision pair.
# A generic --diff string undermines that: head_revision was always computed
# as `git rev-parse HEAD` regardless of what the range's right side actually
# named, a two-dot range's base was silently misparsed by a pattern that only
# matches a literal "...", and a right side naming something other than the
# current checkout HEAD could certify a diff the script never actually
# evaluated. resolve_diff_range rejects all three shapes instead of silently
# certifying whatever it can compute.
#
# Usage: resolve_diff_range <repo> <range>
# On success prints two lines: resolved_base_sha, resolved_head_sha; exit 0.
# On failure prints one error line to stderr; exit 1. Never falls back to a
# guessed revision (e.g. HEAD^) -- an unresolvable or mismatched range is a
# hard failure, not a best-effort substitute.

resolve_diff_range() {
  local repo=$1 range=$2

  local dot_count
  dot_count=$(grep -o '\.\.\.' <<<"$range" | wc -l)
  if [[ "$range" != *"..."* || "$dot_count" -ne 1 ]]; then
    echo "diff range must be exactly one <base>...<head> triple-dot range, got: $range" >&2
    return 1
  fi

  local left=${range%%...*}
  local right=${range#*...}
  if [[ -z "$left" || -z "$right" ]]; then
    echo "diff range must name both a base and a head: $range" >&2
    return 1
  fi
  # A two-dot range ("a..b") satisfies the one-"..."-substring check above
  # only by accident when neither side is empty (e.g. "a..b" has zero "..."
  # substrings and is already rejected); guard the remaining case explicitly:
  # a left/right side that itself still contains ".." after stripping "..."
  # is not a clean single endpoint.
  if [[ "$left" == *".."* || "$right" == *".."* ]]; then
    echo "diff range endpoints must not contain '..': $range" >&2
    return 1
  fi

  local resolved_base resolved_head actual_head
  if ! resolved_base=$(git -C "$repo" rev-parse --verify "${left}^{commit}" 2>/dev/null); then
    echo "cannot resolve base revision '$left' in $range" >&2
    return 1
  fi
  if ! resolved_head=$(git -C "$repo" rev-parse --verify "${right}^{commit}" 2>/dev/null); then
    echo "cannot resolve head revision '$right' in $range" >&2
    return 1
  fi
  actual_head=$(git -C "$repo" rev-parse HEAD)
  if [[ "$resolved_head" != "$actual_head" ]]; then
    echo "diff range head ('$right' -> $resolved_head) does not match checkout HEAD ($actual_head); refusing to certify a mismatched pair" >&2
    return 1
  fi

  printf '%s\n%s\n' "$resolved_base" "$resolved_head"
}

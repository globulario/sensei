#!/usr/bin/env bash
# check-release-boundary.sh — fail if a public/runtime release root contains
# managed-governance assets that should stay out of the open distribution path.
#
# Usage:
#   ./scripts/check-release-boundary.sh <staged-root> [<staged-root>...]
#
# This enforces the product-moat boundary:
#   - local runtime artifacts may be public
#   - managed-governance packs, trust roots, and activation state may not be

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: ./scripts/check-release-boundary.sh <staged-root> [<staged-root>...]" >&2
  exit 2
fi

failures=0

check_root() {
  local root="$1"
  if [[ ! -d "$root" ]]; then
    echo "check-release-boundary: not a directory: $root" >&2
    failures=$((failures + 1))
    return
  fi

  local -a patterns=(
    '*/governance-pack.nt'
    '*/governance-pack.manifest.json'
    '*/governance-pack.manifest.sig'
    '*/trusted-publishers.json'
    '*/active.json'
    '*/.awg/governance/*'
    '*/governance/packs/*'
    '*/governance/incoming/*'
  )

  local hit=""
  for pattern in "${patterns[@]}"; do
    hit=$(find "$root" -path "$pattern" -print -quit)
    if [[ -n "$hit" ]]; then
      echo "check-release-boundary: protected managed-governance asset found in public/runtime release root:" >&2
      echo "  root: $root" >&2
      echo "  file: $hit" >&2
      failures=$((failures + 1))
      return
    fi
  done
}

for root in "$@"; do
  check_root "$root"
done

if [[ "$failures" -gt 0 ]]; then
  echo "check-release-boundary: FAIL (${failures} root(s) violated the public/runtime deployment boundary)" >&2
  exit 1
fi

echo "check-release-boundary: ok"

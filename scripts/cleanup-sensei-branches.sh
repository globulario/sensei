#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-globulario/sensei}"
REMOTE="${REMOTE:-origin}"
BASE="${BASE:-main}"
MODE="${1:-}"

if [[ "$MODE" != "" && "$MODE" != "--execute" ]]; then
  echo "usage: $0 [--execute]"
  exit 2
fi

for cmd in git gh; do
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "error: required command '$cmd' is not installed" >&2
    exit 1
  }
done

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "error: run this inside a clone of $REPO" >&2
  exit 1
}

gh auth status >/dev/null 2>&1 || {
  echo "error: GitHub CLI is not authenticated; run 'gh auth login' first" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

all="$tmp/all"
merged_ancestry="$tmp/merged-ancestry"
merged_pr_exact="$tmp/merged-pr-exact"
open="$tmp/open"
protected="$tmp/protected"
safe_union="$tmp/safe-union"
excluded="$tmp/excluded"
safe="$tmp/safe"
unproven="$tmp/unproven"

echo "Fetching $REMOTE..."
git fetch "$REMOTE" --prune

# All current remote branches.
git for-each-ref --format='%(refname)' "refs/remotes/$REMOTE/" \
  | sed "s#^refs/remotes/$REMOTE/##" \
  | grep -v '^HEAD$' \
  | sort -u > "$all"

if ! grep -qxF "$BASE" "$all"; then
  echo "error: $REMOTE/$BASE does not exist" >&2
  exit 1
fi

# Class A: branches whose current tip is already reachable from main.
git for-each-ref --merged="$REMOTE/$BASE" --format='%(refname)' "refs/remotes/$REMOTE/" \
  | sed "s#^refs/remotes/$REMOTE/##" \
  | grep -v '^HEAD$' \
  | grep -vxF "$BASE" \
  | sort -u > "$merged_ancestry" || true

# Class B: squash/rebase merged PR branches.
# Only accept a PR merged DIRECTLY to main, and only when the branch has not
# moved since that PR: current branch SHA must equal the recorded PR head SHA.
: > "$merged_pr_exact"
while IFS=$'\t' read -r branch pr_sha; do
  [[ -n "${branch:-}" && -n "${pr_sha:-}" ]] || continue
  current_sha="$(git rev-parse "$REMOTE/$branch" 2>/dev/null || true)"
  if [[ -n "$current_sha" && "$current_sha" == "$pr_sha" ]]; then
    printf '%s\n' "$branch" >> "$merged_pr_exact"
  fi
done < <(
  gh pr list \
    --repo "$REPO" \
    --state merged \
    --limit 1000 \
    --json headRefName,headRefOid,baseRefName \
    --jq ".[] | select(.baseRefName == \"$BASE\") | [.headRefName,.headRefOid] | @tsv"
)
sort -u -o "$merged_pr_exact" "$merged_pr_exact"

# Never delete a branch that currently backs an open PR.
gh pr list \
  --repo "$REPO" \
  --state open \
  --limit 1000 \
  --json headRefName \
  --jq '.[].headRefName' \
  | sort -u > "$open"

# Never delete a protected branch.
gh api --paginate "repos/$REPO/branches?per_page=100" \
  --jq '.[] | select(.protected == true) | .name' \
  | sort -u > "$protected"

printf '%s\n' "$BASE" > "$excluded"
cat "$open" "$protected" >> "$excluded"
sort -u -o "$excluded" "$excluded"

cat "$merged_ancestry" "$merged_pr_exact" \
  | sort -u \
  | comm -12 "$all" - \
  > "$safe_union"

# Remove main, protected branches, and open-PR heads.
comm -23 "$safe_union" "$excluded" > "$safe"

# Everything else is intentionally left alone.
comm -23 "$all" <(cat "$safe" "$excluded" | sort -u) > "$unproven"

total="$(wc -l < "$all" | tr -d ' ')"
safe_count="$(wc -l < "$safe" | tr -d ' ')"
open_count="$(wc -l < "$open" | tr -d ' ')"
protected_count="$(wc -l < "$protected" | tr -d ' ')"
unproven_count="$(wc -l < "$unproven" | tr -d ' ')"

echo
echo "Repository:       $REPO"
echo "Remote branches:  $total"
echo "Safe to delete:   $safe_count"
echo "Open-PR heads:    $open_count"
echo "Protected:        $protected_count"
echo "Unproven/kept:    $unproven_count"
echo

if [[ "$safe_count" -gt 0 ]]; then
  echo "Safe deletion candidates:"
  sed 's/^/  - /' "$safe"
else
  echo "No branch is proven safe to delete."
fi

if [[ "$MODE" != "--execute" ]]; then
  echo
  echo "DRY RUN ONLY. Nothing was deleted."
  echo "Review the list, then run:"
  echo "  $0 --execute"
  exit 0
fi

echo
echo "Deleting only the proven-safe branches above..."
while IFS= read -r branch; do
  [[ -n "$branch" ]] || continue
  echo "Deleting $branch"
  git push "$REMOTE" --delete "$branch"
done < "$safe"

git fetch "$REMOTE" --prune

echo
echo "Cleanup complete."
echo "Remaining remote branches:"
git for-each-ref --format='%(refname)' "refs/remotes/$REMOTE/" \
  | sed "s#^refs/remotes/$REMOTE/##" \
  | grep -v '^HEAD$' \
  | sort -u

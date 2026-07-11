#!/usr/bin/env bash
# awg-init-repo.sh — initialize AWG on an existing repository, repeatably.
#
# Runs the DETERMINISTIC passes end to end and prints a checklist for the
# JUDGMENT passes (rule authoring, grounding, promotion, gate). Encodes the
# gotchas that are easy to forget:
#   • namespaces.yaml is REQUIRED for code-symbol extraction (bootstrap skips it silently otherwise)
#   • cold-bootstrap --repo-slug fetches ALL PRs (unbounded → hangs on big repos);
#     we fetch a bounded recent window with gh and feed --pr-comments instead
#   • foreign repos need --repo <domain> so every structural node is domain-scoped
#   • validate must scan BOTH docs/awareness AND docs/awareness/generated
#   • cold-bootstrap writes status:candidate only — nothing is auto-promoted
#
# Usage:
#   scripts/awg-init-repo.sh --repo <path> --domain github.com/owner/name \
#       [--repo-slug owner/name] [--pr-window 200] [--commit-window HEAD~500..HEAD] \
#       [--drafter none|llm|claude-cli] [--skip-history] [--awg awg]
set -euo pipefail

REPO="" DOMAIN="" REPO_SLUG="" PR_WINDOW=200 COMMIT_WINDOW="HEAD~500..HEAD"
DRAFTER="none" SKIP_HISTORY=false AWG="${AWG:-awg}"

while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="$2"; shift 2;;
    --domain) DOMAIN="$2"; shift 2;;
    --repo-slug) REPO_SLUG="$2"; shift 2;;
    --pr-window) PR_WINDOW="$2"; shift 2;;
    --commit-window) COMMIT_WINDOW="$2"; shift 2;;
    --drafter) DRAFTER="$2"; shift 2;;
    --skip-history) SKIP_HISTORY=true; shift;;
    --awg) AWG="$2"; shift 2;;
    -h|--help) grep '^#' "$0" | sed 's/^# \?//'; exit 0;;
    *) echo "unknown flag: $1" >&2; exit 2;;
  esac
done

[ -n "$REPO" ] && [ -d "$REPO" ] || { echo "error: --repo <existing path> required" >&2; exit 2; }
[ -n "$DOMAIN" ] || { echo "error: --domain github.com/owner/name required" >&2; exit 2; }
command -v "$AWG" >/dev/null 2>&1 || { echo "error: sensei not found (set --awg or PATH)" >&2; exit 2; }
REPO="$(cd "$REPO" && pwd)"
AWDIR="$REPO/docs/awareness"
NS="${DOMAIN##*/}"   # namespace id from the repo name

echo "== AWG init: $DOMAIN =="
echo "   repo:   $REPO"
echo "   awg:    $($AWG version 2>/dev/null || echo "$AWG")"
echo

# ── 1. Namespace registry (REQUIRED for symbol extraction) ────────────────────
if [ -f "$AWDIR/namespaces.yaml" ]; then
  echo "[1/5] namespaces.yaml: present (keeping)"
else
  echo "[1/5] namespaces.yaml: scaffolding (needed for code-symbol extraction)"
  mkdir -p "$AWDIR"
  # infer owned source roots = top-level dirs that contain .go files anywhere beneath
  owns=$(find "$REPO" -name '*.go' -not -path '*/vendor/*' -not -path '*/.git/*' \
           -not -path '*/testdata/*' 2>/dev/null | sed "s|$REPO/||" \
           | grep '/' | cut -d/ -f1 | sort -u | grep -vE '^(docs|vendor|\.)' || true)
  {
    echo "# Namespace registry — maps source paths to owning namespaces so the"
    echo "# annotation scanner / code-symbol extraction can attribute symbols."
    echo "# Adjust 'owns' to your repo's real source roots."
    echo "namespaces:"
    echo "  - id: $NS"
    echo "    label: ${NS} source"
    echo "    owns:"
    if [ -n "$owns" ]; then printf '      - %s\n' $owns; else echo "      - ."; fi
    echo "    description: ${NS} — source tree."
  } > "$AWDIR/namespaces.yaml"
  echo "        owns: $(echo $owns | tr '\n' ' ')"
fi
echo

# ── 2. Structural extraction (deterministic) ──────────────────────────────────
echo "[2/5] bootstrap: components + imports + code symbols + tests (--skip-build)"
"$AWG" bootstrap --repo "$REPO" --skip-build 2>&1 \
  | grep -iE "components|import dep|tests found|source anchors|source_symbols|wrote generated|findings" \
  | sed 's/^/      /' || true
echo

# ── 3. Onboarding brief for an agent (knowledge layer) ────────────────────────
echo "[3/5] onboard export: brief for your AI agent to draft rules"
BRIEF="$REPO/.awg-onboard-brief.md"
"$AWG" onboard export --repo "$REPO" --drafter "$DRAFTER" --out "$BRIEF" 2>&1 | sed 's/^/      /' || true
echo "      brief: $BRIEF"
echo "      → hand it to your agent, then: $AWG onboard import --repo \"$REPO\" --from drafts.json"
echo

# ── 4. History mining (commits + PR reviews → triangulated candidates) ────────
if $SKIP_HISTORY; then
  echo "[4/5] cold-bootstrap: skipped (--skip-history)"
elif [ -n "$REPO_SLUG" ] && command -v gh >/dev/null 2>&1; then
  echo "[4/5] cold-bootstrap: mining $REPO_SLUG PR reviews + $COMMIT_WINDOW commits"
  # GOTCHA: --repo-slug fetches ALL PRs (unbounded → hangs). Fetch a bounded
  # recent window ourselves and feed --pr-comments (the offline path).
  PRJSON="$(mktemp)"; PAGES=$(( (PR_WINDOW + 99) / 100 ))
  for p in $(seq 1 "$PAGES"); do
    gh api "repos/$REPO_SLUG/pulls/comments?per_page=100&sort=created&direction=desc&page=$p" 2>/dev/null || true
  done | jq -s 'add // []' \
     | jq '[.[] | {PRID:(.pull_request_url|split("/")|last), CommentID:(.id|tostring),
                   Path:.path, Line:(.line // .original_line // 0), Body:.body}]' > "$PRJSON" 2>/dev/null || echo '[]' > "$PRJSON"
  echo "      fetched $(jq 'length' "$PRJSON" 2>/dev/null || echo 0) PR review comments"
  "$AWG" cold-bootstrap --repo "$REPO" --pr-comments "$PRJSON" --since "$COMMIT_WINDOW" \
    --out "$AWDIR/candidates" 2>&1 | grep -iE "triangulat|candidate|held.back|wrote|signals" | tail -6 | sed 's/^/      /' || true
  rm -f "$PRJSON"
else
  echo "[4/5] cold-bootstrap: no --repo-slug (or gh missing) — PR-review channel skipped."
  echo "      triangulation needs a commit AND a review signal; commit-only themes are held back."
fi
echo

# ── 5. Validate (both dirs — generated holds the components rules reference) ───
echo "[5/5] validate"
"$AWG" validate -repo-root "$REPO" -dir "$AWDIR" -dir "$AWDIR/generated" 2>&1 | tail -3 | sed 's/^/      /' || true
echo

# ── Next steps: the JUDGMENT passes a script cannot do for you ────────────────
cat <<NEXT
== Deterministic passes done. Next (human/agent judgment) ==

  A. AUTHOR rules — hand $BRIEF to your agent; it drafts invariants/contracts/
     forbidden-fixes/failure-modes as JSON. Land them:
        $AWG onboard import --repo "$REPO" --from drafts.json
     Then PROMOTE reviewed ones from docs/awareness/candidates/proposals/ into the
     canonical docs/awareness/{invariants,contracts,forbidden_fixes,failure_modes}.yaml
     (severity vocab: critical|high|warning|info|degraded).

  B. GROUND-CHECK the history-mined candidates in docs/awareness/candidates/ against
     current code (cited file/line/symbol still real?) BEFORE promoting. Reverts are
     the strongest signal; reviewer *questions* are not settled rules.

  C. MAKE RULES GATEABLE — add a detect block + 'enforcement: block' to the rules you
     want CI to enforce:
        detect: { applies_to_paths: ["**/x.go"], forbidden_pattern: '...', required_pattern: '...', enforcement: block }

  D. WIRE THE GATE — copy docs/awg-gate.example.yml to $REPO/.github/workflows/awg-gate.yml
     (add --domain $DOMAIN to the gate step for a foreign repo). Test locally:
        $AWG serve -no-seed & ; $AWG build --repo $DOMAIN --input $AWDIR --input $AWDIR/generated
        $AWG gate --enforce --repo-root $REPO --diff HEAD --domain $DOMAIN

  E. VERIFY payoff — brief a real file:
        $AWG briefing --file <path> --domain $DOMAIN
NEXT

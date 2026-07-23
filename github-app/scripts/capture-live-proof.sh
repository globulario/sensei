#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  capture-live-proof.sh OWNER/REPO PR APP_ID INSTALLATION_ID IMAGE_DIGEST DELIVERY_IDS [OUTPUT]

Example:
  capture-live-proof.sh globulario/sensei-app-test 12 123456 987654 \
    sha256:abc123 delivery-opened,delivery-redelivery,delivery-synchronize \
    sensei-github-app-live-proof.json

Requires authenticated `gh` and `jq`.
EOF
}

if [[ $# -lt 6 || $# -gt 7 ]]; then
  usage >&2
  exit 2
fi

repo="$1"
pr_number="$2"
app_id="$3"
installation_id="$4"
image_digest="$5"
delivery_ids="$6"
output="${7:-sensei-github-app-live-proof.json}"

for command in gh jq; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "Required command not found: $command" >&2
    exit 1
  fi
done

if [[ ! "$repo" =~ ^[^/]+/[^/]+$ ]]; then
  echo "Repository must use OWNER/REPO form" >&2
  exit 2
fi
if [[ ! "$pr_number" =~ ^[1-9][0-9]*$ || ! "$app_id" =~ ^[1-9][0-9]*$ || ! "$installation_id" =~ ^[1-9][0-9]*$ ]]; then
  echo "PR, app ID, and installation ID must be positive integers" >&2
  exit 2
fi
if [[ ! "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "Image digest must be sha256:<64 lowercase hex characters>" >&2
  exit 2
fi

pr_json="$(gh api "repos/$repo/pulls/$pr_number")"
base_sha="$(jq -r '.base.sha' <<<"$pr_json")"
head_sha="$(jq -r '.head.sha' <<<"$pr_json")"
pr_url="$(jq -r '.html_url' <<<"$pr_json")"

comments_json="$(gh api --paginate "repos/$repo/issues/$pr_number/comments?per_page=100" --slurp | jq 'add // []')"
checks_json="$(gh api -H 'Accept: application/vnd.github+json' "repos/$repo/commits/$head_sha/check-runs?check_name=Sensei%20Architectural%20Briefing&per_page=100")"

owned_comments="$(jq --argjson app_id "$app_id" '[.[] | select(.performed_via_github_app.id == $app_id and (.body | contains("<!-- sensei-architectural-briefing -->")))]' <<<"$comments_json")"
owned_checks="$(jq --argjson app_id "$app_id" --arg external_id "sensei-pr-$pr_number-$head_sha" '[.check_runs[] | select(.app.id == $app_id and .external_id == $external_id)]' <<<"$checks_json")"

comment_count="$(jq 'length' <<<"$owned_comments")"
check_count="$(jq 'length' <<<"$owned_checks")"
if [[ "$comment_count" != "1" ]]; then
  echo "Expected exactly one app-owned sticky comment, found $comment_count" >&2
  exit 1
fi
if [[ "$check_count" != "1" ]]; then
  echo "Expected exactly one app-owned head-bound Check Run, found $check_count" >&2
  exit 1
fi

comment_id="$(jq -r '.[0].id' <<<"$owned_comments")"
comment_url="$(jq -r '.[0].html_url' <<<"$owned_comments")"
check_id="$(jq -r '.[0].id' <<<"$owned_checks")"
check_url="$(jq -r '.[0].html_url' <<<"$owned_checks")"
check_conclusion="$(jq -r '.[0].conclusion' <<<"$owned_checks")"

jq -n \
  --arg repository "$repo" \
  --argjson pull_request "$pr_number" \
  --arg pull_request_url "$pr_url" \
  --argjson app_id "$app_id" \
  --argjson installation_id "$installation_id" \
  --arg base_sha "$base_sha" \
  --arg head_sha "$head_sha" \
  --arg image_digest "$image_digest" \
  --arg delivery_ids "$delivery_ids" \
  --argjson comment_id "$comment_id" \
  --arg comment_url "$comment_url" \
  --argjson check_run_id "$check_id" \
  --arg check_run_url "$check_url" \
  --arg check_conclusion "$check_conclusion" \
  --arg captured_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    schema: "sensei.github_app.live_proof.v1",
    captured_at: $captured_at,
    repository: $repository,
    pull_request: $pull_request,
    pull_request_url: $pull_request_url,
    app_id: $app_id,
    installation_id: $installation_id,
    image_digest: $image_digest,
    delivery_ids: ($delivery_ids | split(",") | map(select(length > 0))),
    base_sha: $base_sha,
    head_sha: $head_sha,
    sticky_comment: {id: $comment_id, url: $comment_url, count: 1},
    check_run: {id: $check_run_id, url: $check_run_url, count: 1, conclusion: $check_conclusion, external_id: ("sensei-pr-" + ($pull_request|tostring) + "-" + $head_sha)},
    assertions: {
      exactly_one_app_owned_sticky_comment: true,
      exactly_one_app_owned_check_for_current_head: true,
      current_pr_identity_captured: true
    }
  }' > "$output"

jq . "$output"
echo "Wrote live proof receipt: $output" >&2

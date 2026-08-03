#!/usr/bin/env bash
# preflight-verdict_test.sh — regression tests for preflight_verdict.
#
# Usage: bash scripts/lib/preflight-verdict_test.sh
set -euo pipefail

lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=preflight-verdict.sh
source "$lib_dir/preflight-verdict.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0

assert_verdict() {
  local name=$1 json=$2 want=$3
  local path="$tmp/$name.json"
  printf '%s' "$json" > "$path"
  local got
  got=$(preflight_verdict "$path")
  if [[ "$got" != "$want" ]]; then
    echo "FAIL $name: got '$got', want '$want'" >&2
    failures=$((failures + 1))
  else
    echo "ok $name"
  fi
}

# The exact regression this exists for: sensei preflight exits 0 with a
# DEGRADED, UNKNOWN_IMPACT, thin-coverage body -- the process succeeded, the
# finding did not. A caller checking only the exit code would never see this.
assert_verdict degraded_status_exit_0_still_fails \
  '{"status":"PREFLIGHT_STATUS_DEGRADED","risk_class":"UNKNOWN_IMPACT","coverage":{"file_count":1,"sufficient":false,"note":"domain scope could not be verified"}}' \
  "degraded:status_degraded"

assert_verdict unknown_impact_without_degraded_status_still_fails \
  '{"status":"PREFLIGHT_STATUS_OK","risk_class":"UNKNOWN_IMPACT","coverage":{"file_count":1,"sufficient":true}}' \
  "degraded:unknown_impact"

assert_verdict insufficient_coverage_still_fails \
  '{"status":"PREFLIGHT_STATUS_OK","risk_class":"LOW_RISK","coverage":{"file_count":1,"sufficient":false,"note":"thin"}}' \
  "degraded:insufficient_coverage"

assert_verdict missing_coverage_sufficient_defaults_to_failing \
  '{"status":"PREFLIGHT_STATUS_OK","risk_class":"LOW_RISK","coverage":{"file_count":1}}' \
  "degraded:insufficient_coverage"

assert_verdict malformed_json_fails \
  '{not json' \
  "malformed:parse_error:Expecting property name enclosed in double quotes: line 1 column 2 (char 1)"

assert_verdict missing_status_fails \
  '{"risk_class":"LOW_RISK","coverage":{"sufficient":true}}' \
  "malformed:missing_status"

assert_verdict unknown_status_fails \
  '{"status":"PREFLIGHT_STATUS_UNSPECIFIED","risk_class":"LOW_RISK","coverage":{"sufficient":true}}' \
  "malformed:unknown_status:PREFLIGHT_STATUS_UNSPECIFIED"

# The two real healthy shapes, both scoped and sufficient, must pass.
assert_verdict real_ok_with_anchors_passes \
  '{"status":"PREFLIGHT_STATUS_OK","risk_class":"ARCHITECTURE_SENSITIVE","coverage":{"file_count":1,"indexed_file_count":1,"sufficient":true,"note":"12 direct anchor(s) matched"}}' \
  "ok"

assert_verdict real_empty_but_sufficient_passes \
  '{"status":"PREFLIGHT_STATUS_EMPTY","risk_class":"LOW_RISK","coverage":{"file_count":1,"indexed_file_count":1,"sufficient":true,"note":"no anchors, coverage sufficient"}}' \
  "ok"

if ((failures > 0)); then
  echo "$failures preflight_verdict regression(s) failed" >&2
  exit 1
fi
echo "all preflight_verdict regressions passed"

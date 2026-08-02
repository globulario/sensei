#!/usr/bin/env bash
# preflight-verdict.sh — classify one `sensei preflight --json` response.
#
# sensei preflight exits 0 whenever the RPC itself succeeds, even when the
# response body reports PREFLIGHT_STATUS_DEGRADED, UNKNOWN_IMPACT, or
# insufficient coverage (the server's own contract: a degraded response is
# explicit, not a process failure, so callers must read the payload rather
# than the exit code). A caller that only checks the exit code cannot tell
# "this file has no governed knowledge" (an honest empty/degraded finding)
# from "this call never actually verified anything" (an unset or mismatched
# domain, a malformed response). preflight_verdict reads the payload so the
# distinction survives past this one process boundary.
#
# Usage: preflight_verdict <path-to-json-response>
# Prints one of: ok | degraded:<reason> | malformed:<reason>
# Exit code is always 0 -- this is a pure classifier; the caller decides
# whether "degraded"/"malformed" should fail its own mode.

preflight_verdict() {
  local json_path=$1
  python3 - "$json_path" <<'PY'
import json, sys

path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)
except Exception as exc:
    print(f"malformed:parse_error:{exc}")
    raise SystemExit(0)

if not isinstance(data, dict):
    print("malformed:not_an_object")
    raise SystemExit(0)

status = data.get("status")
risk_class = data.get("risk_class")
coverage = data.get("coverage")

if status is None:
    print("malformed:missing_status")
    raise SystemExit(0)
if status == "PREFLIGHT_STATUS_DEGRADED":
    print("degraded:status_degraded")
    raise SystemExit(0)
if status not in ("PREFLIGHT_STATUS_OK", "PREFLIGHT_STATUS_EMPTY"):
    print(f"malformed:unknown_status:{status}")
    raise SystemExit(0)
if risk_class == "UNKNOWN_IMPACT":
    print("degraded:unknown_impact")
    raise SystemExit(0)
if not isinstance(coverage, dict):
    print("malformed:missing_coverage")
    raise SystemExit(0)
if coverage.get("sufficient") is not True:
    print("degraded:insufficient_coverage")
    raise SystemExit(0)

print("ok")
PY
}

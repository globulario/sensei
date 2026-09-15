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
# Prints one of: ok | degraded:<reason> | refused:<kind> | malformed:<reason>
#   refused:* is a typed authority refusal -- the command declined to answer because the
#   served graph is not the generation the registry declares ACTIVE. It is a real finding,
#   not a broken response, and must not be classified malformed.
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

# A SERVED-GENERATION REFUSAL IS NOT A MALFORMED RESPONSE.
#
# preflight refuses, with a nonzero exit, when the graph that answered is not the generation
# the registry declares ACTIVE for the governed domain. That refusal is a typed payload on
# stdout precisely so this classifier can name it: without this branch the caller sees an
# authority refusal as "malformed", which is the distinction this file exists to preserve --
# "this call never actually verified anything" must not be reported as a broken response.
#
# The two kinds are kept apart because the remedy differs: declare the ACTIVE generation, or
# reconcile two claims that disagree.
refusal = data.get("refusal")
if isinstance(refusal, dict):
    kind = refusal.get("kind")
    if kind == "served_generation_not_established":
        print("refused:served_generation_not_established")
        raise SystemExit(0)
    if kind == "served_generation_mismatch":
        print("refused:served_generation_mismatch")
        raise SystemExit(0)
    print(f"malformed:unknown_refusal_kind:{kind}")
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

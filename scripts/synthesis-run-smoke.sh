#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# A real CLI smoke for `sensei synthesis-run`, required by issue #149's proof
# matrix.
#
# Everything below runs against a REAL graph server over a REAL store, because
# the thing under test is the command's behaviour at its own boundary. Identity
# composition goes through a live Metadata RPC and degrades to a blocking
# limitation when the server is absent, so a run without one never reaches the
# governed driver at all -- it stops at graph-identity-unusable and would prove
# nothing about the worlds this smoke exists to cover.
#
# ISOLATION IS THE POINT. Every port, data directory, marker file and proof set
# is scratch, chosen so a developer machine running the production deployment on
# :7878/:10120 is untouched. The teardown kills by PID recovered from the
# listening socket, never by pattern: `pkill -f` on this repository's own
# process names matches the live daemon.
set -euo pipefail

OXI_PORT="${OXI_PORT:-7981}"
GRPC_PORT="${GRPC_PORT:-10131}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
FAILURES=0

log()  { printf '\n=== %s ===\n' "$*"; }
pass() { printf '  ok   %s\n' "$*"; }
fail() { printf '  FAIL %s\n' "$*"; FAILURES=$((FAILURES + 1)); }

port_pid() { ss -ltnp 2>/dev/null | grep ":$1 " | grep -oP 'pid=\K[0-9]+' | head -1; }

teardown() {
  local rc=$?
  # A smoke that reports success while its assertions failed is worse than no
  # smoke: CI reads the exit status, not the transcript. FAILURES wins over the
  # incidental status of whatever ran last.
  if [ "${FAILURES:-0}" -gt 0 ]; then rc=1; fi
  log "teardown"
  if [ -n "${TREE:-}" ]; then
    git -C "$REPO_ROOT" worktree remove --force "$TREE" 2>/dev/null && echo "  removed scratch worktree"
  fi
  for p in "$GRPC_PORT" "$OXI_PORT"; do
    local pid; pid="$(port_pid "$p" || true)"
    if [ -n "${pid:-}" ]; then kill "$pid" 2>/dev/null || true; echo "  stopped :$p (pid $pid)"; fi
  done
  sleep 1
  [ -n "${PROOF_DIR:-}" ] && rm -rf "$PROOF_DIR" && echo "  removed scratch proof set"
  rm -rf "$WORK"
  # A live deployment on the default ports must have survived untouched.
  if ss -ltn 2>/dev/null | grep -q ':10120'; then echo "  live :10120 still up (untouched)"; fi
  exit $rc
}
trap teardown EXIT

refuse_default_ports() {
  if [ "$OXI_PORT" = "7878" ] || [ "$GRPC_PORT" = "10120" ]; then
    echo "refusing to run against the default production ports" >&2
    exit 2
  fi
}

main() {
  refuse_default_ports
  cd "$REPO_ROOT"

  log "build binaries"
  go build -o "$WORK/sensei" ./cmd/awg
  make service-build >/dev/null 2>&1 || go build -o bin/awareness-graph ./golang/server
  pass "sensei + awareness-graph"

  # An ISOLATED WORKTREE, not this checkout. prepare-change writes
  # .sensei/tasks/ and rewrites active.yaml, and a developer running this
  # smoke would otherwise lose the task pointer they were working against.
  # .sensei/ is gitignored, so a fresh worktree starts without one and the
  # smoke supplies exactly the configuration it needs.
  log "isolated worktree"
  TREE="$WORK/tree"
  git -C "$REPO_ROOT" worktree add --detach "$TREE" HEAD >/dev/null 2>&1
  mkdir -p "$TREE/.sensei"
  cat >"$TREE/.sensei/config.yaml" <<CFG
sources:
    - docs/awareness
repository:
    domain: github.com/globulario/sensei
CFG
  pass "worktree at $(git -C "$TREE" rev-parse --short HEAD)"

  log "isolated store on :$OXI_PORT"
  ./bin/oxigraph serve --location "$WORK/oxi" --bind "127.0.0.1:$OXI_PORT" >"$WORK/oxi.log" 2>&1 &
  for _ in $(seq 1 20); do
    curl -sf -o /dev/null "http://127.0.0.1:$OXI_PORT/query?query=SELECT%20*%20WHERE%7B%3Fs%20%3Fp%20%3Fo%7D%20LIMIT%201" && break
    sleep 0.5
  done
  pass "oxigraph healthy"

  log "publish this repository's domain"
  (cd "$TREE" && "$WORK/sensei" build --repo github.com/globulario/sensei \
    --store-url "http://127.0.0.1:$OXI_PORT/store?default" \
    --graph-marker-file "$WORK/marker.json" \
    --input docs/awareness --output "$WORK/graph.nt" >"$WORK/compile.log" 2>&1)
  (cd "$TREE" && "$WORK/sensei" build --repo github.com/globulario/sensei \
    --store-url "http://127.0.0.1:$OXI_PORT/store?default" \
    --graph-marker-file "$WORK/marker.json" \
    --input docs/awareness >"$WORK/build.log" 2>&1)
  grep -E 'closure: PROVEN' "$WORK/build.log" >/dev/null && pass "closure proven" || fail "closure not proven"
  PROOF_DIR="$(grep -oP 'proof set: \K\S+' "$WORK/build.log" | head -1)"

  # Claims bind to the exact repository revision AND graph snapshot, so they
  # must be derived from this worktree against the graph just compiled from it.
  # Copying the checkout's claims fails honestly: "architecture claims binding
  # does not match the repository revision and graph snapshot".
  #
  # CACHED, because deriving them takes ~9 minutes on a repository this size and
  # a smoke nobody will wait for is a smoke nobody runs. The cache key is the
  # revision plus the graph-snapshot digest -- the exact pair the binding is
  # made of -- so a stale entry cannot be silently reused: change either and the
  # key changes. infer-claims is offline and deterministic, so a cache hit and a
  # fresh derivation are the same document.
  log "claims for this exact revision + graph"
  GRAPH_DIGEST="$(sha256sum "$WORK/graph.nt" | cut -d" " -f1)"
  REV="$(git -C "$TREE" rev-parse HEAD)"
  CACHE_DIR="${SENSEI_SMOKE_CACHE:-${TMPDIR:-/tmp}/sensei-smoke-claims}"
  CACHE_KEY="$(printf '%s\n%s\n' "$REV" "$GRAPH_DIGEST" | sha256sum | cut -d" " -f1)"
  CACHED="$CACHE_DIR/$CACHE_KEY.yaml"
  mkdir -p "$TREE/.sensei/project" "$CACHE_DIR"
  if [ -s "$CACHED" ] && [ "${SMOKE_REFRESH_CLAIMS:-0}" != "1" ]; then
    cp "$CACHED" "$TREE/.sensei/project/claims.yaml"
    pass "claims from cache (${CACHE_KEY:0:12})"
  else
    echo "  deriving claims (offline, several minutes; cached for later runs)..."
    if (cd "$TREE" && "$WORK/sensei" infer-claims --repo "$TREE" \
      --repo-domain github.com/globulario/sensei \
      --graph-nt "$WORK/graph.nt" \
      --graph-digest "$GRAPH_DIGEST" --graph-digest-status resolved \
      --output "$TREE/.sensei/project/claims.yaml" >"$WORK/infer.log" 2>&1); then
      cp "$TREE/.sensei/project/claims.yaml" "$CACHED"
      pass "claims derived and cached (${CACHE_KEY:0:12})"
    else
      fail "infer-claims failed"; tail -4 "$WORK/infer.log"
    fi
  fi

  log "graph server on :$GRPC_PORT"
  "$WORK/sensei" serve --no-oxigraph --oxigraph-bind "127.0.0.1:$OXI_PORT" --no-seed \
    --addr ":$GRPC_PORT" --graph-marker-file "$WORK/marker.json" \
    --home-domain github.com/globulario/sensei >"$WORK/serve.log" 2>&1 &
  for _ in $(seq 1 30); do ss -ltn 2>/dev/null | grep -q ":$GRPC_PORT" && break; sleep 0.5; done
  sleep 2
  "$WORK/sensei" preflight --addr "localhost:$GRPC_PORT" --file docs/awareness/invariants.yaml \
    --domain github.com/globulario/sensei 2>&1 | grep -q 'authoritative' \
    && pass "server authoritative" || fail "server did not report authority"

  log "prepare a real task checkpoint"
  (cd "$TREE" && "$WORK/sensei" prepare-change \
    --repo-domain github.com/globulario/sensei \
    --description "smoke: exercise synthesis-run degraded worlds" \
    --mode inspect --task-class bugfix --risk-class low_risk \
    --direction preserve --graph-nt "$WORK/graph.nt" \
    --file read:docs/awareness/invariants.yaml \
    >"$WORK/prepare.log" 2>&1) && pass "task prepared" || { fail "prepare-change failed"; tail -3 "$WORK/prepare.log"; }
  TASK_DIR="$(ls -d "$TREE"/.sensei/tasks/task.* 2>/dev/null | head -1)"
  [ -n "$TASK_DIR" ] && pass "task dir $(basename "$TASK_DIR")" || fail "no task directory was created"

  # prepare-change runs at most ONE convergence iteration, so a task can land
  # unconverged with its closure unresolved. synthesis-run then stops at
  # closure-digest-unavailable and every world below it goes untested -- which
  # is exactly how an earlier version of this smoke reported two worlds as
  # passing while the run never reached the provider at all.
  if [ -n "$TASK_DIR" ]; then
    (cd "$TREE" && "$WORK/sensei" advance-task --repo "$TREE" --task "$TASK_DIR" \
      >"$WORK/advance.log" 2>&1) && pass "task advanced" || {
        fail "advance-task failed"; tail -3 "$WORK/advance.log"; }
  fi


  # ---- the degraded worlds, through the real CLI -------------------------
  #
  # Each world is asserted by EXIT CODE, because that is the contract issue
  # #149 requires: a caller must distinguish these without parsing prose. A
  # world that merely "fails" proves nothing here.
  log "degraded worlds"

  # An interpretation must DISCLOSE the closure report's required surface, or
  # its closure receipt stays advisory and the run stops at step 1 with
  # blockers=[completeness:incomplete] -- never reaching the provider the worlds
  # below are about. completenessForReferences derives the required set from the
  # task's own closure report and compares it against source_references, so an
  # empty list can never be complete when the task has any file scope.
  INTERP="$WORK/interpretation.json"
  SCOPE_FILE="docs/awareness/invariants.yaml"
  SCOPE_DIGEST="$(sha256sum "$TREE/$SCOPE_FILE" | cut -d' ' -f1)"
  cat >"$INTERP" <<JSON
{
  "objective": "smoke: exercise synthesis-run degraded worlds",
  "applicable_intent": [],
  "binding_invariants": [],
  "relevant_contracts": [],
  "authority_boundaries": [],
  "known_failure_modes": [],
  "forbidden_fixes": [],
  "required_proof_obligations": [],
  "assumptions": [],
  "unresolved_questions": [],
  "source_references": [
    {"reference": "$SCOPE_FILE", "source_digest_sha256": "$SCOPE_DIGEST"}
  ],
  "limitations": []
}
JSON

  run_synth() { # run_synth <agent-command> [extra args...]
    local cmd="$1"; shift
    (cd "$TREE" && "$WORK/sensei" synthesis-run \
      --repo "$TREE" --task "$TASK_DIR" --addr "localhost:$GRPC_PORT" \
      --interpretation "$INTERP" --agent codex --agent-command "$cmd" \
      --force-unconverged \
      "$@" >"$WORK/run.log" 2>&1); echo $?
  }

  expect_exit() { # expect_exit <label> <want> <got>
    if [ "$3" = "$2" ]; then pass "$1 -> exit $3"
    else fail "$1 -> exit $3, want $2"; sed -n '1,6p' "$WORK/run.log" | sed 's/^/       /'; fi
  }

  # A provider binary that is not there. Distinct from "the model refused" and
  # from "the task is not ready", and it is the operator's own environment to
  # repair, so it must not share their codes.
  expect_exit "cognitive provider absent" 15 "$(run_synth /nonexistent/definitely/not/here)"

  # Output that is not a vendor envelope at all. Must fail closed rather than
  # be coerced into a candidate.
  BAD="$WORK/agent-garbage.sh"
  printf '#!/bin/sh\nprintf "not json at all\\n"\nexit 0\n' >"$BAD"; chmod +x "$BAD"
  # "non-zero" is not an assertion. An earlier version of this check passed
  # while the run exited 13 (closure-digest-unavailable) having never reached
  # the provider at all -- it would have passed for any failure whatsoever.
  # A governed stop code (3-6) is the proof the driver actually ran.
  code="$(run_synth "$BAD")"
  case "$code" in
    3|4|5|6) pass "malformed provider output -> governed stop $code" ;;
    0) fail "malformed provider output produced a candidate (exit 0)" ;;
    *) fail "malformed provider output -> exit $code; the run never reached the provider" ;;
  esac

  # A provider that never returns. The deadline is the caller's, and exhausting
  # it must be a governed stop, never a silent success.
  HANG="$WORK/agent-hang.sh"
  printf '#!/bin/sh\nsleep 600\n' >"$HANG"; chmod +x "$HANG"
  code="$(run_synth "$HANG" --deadline-minutes 1)"
  case "$code" in
    3|4|5|6) pass "provider timeout -> governed stop $code" ;;
    0) fail "a provider that never returned reported success" ;;
    *) fail "provider timeout -> exit $code; the run never reached the provider" ;;
  esac

  # HARD LAW 7: no run without --apply may leave an application behind.
  if find "$TREE" -name '*application*receipt*' -newer "$INTERP" 2>/dev/null | grep -q .; then
    fail "a default (no --apply) run wrote an application receipt"
  else
    pass "no application receipt without --apply"
  fi

  # HARD LAW 7: and no run may commit, push, or move the branch.
  if [ "$(git -C "$TREE" rev-parse HEAD)" = "$REV" ]; then
    pass "no commit created by any run"
  else
    fail "a run moved HEAD from $REV to $(git -C "$TREE" rev-parse HEAD)"
  fi

  echo
  echo "environment ready: store :$OXI_PORT, server :$GRPC_PORT, work $WORK"
  echo "failures: $FAILURES"
  [ "$FAILURES" -eq 0 ]
}

main "$@"

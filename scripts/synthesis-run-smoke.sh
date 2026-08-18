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
  for w in "${WRONG_BASE_TREE:-}" "${APPLY_TREE:-}" "${TREE:-}"; do
    [ -n "$w" ] || continue
    if [ "${SMOKE_KEEP:-0}" = 1 ]; then
      echo "  kept worktree $w (remove with: git worktree remove --force $w)"
      continue
    fi
    git -C "$REPO_ROOT" worktree remove --force "$w" 2>/dev/null && echo "  removed scratch worktree $(basename "$w")"
  done
  for p in "$GRPC_PORT" "$OXI_PORT"; do
    local pid; pid="$(port_pid "$p" || true)"
    if [ -n "${pid:-}" ]; then kill "$pid" 2>/dev/null || true; echo "  stopped :$p (pid $pid)"; fi
  done
  sleep 1
  [ -n "${PROOF_DIR:-}" ] && rm -rf "$PROOF_DIR" && echo "  removed scratch proof set"
  # SMOKE_KEEP leaves the run's artifacts -- task directory, receipts, logs --
  # for inspection. A ten-minute smoke that deletes the only evidence of how it
  # failed makes every diagnosis a second ten-minute run.
  if [ "${SMOKE_KEEP:-0}" = 1 ]; then echo "  kept $WORK (SMOKE_KEEP=1)"; else rm -rf "$WORK"; fi
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
  # The O4 gate policy is supplied BY THE CALLER, at a path outside the
  # candidate surface -- deliberately, so a candidate cannot author the policy
  # that judges it. It is not written into the worktree's .sensei/, because
  # the unconfigured repository is a world this smoke tests rather than one it
  # quietly repairs (see "absent gate policy" below).
  cat >"$WORK/gate-policy.yaml" <<'POLICY'
default: inherit
rules: {}
POLICY
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

  # The scope file is a MODIFY scope, and every part of that choice is
  # load-bearing -- each of these was a dead end this smoke actually hit:
  #
  #  - an INSPECT task can never reach admission's mutation capability;
  #  - a file the graph does not represent is refused (ReasonScopeUnrepresented)
  #    before any candidate is considered;
  #  - a scope whose resolved nodes carry NO required test leaves closure open
  #    on closure.agent.required_test_unidentified, which makes every admission
  #    decision uncertifiable and puts the apply worlds out of reach. Plain
  #    documentation files (README.md, docs/design/*.md) are in the graph and
  #    still resolve no required test;
  #  - the governed corpus files DO carry required tests -- 114 test-requiring
  #    nodes are authored in docs/awareness/invariants.yaml alone -- but a task
  #    that mutates one collects four CRITICAL
  #    closure.authority.applicable_records_contradict blockers, because several
  #    authority records disagree about who may change the corpus.
  #
  # A governed code file resolves structural nodes bound to the invariants that
  # constrain it, converges CLOSED in one iteration, and admits mutation.
  #
  # Likewise the direction requirement: `preserve` demands a current intended
  # basis (closure.direction.intended_missing) that a synthetic task has no
  # honest way to supply, while `not_applicable` is exactly right for a task
  # whose whole content is "write this one file", and is permitted at low risk.
  SCOPE_FILE="golang/architecture/synthesis/transition.go"

  log "prepare a real task checkpoint"
  (cd "$TREE" && "$WORK/sensei" prepare-change \
    --repo-domain github.com/globulario/sensei \
    --description "smoke: exercise synthesis-run degraded worlds" \
    --mode modify --task-class bugfix --risk-class low_risk \
    --direction not_applicable --graph-nt "$WORK/graph.nt" \
    --file "modify:$SCOPE_FILE" \
    >"$WORK/prepare.log" 2>&1) && pass "task prepared" || { fail "prepare-change failed"; tail -3 "$WORK/prepare.log"; }
  TASK_DIR="$(ls -d "$TREE"/.sensei/tasks/task.* 2>/dev/null | head -1)"
  [ -n "$TASK_DIR" ] && pass "task dir $(basename "$TASK_DIR")" || fail "no task directory was created"

  # CONVERGENCE, and only as much of it as the task actually needs.
  #
  # prepare-change runs at most ONE convergence iteration, so a task can land
  # unconverged with its closure unresolved; synthesis-run then stops at
  # closure-digest-unavailable and every world below it goes untested -- which is
  # exactly how an earlier version of this smoke reported worlds as passing while
  # the run never reached the provider at all.
  #
  # But advancing a task that has ALREADY converged is not free: advance-task
  # publishes a new generation whose admission request binds a different
  # iteration, and every derived request then decides as
  # admission.session.stale_iteration -> uncertifiable. That cost the apply
  # worlds below, and it looked exactly like "admission refused the candidate"
  # rather than "the smoke advanced a task that did not need advancing".
  #
  # So: read the state, advance only if it is open, and assert the result
  # instead of forcing past it.
  conv_status() {
    "$WORK/sensei" convergence-status --session "$1/convergence/session.yaml" --format json 2>/dev/null |
      python3 -c 'import json,sys; print(json.load(sys.stdin)["architecture_convergence_status"]["status"])' 2>/dev/null || true
  }
  FORCE=()
  if [ -n "$TASK_DIR" ]; then
    STATUS="$(conv_status "$TASK_DIR")"
    if [ "$STATUS" != "closed" ]; then
      (cd "$TREE" && "$WORK/sensei" advance-task --repo "$TREE" --task "$TASK_DIR" \
        >"$WORK/advance.log" 2>&1) || { fail "advance-task failed"; tail -3 "$WORK/advance.log"; }
      STATUS="$(conv_status "$TASK_DIR")"
    fi
    if [ "$STATUS" = "closed" ]; then
      pass "convergence closed without forcing"
    else
      # Not a smoke failure by itself -- but every world downstream of admission
      # is then decided against an open session, so say so once, loudly, rather
      # than let the apply rows look like admission's verdict on the candidate.
      fail "convergence is '$STATUS', not closed; admission below cannot admit mutation"
      FORCE=(--force-unconverged)
    fi
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

  # GATE_POLICY empty means "run as an unconfigured repository would", which is
  # its own world rather than a broken invocation.
  GATE_POLICY="$WORK/gate-policy.yaml"
  run_synth() { # run_synth <agent-command> [extra args...]
    local cmd="$1"; shift
    local policy=()
    [ -n "${GATE_POLICY:-}" ] && policy=(--gate-policy "$GATE_POLICY")
    (cd "$TREE" && "$WORK/sensei" synthesis-run \
      --repo "$TREE" --task "$TASK_DIR" --addr "localhost:$GRPC_PORT" \
      --interpretation "$INTERP" --agent codex --agent-command "$cmd" \
      "${FORCE[@]}" "${policy[@]}" \
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

  # The HAPPY PATH, and the precondition for every apply-phase world below it:
  # a provider that returns a VALID mutation plan should seal a candidate.
  #
  # The plan is generated by scripts/smokeplan, which calls the real
  # MutationPlanDigest and schema rather than reimplementing a semantic digest
  # in shell. A hand-rolled fixture would fail inside the run for reasons
  # unrelated to what is being tested -- it already caught its own first draft
  # setting mode "regular" on a write, where the schema pins mode to "".
  go build -o "$WORK/smokeplan" ./scripts/smokeplan
  # The candidate restates the scope file with one line appended. It must be a
  # MODIFY of a tracked file: admission supports modify and nothing else, so a
  # candidate that creates a file is refused by synthesis-admit before any
  # decision exists to test.
  CANDIDATE_CONTENT="$WORK/candidate-content"
  cp "$TREE/$SCOPE_FILE" "$CANDIDATE_CONTENT"
  printf '\n// written by the synthesis-run smoke\n' >>"$CANDIDATE_CONTENT"

  # ONE agent, TWO contracts. The pipeline calls it for O8/O2 PLANNING first and
  # O3 GENERATION second, and each expects a different schema. The stub branches
  # on the prompt it is actually handed -- the planning prompt carries the plan
  # proposal schema, the generation prompt carries GENERATION_PROMPT_JSON -- and
  # not on a call counter, which would answer confidently while being blind to
  # what was asked. Anything else is recorded and refused rather than guessed.
  GOOD="$WORK/agent-valid.sh"
  cat >"$GOOD" <<AGENT
#!/bin/sh
prompt="\$(cat)"
case "\$prompt" in
  *GENERATION_PROMPT_JSON*)
    echo o3-generation >>"$WORK/agent-contracts"
    exec "$WORK/smokeplan" --path "$SCOPE_FILE" --content-file "$CANDIDATE_CONTENT" --profile codex ;;
  *PROPOSAL_JSON_SCHEMA*)
    echo o8-planning >>"$WORK/agent-contracts"
    exec "$WORK/smokeplan" --path "$SCOPE_FILE" --kind plan-proposal --profile codex ;;
esac
echo unrecognised-contract >>"$WORK/agent-contracts"
exit 90
AGENT
  chmod +x "$GOOD"
  rm -f "$WORK/agent-contracts"
  code="$(run_synth "$GOOD")"
  case "$code" in
    0) pass "valid provider output -> candidate-ready (exit 0)"
       CANDIDATE_READY=1 ;;
    *) fail "valid provider output -> exit $code; no candidate was sealed"
       sed -n '1,12p' "$WORK/run.log" | sed 's/^/       /' ;;
  esac

  # Both contracts must actually have been requested. A run that only ever asked
  # for one of them and still succeeded would mean a stage was skipped, and the
  # exit code alone cannot tell the difference.
  if grep -q '^o8-planning$' "$WORK/agent-contracts" 2>/dev/null &&
     grep -q '^o3-generation$' "$WORK/agent-contracts" 2>/dev/null; then
    pass "both provider contracts requested ($(tr '\n' ' ' <"$WORK/agent-contracts"))"
  else
    fail "the run did not request both contracts: $(tr '\n' ' ' <"$WORK/agent-contracts" 2>/dev/null)"
  fi
  if grep -q '^unrecognised-contract$' "$WORK/agent-contracts" 2>/dev/null; then
    fail "the provider was handed a prompt matching neither the planning nor the generation contract"
  fi

  # An UNCONFIGURED repository, which is the ordinary case: .sensei/ is
  # gitignored, so a fresh checkout has no gate policy at all. `sensei gate`
  # reads that same absence as "inherit everything" and proceeds; the O4
  # required evaluator reads it as construction failure and the run ends as
  # required-evaluator-unavailable. Fail-closed is the right call for a governed
  # run -- but the two surfaces do not mean the same thing by an absent policy,
  # and the disposition names the evaluator rather than the missing policy.
  # Asserted here so the divergence is measured rather than remembered.
  code="$(GATE_POLICY="" run_synth "$GOOD")"
  case "$code" in
    3) if grep -q 'required-evaluator-unavailable' "$WORK/run.log"; then
         pass "absent gate policy -> required-evaluator-unavailable (exit 3)"
       else
         fail "absent gate policy -> exit 3, but not as an evaluator world"
       fi ;;
    0) fail "absent gate policy still produced a candidate-ready run" ;;
    *) fail "absent gate policy -> exit $code, want 3" ;;
  esac
  # Re-run the happy path so the lineage below belongs to a fully evaluated
  # candidate, not to the unconfigured run just made.
  rm -f "$WORK/agent-contracts"
  code="$(run_synth "$GOOD")"
  [ "$code" = 0 ] || fail "the happy path stopped reproducing (exit $code)"

  # HARD LAW 7: no run without --apply may leave an application behind.
  if find "$TREE" -name '*application*receipt*' -newer "$INTERP" 2>/dev/null | grep -q .; then
    fail "a default (no --apply) run wrote an application receipt"
  else
    pass "no application receipt without --apply"
  fi

  # ---- composed is not decided: the admission chain through the CLI ------
  #
  # Issue #149 names this gap exactly: "composed != decided -- no real
  # admit-change evaluation of a derived request has been run end to end". Both
  # halves were proven against fixtures. What had never happened is a request
  # DERIVED from a sealed candidate being carried into a real evaluation, and
  # the resulting decision being carried into a real application.
  #
  # The scope of a derived request comes from diffing the candidate's sealed
  # manifest against the repository tree at its own base revision -- not from
  # the task's declared scope. A fixture cannot show that those two agree on a
  # real repository; only a real run can.
  log "admission chain"
  DECISION="$WORK/decision.yaml"
  if [ "${CANDIDATE_READY:-0}" != 1 ]; then
    fail "no sealed candidate, so the admission chain below could not run"
  else
    LINEAGE="$(grep -oP 'admission lineage: \K\S+' "$WORK/run.log" | head -1)"
    if [ -z "${LINEAGE:-}" ] || [ ! -s "${LINEAGE:-}" ]; then
      fail "the run sealed a candidate but reported no admission lineage bundle"
    else
      pass "lineage bundle $(basename "$LINEAGE")"

      # The continuity chain starts at the digest the RUN reported, read from
      # the run's own output rather than from the bundle, so the bundle is
      # checked against something and not against itself.
      RUN_CANDIDATE="$(grep -oP '^candidate:\s+\K\S+' "$WORK/run.log" | head -1)"
      LINEAGE_CANDIDATE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["candidate_artifact_digest_sha256"])' "$LINEAGE" 2>/dev/null || true)"
      if [ -n "$RUN_CANDIDATE" ] && [ "$RUN_CANDIDATE" = "$LINEAGE_CANDIDATE" ]; then
        pass "run and lineage name the same candidate (${RUN_CANDIDATE:0:12})"
      else
        fail "run candidate '$RUN_CANDIDATE' != lineage candidate '$LINEAGE_CANDIDATE'"
      fi

      # O5A. Exit 3 (unsupported operation) and 4 (changes nothing) are real
      # governed answers, but neither is the one this fixture asks for, and
      # both would leave the rest of the chain untested -- so they are failures
      # OF THE SMOKE, reported as such rather than passed over.
      set +e
      (cd "$TREE" && "$WORK/sensei" synthesis-admit --repo "$TREE" --task "$TASK_DIR" \
        --lineage "$LINEAGE" --format json >"$WORK/admit.json" 2>"$WORK/admit.err")
      code=$?
      set -e
      case "$code" in
        0) pass "synthesis-admit -> admission request composed" ;;
        3) fail "synthesis-admit refused an unsupported operation; the fixture must modify, not add" ;;
        4) fail "synthesis-admit found nothing to admit; the candidate did not change the scope file" ;;
        *) fail "synthesis-admit -> exit $code"; sed -n '1,6p' "$WORK/admit.err" | sed 's/^/       /' ;;
      esac

      REQUEST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("admission_request_path",""))' "$WORK/admit.json" 2>/dev/null || true)"
      if [ -z "${REQUEST:-}" ] || [ ! -s "${REQUEST:-}" ]; then
        fail "no derived admission request was written"
      else
        # The derived scope must be the file the candidate actually changed.
        # A request that composed cleanly while naming the wrong file would be
        # the worst possible outcome here: a decision about something else.
        if python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if d.get("modified_files")==[sys.argv[2]] else 1)' \
             "$WORK/admit.json" "$SCOPE_FILE"; then
          pass "derived scope is exactly [$SCOPE_FILE]"
        else
          fail "derived scope is $(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("modified_files"))' "$WORK/admit.json"), want [$SCOPE_FILE]"
        fi

        # The decision itself. Evaluated against the task's own convergence
        # bundle and graph snapshot -- the same inputs advance-task uses, so a
        # difference in the outcome can only come from the derived request.
        set +e
        (cd "$TREE" && "$WORK/sensei" admit-change --bundle "$TASK_DIR/convergence" \
          --request "$REQUEST" --graph-nt "$TASK_DIR/source/graph.nt" --repo "$TREE" \
          --output "$DECISION" --format json >"$WORK/decision.json" 2>"$WORK/decision.err")
        code=$?
        set -e
        MUTATION=""
        if [ -s "$DECISION" ]; then
          MUTATION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["architecture_admission_decision"]["mutation_capability"])' "$WORK/decision.json" 2>/dev/null || true)"
          VERDICT="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["architecture_admission_decision"]["decision"])' "$WORK/decision.json" 2>/dev/null || true)"
          pass "admit-change decided a DERIVED request: ${VERDICT:-?} / mutation=${MUTATION:-?}"

          # Digest continuity, not merely a zero exit. The decision must be
          # bound to the request synthesis-admit composed -- a decision about
          # some other request would exit 0 just as happily.
          ADMIT_CANDIDATE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["candidate_artifact_digest_sha256"])' "$WORK/admit.json" 2>/dev/null || true)"
          # THREE digests, three meanings, and comparing the wrong pair is how a
          # continuity check passes while proving nothing:
          #   request_digest_sha256                    -> the O5A COMPOSITION
          #                                               request's own identity
          #   admission_request_identity_digest_sha256 -> the derived ADMISSION
          #                                               request, which is what
          #                                               a decision binds
          #   candidate_artifact_digest_sha256         -> the sealed candidate
          ADMIT_REQUEST_DIGEST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["request_digest_sha256"])' "$WORK/admit.json" 2>/dev/null || true)"
          ADMIT_IDENTITY_DIGEST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["admission_request_identity_digest_sha256"])' "$WORK/admit.json" 2>/dev/null || true)"
          DECISION_REQUEST_DIGEST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["architecture_admission_decision"]["request_receipt"]["digest_sha256"])' "$WORK/decision.json" 2>/dev/null || true)"
          [ "$ADMIT_CANDIDATE" = "$RUN_CANDIDATE" ] \
            && pass "the composed request carries the run's candidate" \
            || fail "composed request candidate '$ADMIT_CANDIDATE' != run candidate '$RUN_CANDIDATE'"
          if [ -n "$ADMIT_IDENTITY_DIGEST" ] && [ "$ADMIT_IDENTITY_DIGEST" = "$DECISION_REQUEST_DIGEST" ]; then
            pass "the decision is bound to the composed request (${ADMIT_IDENTITY_DIGEST:0:12})"
          else
            fail "composed request identity '$ADMIT_IDENTITY_DIGEST' != decided request digest '$DECISION_REQUEST_DIGEST'"
          fi

          # ONE invocation writes the decision and prints it. Both carry a
          # decision_digest_sha256, and that field is the decision's identity --
          # what an application receipt binds and what a verification quotes. The
          # two disagreed: normalization was not idempotent, so the second
          # marshal of one value produced a different identity. Nothing that
          # compares exit codes could see it.
          STORED_DECISION_DIGEST="$(grep -m1 -oP 'decision_digest_sha256: \K\S+' "$DECISION" || true)"
          PRINTED_DECISION_DIGEST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["architecture_admission_decision"]["decision_digest_sha256"])' "$WORK/decision.json" 2>/dev/null || true)"
          if [ -n "$STORED_DECISION_DIGEST" ] && [ "$STORED_DECISION_DIGEST" = "$PRINTED_DECISION_DIGEST" ]; then
            pass "the stored and printed decision declare one identity (${STORED_DECISION_DIGEST:0:12})"
          else
            fail "stored decision '$STORED_DECISION_DIGEST' != printed decision '$PRINTED_DECISION_DIGEST'"
          fi
        else
          fail "admit-change wrote no decision (exit $code)"; sed -n '1,6p' "$WORK/decision.err" | sed 's/^/       /'
        fi
      fi
    fi
  fi

  # ---- O5B application, through the real CLI -----------------------------
  #
  # Hard laws 5 and 6: the exact admitted artifact, into one dedicated clean
  # worktree pinned to the admitted base revision, and nothing else.
  log "application"
  if [ ! -s "$DECISION" ]; then
    fail "no admission decision, so no application world could be tested"
  else
    APPLY_TREE="$WORK/apply"
    git -C "$REPO_ROOT" worktree add --detach "$APPLY_TREE" "$REV" >/dev/null 2>&1
    apply_to() { # apply_to <target> [extra args...]
      local target="$1"; shift
      (cd "$TREE" && "$WORK/sensei" synthesis-apply --repo "$TREE" --task "$TASK_DIR" \
        --lineage "$LINEAGE" --decision "$DECISION" --target "$target" --format json \
        "$@" >"$WORK/apply.json" 2>"$WORK/apply.err"); echo $?
    }

    case "${MUTATION:-}" in
      admitted|admitted_with_conditions)
        # The target refusals are only reachable once a decision admits: an
        # unadmitted decision is refused first, by design, so these rows cannot
        # be reached at all through a task that has not converged.
        echo "dirty" >"$APPLY_TREE/.smoke-dirt"
        git -C "$APPLY_TREE" add .smoke-dirt >/dev/null 2>&1
        code="$(apply_to "$APPLY_TREE")"
        [ "$code" = 4 ] && pass "dirty target refused (exit 4)" || fail "dirty target -> exit $code, want 4"
        git -C "$APPLY_TREE" reset --hard "$REV" >/dev/null 2>&1
        rm -f "$APPLY_TREE/.smoke-dirt"

        code="$(apply_to "$TREE")"
        [ "$code" = 4 ] && pass "the source checkout refused as target (exit 4)" || fail "source checkout as target -> exit $code, want 4"

        # A clean worktree is not enough: it must sit on the revision the
        # decision admitted, or the applied result is the candidate's content
        # over a tree it was never diffed against.
        WRONG_BASE_TREE="$WORK/wrong-base"
        if git -C "$REPO_ROOT" worktree add --detach "$WRONG_BASE_TREE" "$REV^" >/dev/null 2>&1; then
          code="$(apply_to "$WRONG_BASE_TREE")"
          [ "$code" = 4 ] && pass "a target on the wrong base refused (exit 4)" || fail "wrong base -> exit $code, want 4"
        else
          fail "could not create a worktree at $REV^ to test the wrong-base refusal"
        fi

        code="$(apply_to "$APPLY_TREE")"
        if [ "$code" = 0 ]; then
          pass "admitted candidate applied to a clean dedicated worktree"
          if cmp -s "$APPLY_TREE/$SCOPE_FILE" "$CANDIDATE_CONTENT"; then
            pass "the applied file is exactly the sealed candidate"
          else
            fail "the applied file is not the sealed candidate's content"
          fi
          # The last link: the application receipt must name the same candidate
          # and the same request the decision was made about. Matching bytes on
          # disk is not the same fact -- two candidates can carry identical
          # content and only the digests say which one was admitted.
          # The apply report's own request_digest_sha256 is the O5B APPLY
          # request's identity -- a fourth artifact, not the composition request
          # ($ADMIT_REQUEST_DIGEST) and not the admission request
          # ($ADMIT_IDENTITY_DIGEST). The link that closes the chain lives in the
          # persisted receipt, which names the decision it acted on.
          APPLIED_CANDIDATE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["candidate_artifact_digest_sha256"])' "$WORK/apply.json" 2>/dev/null || true)"
          APPLY_RECEIPT="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["receipt_path"])' "$WORK/apply.json" 2>/dev/null || true)"
          RECEIPT_DECISION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["admission_decision_digest_sha256"])' "$APPLY_RECEIPT" 2>/dev/null || true)"
          RECEIPT_CANDIDATE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["candidate_artifact_digest_sha256"])' "$APPLY_RECEIPT" 2>/dev/null || true)"
          if [ "$APPLIED_CANDIDATE" = "$RUN_CANDIDATE" ] && [ "$RECEIPT_CANDIDATE" = "$RUN_CANDIDATE" ] &&
             [ -n "$RECEIPT_DECISION" ] && [ "$RECEIPT_DECISION" = "$STORED_DECISION_DIGEST" ]; then
            pass "digest continuity run -> request -> decision -> application"
          else
            fail "continuity broken: applied candidate '$APPLIED_CANDIDATE', receipt candidate '$RECEIPT_CANDIDATE', receipt decision '$RECEIPT_DECISION' vs stored '$STORED_DECISION_DIGEST'"
          fi
        else
          fail "apply -> exit $code"; sed -n '1,8p' "$WORK/apply.log" | sed 's/^/       /'
        fi

        # Hard law 6, the half nothing enforced until PR #149's apply work: a
        # second application of a consumed candidate must be refused, not
        # silently redone over a reset worktree.
        git -C "$APPLY_TREE" checkout -- "$SCOPE_FILE" >/dev/null 2>&1
        code="$(apply_to "$APPLY_TREE")"
        [ "$code" = 6 ] && pass "a previously consumed candidate is refused (exit 6)" || fail "re-apply -> exit $code, want 6"
        ;;
      *)
        # A decision that does not admit mutation is a governed answer, not a
        # smoke failure -- and refusing to apply it IS the admission-refusal row
        # of #149's proof matrix. What it cannot do is stand in for the rows
        # that need an admitting decision, so those are named as not covered
        # rather than left to look green.
        code="$(apply_to "$APPLY_TREE")"
        [ "$code" = 3 ] && pass "apply refuses a decision that does not admit mutation (exit 3)" \
          || fail "unadmitted decision -> exit $code, want 3"
        echo "  NOT COVERED (needs a converged task): clean-worktree application,"
        echo "              dirty / wrong-base / consumed target refusals"
        ;;
    esac

    if [ "$(git -C "$APPLY_TREE" rev-parse HEAD)" = "$REV" ]; then
      pass "application moved no branch in the target worktree"
    else
      fail "application moved the target worktree's HEAD"
    fi
  fi

  # ---- drift between the run and everything downstream of it --------------
  #
  # Hard law 10 asks that resume refuse workspace, graph, task, closure and
  # base-revision drift. Issue #149 already states which half exists: the
  # persisted-bundle commands refuse BASE-REVISION drift, and workspace/graph
  # drift is not re-verified "because recomputing workspace identity needs a
  # live Metadata RPC". That claim is testable, and an untested claim about a
  # missing safeguard is exactly the kind that quietly stops being true.
  #
  # The probe: synthesis-run reached the governed driver only because a live
  # server answered Metadata -- without one it stops at graph-identity-unusable
  # and never plans at all. So if synthesis-admit and synthesis-apply still act
  # on the same bundle after that server is gone, they are demonstrably not
  # revalidating the graph identity the candidate was produced under.
  #
  # Run LAST: it takes the graph server down.
  log "graph drift after sealing"
  if [ "${CANDIDATE_READY:-0}" != 1 ] || [ ! -s "${LINEAGE:-/nonexistent}" ]; then
    echo "  skipped: no sealed candidate to revisit"
  else
    pid="$(port_pid "$GRPC_PORT" || true)"
    if [ -n "${pid:-}" ]; then kill "$pid" 2>/dev/null || true; fi
    for _ in $(seq 1 20); do ss -ltn 2>/dev/null | grep -q ":$GRPC_PORT" || break; sleep 0.5; done
    if ss -ltn 2>/dev/null | grep -q ":$GRPC_PORT"; then
      fail "could not stop the graph server, so drift could not be probed"
    else
      pass "graph server stopped"
      # A control: the run itself must NOT proceed without the graph.
      code="$(run_synth "$GOOD")"
      if [ "$code" = 0 ]; then
        fail "synthesis-run produced a candidate with no graph server at all"
      else
        pass "synthesis-run refuses to run without the graph (exit $code)"
      fi
      set +e
      (cd "$TREE" && "$WORK/sensei" synthesis-admit --repo "$TREE" --task "$TASK_DIR" \
        --lineage "$LINEAGE" --format json >"$WORK/admit-drift.json" 2>"$WORK/admit-drift.err")
      drift=$?
      set -e
      if [ "$drift" = 0 ]; then
        echo "  GAP  synthesis-admit composed an admission request with the graph"
        echo "       server gone -- the graph identity the candidate was produced"
        echo "       under is never revalidated (#149 hard law 10, second half)"
      else
        pass "synthesis-admit refused after the graph became unavailable (exit $drift)"
      fi
    fi
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

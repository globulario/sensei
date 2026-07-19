# Meta-Principles Reference

Meta-principles are universal patterns that predict where bugs hide. They are not rules about your specific codebase — they are rules about how software systems fail. Use them to classify incidents and find sibling bugs before they ship.

Every initialized Sensei project gets a generated cold-start pack at
`docs/awareness/meta_principles.yaml` — **137 principles across 8 categories**:

| Category | Question it answers | Count |
|---|---|---|
| **Authority** | who owns this truth, and is this code that owner? | 20 |
| **Signal** | is the truth arriving intact, or degraded / silent / absorbed? | 19 |
| **Lifecycle** | will this operation complete, and what happens if it fails? | 38 |
| **Dependency** | what breaks if a non-critical thing fails? | 7 |
| **Perception** | is the screen telling the truth about the system? | 19 |
| **Composition** | does the layout make truth easy to perceive? | 7 |
| **Structure** | is this unit shaped to be reused, inspected, and outlive its implementation? | 12 |
| **Evolution** | how is this project allowed to change safely over time? | 11 |

This page explains the framework and walks the **Authority / Signal / Lifecycle / Dependency** backend categories in depth. The **Perception / Composition / Structure** categories were added later for GUI truth, visual composition, and code structure; **Evolution** is the newest — the engineering laws of safe project change (releasable trunk, reviewable slices, deterministic builds, observable change, intent-before-drift). In this repository, the canonical authored source for the pack is `docs/awareness/generic/state_authority_invariants.yaml`; the generated pack is the queryable artifact installed into new projects. Query any principle live:

```bash
sensei resolve invariant meta.ui.screen_claim_must_bind_to_authority
```

> The pack is **generated** from one canonical corpus by
> `scripts/sync-principle-pack.py` — never hand-edited. This page describes;
> the pack defines.

---

## How to use them

1. **After an incident**: classify the bug against the principles below. Add `related_invariants: [meta.<id>]` to your failure mode entry.

2. **Search for siblings**: the principle tells you what to look for. If your bug was a fallback hiding a failure (`meta.fallback_must_degrade_semantics`), search your codebase for every fallback path and check if it degrades visibly.

3. **When none fits**: flag the incident as UNCLASSIFIABLE. This is where the next principle is hiding. Don't force-fit — bad classification is worse than none.

---

## Authority (4)

*"Who owns this truth, and is this code that owner?"*

### meta.storage_is_not_semantic_authority

**What it catches:** Wrong actor writes or reads truth.

Storage holds bytes. The service that writes and interprets those bytes is the authority. When a different service reads storage directly and interprets the bytes, it is stealing authority. The interpretation may be correct today and wrong tomorrow — the owning service can change format, add validation, or change semantics without knowing it has a second consumer.

**Example:** Service A writes a JSON blob to a database. Service B reads that blob directly instead of calling Service A's API. Service A adds a new required field. Service B silently operates on incomplete data.

**Search pattern:** Find every place a service reads another service's storage directly (database tables, files, etcd keys) instead of through its API.

---

### meta.identity_computation_must_be_invariant

**What it catches:** Same value means different things in different contexts.

An identity value (a name, an ID, a hash) must have exactly one computation and one meaning. When the same field is computed differently by different writers, readers see values that look identical but mean different things.

**Example:** `build_id` is a UUID in the repository (artifact identity) but an integer in the release pipeline (sequence number). A query that joins on `build_id` silently correlates unrelated records.

**Search pattern:** Find fields that are written by more than one code path. Check if both paths compute the same value for the same input.

---

### meta.competing_writers_must_converge_or_be_fenced

**What it catches:** Two writers with different state fight by timing.

When two actors can write the same state, the outcome depends on which one writes last. Without convergence (both computing the same value) or fencing (only one allowed to write), the system oscillates or silently picks the wrong winner.

**Example:** Two replicas both update a "last seen" timestamp. Depending on network timing, the timestamp jumps backward, triggering false "node offline" alerts.

**Search pattern:** Find every shared-state key/row/file that is written by more than one process. Verify convergence or fencing.

---

### meta.structure_must_not_be_stripped_in_projection

**What it catches:** Scope, subject, or source removed when projecting to a primitive.

A structured value (e.g., `{node: "A", status: "healthy", observed_at: T}`) projected to a primitive (`status: "healthy"`) loses the scope that made it meaningful. The primitive carries the lie of universality — it looks like a global truth but is actually scoped to one node at one time.

**Example:** A health check returns `{healthy: true}` without naming which component or when. The caller caches it and serves it for all components.

**Search pattern:** Find every place a structured response is reduced to a boolean or string. Check if the stripped fields mattered.

---

## Signal (5)

*"Is the truth arriving intact, or degraded / silent / absorbed?"*

### meta.fallback_must_degrade_semantics

**What it catches:** Fallback returns the same shape as truth.

When a primary source fails and a fallback activates, the fallback response must look different from a successful primary response. If the shapes are identical, the caller cannot distinguish "real answer" from "fallback guess." The fallback value gets cached, propagated, and acted on as if it were authoritative.

**Example:** A config service falls back to a hardcoded default when the config store is unavailable. The response looks identical to a real config response. The caller caches the default as authoritative. When the config store recovers, the cached default overrides it.

**Search pattern:** Find every fallback/default path. Check if the response shape or type differs from the primary path.

---

### meta.authority_must_express_uncertainty

**What it catches:** Owner can't say "unknown" — callers fabricate certainty.

When the authoritative source has no answer, it must return a distinct "unknown" or "unavailable" response — not an empty success, not a zero value, not a cached stale value. If the authority can't express uncertainty, callers are forced to invent certainty.

**Example:** A service returns `status: ""` (empty string) when it doesn't know. The caller treats empty as "ok" because the enum doesn't have an "unknown" value.

**Search pattern:** Find every API response type. Check if it has an explicit unknown/unavailable state. Check what callers do with zero values.

---

### meta.absence_scope_must_be_explicit

**What it catches:** "Not found here" treated as "does not exist anywhere."

A lookup that returns "not found" is scoped to the source that was queried. It does not prove the thing doesn't exist — only that this particular source doesn't have it. When absence is treated as universal, the system deletes or skips things that exist elsewhere.

**Example:** A package isn't found in the local cache. The installer treats this as "package doesn't exist" instead of "package isn't cached locally." It skips installation.

**Search pattern:** Find every "not found" / "nil" / "empty" handler. Check if it specifies where it looked.

---

### meta.connection_errors_must_not_be_absorbed

**What it catches:** TLS/auth/connection errors absorbed into generic timeouts or "not found."

A connection error (TLS handshake failure, auth rejection, DNS failure) is fundamentally different from "the service responded with not-found." When connection errors are caught by a generic error handler and converted to "unavailable" or "not found," the diagnostic signal is destroyed. The operator sees "service unavailable" when the real problem is a misconfigured certificate.

**Example:** A gRPC call fails with a TLS certificate error. The error is caught by `if err != nil { return nil, status.Error(codes.Unavailable, "service unavailable") }`. The operator restarts the service repeatedly, not knowing the cert expired.

**Search pattern:** Find every `if err != nil` after a network call. Check if TLS/auth errors are distinguished from other failures.

---

### meta.assertions_must_carry_their_scope

**What it catches:** Assertion aggregated without naming its scope.

A positive assertion ("node X is healthy at time T") and a negative assertion ("node X failed probe at time T") must both carry their scope — which node, which probe, which moment. When assertions are aggregated without scope (e.g., "3 of 5 nodes healthy"), the aggregation hides which nodes are unhealthy and when the last check ran.

**Example:** A cluster health endpoint returns `{healthy_nodes: 4, total: 5}`. The operator can't tell which node is unhealthy or when it was last checked. The unhealthy node might have recovered 10 minutes ago.

**Search pattern:** Find every aggregation (count, percentage, summary). Check if the individual items and their timestamps are accessible.

---

## Lifecycle (7)

*"Will this operation complete, and what happens if it fails?"*

### meta.write_creates_completion_obligation

**What it catches:** Write without cleanup path — permanent stall.

When a system writes a record that represents an in-progress operation (a lock, a pending state, a job record), it creates an obligation to eventually complete or clean up that record. If the writer crashes after writing but before completing, and no other actor knows how to clean up, the record persists forever and blocks progress.

**Example:** A workflow writes `status: PENDING` to a job record, then crashes before updating it to `COMPLETED` or `FAILED`. No other actor monitors for stale PENDING records. The job is stuck forever.

**Search pattern:** Find every write that creates a non-terminal state. Trace the code path to its terminal state. Check what happens if the process crashes between write and completion.

---

### meta.half_done_must_not_look_done

**What it catches:** Intermediate state satisfies completeness checks.

When a multi-step operation writes partial results that look like complete results, downstream consumers treat the partial state as complete. They proceed with incomplete data, and the system diverges.

**Example:** A publish operation writes the artifact file, then writes the manifest, then updates the state to PUBLISHED. If it crashes after writing the file but before the manifest, the file exists but has no manifest. A naive check ("does the file exist?") returns true.

**Search pattern:** Find every multi-step write operation. Check if intermediate states can be mistaken for complete states by any consumer.

---

### meta.silence_is_not_valid_for_unexpected

**What it catches:** Unhandled case is a silent no-op.

When a switch/case or if/else chain encounters an unexpected value and does nothing (no log, no error, no metric), the system silently ignores a condition that the programmer didn't anticipate. The unexpected value propagates, and failures surface far from the cause.

**Example:** A type switch handles `ServiceRelease` and `ApplicationRelease` but not `InfrastructureRelease`. Infrastructure releases are silently skipped. The operator sees "release succeeded" but nothing was deployed.

**Search pattern:** Find every switch/type-switch without a default case. Find every if/else chain that doesn't handle the "none of the above" case.

---

### meta.failure_response_must_contract_not_amplify

**What it catches:** Unbounded retry/re-enqueue turns one failure into a cascade.

When a failure triggers a retry, and the retry triggers the same failure, and nothing limits the retry rate or count, one failure becomes an exponential cascade. The system consumes all resources retrying a permanently failed operation.

**Example:** A message queue consumer fails to process a message. The message is re-enqueued immediately. Processing fails again. The queue fills with copies of the same failing message, starving healthy messages.

**Search pattern:** Find every retry loop and re-enqueue path. Check for backoff, max attempts, and dead-letter handling.

---

### meta.diagnostic_output_must_be_bounded

**What it catches:** One error produces N log lines that fill disk.

When an error condition triggers unbounded logging (e.g., logging on every loop iteration, every request, every heartbeat tick), the diagnostic output itself becomes a failure mode. Disk fills, log aggregators are overwhelmed, and healthy services are affected by the logging load.

**Example:** A connection error is logged on every health check tick (every 5 seconds). The log file grows at 100MB/hour. After 24 hours, disk is full and all services on the node fail.

**Search pattern:** Find every error log inside a loop or periodic callback. Check if the log is deduplicated or rate-limited.

---

### meta.binding_outlives_evidence_until_invalidated

**What it catches:** Decision durable, but the evidence it was bound to has moved.

A decision (a routing rule, a permission grant, a cached lookup) is bound to evidence that was true at decision time. If the evidence changes and the decision is not re-evaluated, the decision authorizes the wrong present.

**Example:** A load balancer routes traffic to node A based on a health check from 5 minutes ago. Node A crashed 4 minutes ago. The routing decision is stale, but nothing invalidated it.

**Search pattern:** Find every cached decision or binding. Check if it has an expiry, a refresh trigger, or an invalidation hook.

---

### meta.state_mutations_must_be_durably_committed_before_side_effects

**What it catches:** Side effect before durable commit — can't replay.

When a state mutation (database write, etcd put) is followed by a side effect (sending a message, starting a process, calling an external API), the side effect must not execute until the mutation is durably committed. If the mutation fails after the side effect, the side effect cannot be undone, and replay will execute it again.

**Example:** A workflow dispatches a node restart (side effect), then writes the workflow step receipt (durable commit). The receipt write fails. On retry, the workflow dispatches the restart again — the node is restarted twice.

**Search pattern:** Find every side effect (external call, message send, process start) and trace backward to the nearest durable write. Check if the write is committed before the side effect executes.

---

## Dependency (2)

*"What breaks if a non-critical thing fails? What if A needs B needs A?"*

### meta.critical_path_no_non_critical_dependency

**What it catches:** Critical path blocked or flooded by non-critical service.

When a critical operation (serving requests, maintaining cluster health, processing heartbeats) depends on a non-critical service (logging, metrics, AI analysis), a failure or overload of the non-critical service can take down the critical path.

**Example:** A heartbeat handler calls the AI analysis service to annotate each heartbeat. The AI service is slow. Heartbeat processing backs up. Nodes are marked offline because heartbeats aren't processed in time.

**Search pattern:** Trace every critical path (request serving, health checks, convergence). List every external call on that path. Check if each call is truly required for the critical operation.

---

### meta.circular_dependency_must_have_break_glass

**What it catches:** Self-deploying system can't deploy fix for itself.

When a system is responsible for deploying itself (a CI/CD pipeline that deploys the CI/CD service, a package manager that installs its own updates), a failure in the system prevents deploying the fix. Without a break-glass path that bypasses the normal deployment, the system is permanently stuck.

**Example:** The deployment service has a bug that causes all deployments to fail. The fix is ready, but deploying it requires the deployment service — which is broken. There is no manual deployment path.

**Search pattern:** Find every self-referential dependency (system deploys itself, system monitors itself, system authenticates itself). Check if a manual bypass exists and is documented.

# Phase 9.6 — governed briefing-feedback leg (opening, design-first)

Phase 9.4 is closed and merged. This opens Phase 9.6 **before** Phase 9.5, because the
briefing-feedback projection is an upstream semantic service the later VS Code cockpit
must consume rather than reinterpret. **Design only — no production code, protobuf,
server wiring, governed YAML, generated artifacts, or tests** beyond a design-presence
check if repository convention requires one. Checkpoint 1 stays locked until this opening
contract is reviewed.

## 1. Repository-grounded problem

Phase 8.1c already built a verified promoted-knowledge path for TASK briefings, in
`golang/architecture/tasksession/briefing_promotion.go`:

- `collectPromotedKnowledge(repoRoot, file, taskFiles, domain)` discovers committed
  promotions via `questionpromotion.DiscoverCommittedPromotions`, independently re-proves
  each through `questionpromotion.VerifyCommittedPromotion` (it does NOT re-implement
  receipt/journal/source/graph/provenance validation), scope-filters via
  `promotionInScope`, and returns `[]PromotedGovernedRecord` + `[]string` findings,
  deterministically sorted;
- `PromotedGovernedRecord` preserves provenance: `GovernedNodeIRI`, `Kind`,
  `CanonicalRecordID`, `SourceDocument`, `PromotionLineageID`, `ReceiptDigestSHA256`,
  `QuestionID`, `AnswerID`, `DispositionReceiptDigestSHA256`, `TaskID`, `SessionID`;
- `tasksession.BuildTaskBriefing` surfaces it as `TaskBriefing.PromotedGovernedKnowledge`.

**The residual gap.** This lives in `tasksession` as UNEXPORTED helpers, its findings are
**untyped strings**, and the server `Briefing` surface (`golang/server/briefing.go`) does
NOT consume it — the server builds from graph impact/patterns/intents/repair-plans/
rendering-groups/generic provenance, with no stable typed feedback-lineage section.

Phase 9.6 closes this loop:

```
architectural question → authorized disposition → committed governed promotion
→ independently verified promotion lineage → scope-relevant future server briefing
```

The result is a **read-only feedback projection**. It is NOT a new knowledge, promotion,
dialogue, certification, or completion owner.

## 2. Objective

Define (and later implement) one canonical, deterministic, read-only briefing-feedback
projection that lets a future briefing explain: which governed knowledge entered the
current scope through an architect-answer promotion; the question/answer lineage that
produced it; the disposition + promotion receipts authorizing that lineage; the
originating task/session/result world; whether relevant promotion evidence was verified /
unavailable / incomplete / contradictory / stale / integrity-invalid; and the exact
limitations preventing its use as binding briefing context.

The **same** canonical projection is consumable by the task briefing, the server
`Briefing` RPC, and the future Phase 9.5 cockpit. **No consumer reimplements verification,
scope, or classification.**

## 3. Canonical owner boundary

New package **`golang/architecture/briefingfeedback`**, sitting BENEATH both `tasksession`
and `golang/server`. The server must not depend on an unexported `tasksession` helper, and
promotion verification must not be duplicated in server code.

The owner **may**: discover candidates through the accepted `questionpromotion` discovery
boundary; use untrusted candidate metadata only for bounded relevance routing; establish
authority ONLY through `questionpromotion.VerifyCommittedPromotion`; apply exact
domain + file-scope intersection; construct + validate a deterministic typed projection;
report typed relevant integrity findings; render no policy of its own.

The owner **must not**: read promotion journals/receipts/governed YAML/graph files to
reproduce rules already owned by `questionpromotion`; trust graph adjacency, a promotion
directory name, receipt path, issuer string, or claimed status as authority; promote /
dispose / certify / complete / recover / mutate; call CLI handlers; depend on GitHub;
introduce a new authority triple.

## 4. Existing-implementation reuse (migration)

The final implementation must NOT leave two semantic implementations of: promotion
discovery, committed-promotion verification, domain matching, file-scope intersection,
provenance projection, or integrity classification.

Migration:

- the current `tasksession.collectPromotedKnowledge` + `promotionInScope` logic moves INTO
  `briefingfeedback` as the single owner (discovery via `questionpromotion`, verification
  via `VerifyCommittedPromotion`, exact scope filtering);
- `tasksession.BuildTaskBriefing` consumes the shared owner result (a thin adapter mapping
  the typed projection's verified records onto the existing `PromotedGovernedRecord` shape,
  OR replacing it — Checkpoint 1 decides, preserving accepted task-briefing behavior);
- untyped string findings become the typed finding vocabulary (§6);
- existing accepted task-briefing behavior (the verified records already surfaced) remains
  compatible.

## 5. Proposed projection contract — `briefing.feedback_projection/v1`

Exact field names follow repository conventions; the projection contains at least:

### Projection identity
- `schema_version` (`briefing.feedback_projection/v1`);
- producer identity + version;
- repository identity / repository-domain identity;
- requested domain;
- requested file scope;
- optional exact task binding (task id + session id) when available;
- deterministic self-excluding `digest_sha256` (clear the field, canonicalize, hash — the
  closureprotocol canonical-JSON discipline);
- `non_authoritative_projection: true`;
- a fixed bound statement.

### Verified promoted records (one per admitted record)
`governed_node_iri`, `governed_kind`, `canonical_record_id`, `source_document`,
`promotion_lineage_id`, `promotion_receipt_digest_sha256`, `question_id`, `answer_id`,
`disposition_receipt_digest_sha256`, `originating_task_id`, `originating_session_id`,
`originating_result_identity` (only where the VERIFIED receipt exposes it),
`effective_domain`, `effective_file_scope`, a stable `verification_class`, and an explicit
statement that **the governed record is the reusable truth while the question/answer are
provenance**.

### Findings (typed, never prose-only)
`finding_class`, `reason_code`, candidate lineage identity (where safely available),
claimed scope (used ONLY as untrusted routing metadata), affected requested scope, whether
the candidate was `admitted` / `excluded` / `unavailable`, and concise diagnostic detail.

## 6. Closed vocabularies (zero value fails closed; never derived from error text)

**Projection availability:** `feedback_available`, `feedback_empty`, `feedback_degraded`,
`feedback_unavailable`, `feedback_invalid`.

**Candidate / finding classes:** `promotion_verified`, `promotion_out_of_scope`,
`promotion_incomplete`, `promotion_integrity_failure`, `promotion_contradictory`,
`promotion_stale`, `promotion_unverifiable`, `promotion_discovery_unavailable`,
`promotion_scope_identity_invalid`, `promotion_unknown_classification`.

**Mapping (frozen at Checkpoint 1):** a successful `VerifyCommittedPromotion` + in-scope →
`promotion_verified`. A `VerifyCommittedPromotion` error is classified by its TYPED cause
(not text) into `promotion_incomplete` / `promotion_integrity_failure` /
`promotion_contradictory` / `promotion_stale` / `promotion_unverifiable`; a discovery error
→ `promotion_discovery_unavailable`; a missing/empty requested domain or malformed scope →
`promotion_scope_identity_invalid`; **any unknown/unmapped error →
`promotion_unknown_classification` (visible, never admission).** Checkpoint 1 audits the
`questionpromotion` error surface and, if it is not already typed enough to distinguish
these causes without string matching, adds the minimum typed error/outcome there
(govern-first) rather than parsing messages.

## 7. Scope law

- exact domain identity ONLY — no trimming, case fold, basename/prefix/suffix match, or
  domain fallback;
- a promotion declaring a domain requires an EXACT requested-domain match;
- an unknown/empty requested domain cannot admit a domain-scoped promotion (fail closed —
  matching the existing `promotionInScope`);
- effective file scope must intersect the requested briefing file or an explicitly verified
  task file set;
- a promotion with no effective file scope is **not** assumed global;
- task-local answers never enter promoted governed knowledge;
- one repository / task / domain cannot authorize another.

**Unrelated-debris law.** A broken promotion may DEGRADE the requested briefing only when
its untrusted claimed identity or scope plausibly binds it to the requested scope. Unrelated
broken-promotion debris must not poison every briefing. Untrusted claim metadata may ROUTE a
candidate for verification; it may never establish authority or admission. (This is a new
guarantee beyond today's `collectPromotedKnowledge`, which reports every discovery-set
integrity failure as a finding regardless of relevance.)

## 8. Content + privacy boundary

- the governed record is reusable architectural knowledge; question/answer identities are
  provenance;
- task-local answer content is never presented as governed knowledge;
- raw answer TEXT is not included merely because an `answer_id` exists;
- raw dialogue is exposed only through an already-authorized explicit dialogue surface,
  never smuggled into the normal briefing;
- provenance never implies correctness, completion, merge approval, or repo-wide perfection.

**Renderable governed statement:** only the governed record's own canonical statement
(the promoted invariant/failure-mode/etc. text as it exists in the governed source) may be
rendered as human-readable knowledge. All lineage fields (`question_id`, `answer_id`,
receipt digests, task/session) remain **structured references only** — never rendered as
prose knowledge.

## 9. Server integration contract (frozen shape; wired in Checkpoint 2)

The server `Briefing` gains ONE stable typed response section (not a prose paragraph):

- a protobuf/typed addition carrying the structured `feedback_projection` (verified records
  + typed findings + availability), additive and backward compatible;
- the prose section is rendered EXCLUSIVELY from the typed projection;
- referenced IDs include governed-record + lineage identities;
- task-only briefing mode: the projection binds the exact task; file-scoped mode: the
  projection binds the requested file scope; repository context per §10;
- availability maps onto briefing status: `feedback_available`/`feedback_empty` keep the
  base `OK`/`EMPTY`; `feedback_degraded`/`feedback_unavailable` surface as a `DEGRADED`
  feedback section WITHOUT erasing the base graph briefing; `feedback_invalid` is explicit,
  never a silent empty;
- a promotion integrity problem must NOT silently collapse to an ordinary empty briefing;
- a feedback outage must NOT erase the existing graph briefing (base stays usable, feedback
  section reports its typed degraded/unavailable state);
- existing consumers remain backward compatible (the section is additive).

## 10. Repository-context authority — the bounded OPEN QUESTION

Promotion verification (`VerifyCommittedPromotion`) needs a **filesystem repository root**.
The task briefing gets it from the explicit `--repo` CLI/MCP argument. **The server
`Briefing` RPC has no filesystem repo root today — it operates over the Oxigraph graph.**

The repo root must NOT be inferred from: a file suffix, the process working directory, the
first matching checkout, a domain-name guess, a caller-controlled absolute path, or a
promotion artifact path.

**Proposed decision (for review):** the server acquires a SINGLE canonical repository-context
identity established at `sensei serve` startup (one configured/registered root, e.g. a
`--repo-root` serve flag or the already-known serve working root), never selected by the
remote caller. A `Briefing` request that names or implies a different filesystem repo is
refused; feedback for an unconfigured server reports `feedback_unavailable`
(repository-context absent), never a guessed root. **This is the smallest bounded
repository-context identity; its exact source (serve flag vs. an existing serve-owned root)
is the one explicitly unresolved question this opening defers to review before Checkpoint 2.**
No arbitrary filesystem access is added to the RPC.

## 11. Relationship to completion + GitHub

Phase 9.6 may display completion info only by consuming the canonical Phase 9.1 completion
projection where already appropriate. It must NOT: reinterpret Phase 9.4 binding/enforcement
decisions; use GitHub checks/comments/artifacts as briefing authority; change completion
policy; call the completion mutation owner; set `CorrectnessCertified`; claim a promoted
answer completed a task; or claim a completed task proves a promotion valid. Promotion
feedback and completion truth remain separate typed sections.

## 12. Proposed checkpoints

**Checkpoint 1 — canonical feedback model + reusable owner.** Govern-first records; typed
schema + closed vocabularies; deterministic digest; candidate discovery seam; owner-reused
committed-promotion verification; exact scope filtering; the unrelated-debris relevance
routing; `tasksession` migration to the shared owner; the minimum typed
`questionpromotion` error/outcome if needed for §6 mapping. **No server/protobuf wiring.**

**Checkpoint 2 — server + wire integration.** Protobuf/typed response extension; server
repository-context resolution (§10); task-only + file-scoped integration; structured
projection; prose rendered from the typed result; status + degradation semantics; backward
compatibility.

**Checkpoint 3 — adversarial proof + Phase 9.6 closure.** The full §13 matrix; unrelated-
debris isolation; determinism; structured/prose parity; no mutation; tasksession/server
parity; Ubuntu + Windows agreement; full governed-record realization.

**Checkpoint 1 does not begin in this opening PR.**

## 13. Required adversarial design matrix (forward-declared)

1. verified in-scope committed promotion appears; 2. exact provenance identities preserved;
3. task-local answer never appears; 4. unpromoted reusable candidate never appears as
governed truth; 5. incomplete promotion excluded + typed; 6-9. tampered
journal/receipt/governed-source/graph-or-marker each blocks that candidate; 10. missing
provenance edge blocks; 11. contradictory promotion evidence fails closed; 12. unknown
verification classification fails closed; 13. different domain excluded; 14. empty requested
domain cannot admit a domain-scoped promotion; 15. case/whitespace/prefix/suffix/basename
domain variants do not match; 16. unrelated file scope excluded; 17. absent effective file
scope not treated as global; 18. one task's promotion cannot enter another task's briefing
without scope intersection; 19. one repository's promotion cannot enter another's; 20.
unrelated broken-promotion debris does not degrade the requested briefing; 21. RELEVANT
broken promotion produces a typed degraded result; 22. discovery outage explicit; 23.
verification outage not reclassified as verified; 24. graph adjacency without a committed
promotion conjunction is insufficient; 25. self-described issuer/tool/status insufficient;
26. structured response + prose use the same projection; 27. prose rendering cannot change
classification; 28. mutation after projection digesting fails canonical validation; 29.
repeated unchanged execution byte-identical (excluding approved timing metrics); 30.
tasksession + server select the same promoted record set for the same verified scope; 31.
server feedback generation mutates no promotion/graph/governed/task/certification/completion
state; 32. existing briefing behavior available when feedback degraded; 33. existing Phase
9.4 behavior unchanged; 34. `CorrectnessCertified` unchanged.

## 14. Govern-first records proposed for Checkpoint 1 (not yet authored)

**Invariants:** briefing feedback consumes only independently verified committed promotion
evidence; promoted briefing knowledge preserves exact question/answer/disposition/task/
session/result/promotion provenance; task-local answers never enter reusable server briefing
context; briefing feedback is scope-exact + repository-bounded; the feedback projection is
non-authoritative and cannot certify/complete; server + task briefing consume one canonical
feedback projection.

**Failure modes:** graph provenance treated as sufficient promotion authority; server
reimplements promotion verification; unrelated promotion debris poisons every briefing;
task-local answer leaks into governed briefing context; domain/file fallback broadens
promotion scope; prose feedback diverges from the typed projection; missing feedback silently
omitted.

**Forbidden fixes:** trust promotion journal/receipt/graph-node/issuer independently; infer
scope through normalization/fallback; render raw answer dialogue as governed truth; duplicate
feedback policy in server or editor; treat a feedback outage as proof no promoted knowledge
exists; let the future editor become the feedback owner.

Each record binds later to real production symbols + executable tests.

## 15. Explicit exclusions

No VS Code cockpit; no editor commands/mutation; no new question-answer recording; no
disposition; no governed promotion; no completion; no certification; no GitHub comment/check
ingestion; no model feedback/RL/GNN/ranking; no automatic answer generation or promotion; no
raw dialogue publication; no repo-wide project completion; no generic arbitrary-RDF briefing
extension; no unrelated briefing redesign.

## 16. Checkpoint-1 review rulings (frozen)

### 16.1 Future server repository context (implemented in Checkpoint 2, frozen now)
Checkpoint 2 establishes the server repository context through an explicit, optional,
startup-owned configuration equivalent to `--repo-root <path>` (exact spelling may follow
existing CLI conventions). Frozen semantics: configured once at server startup;
canonicalized once; verified as an existing repository root; retained as immutable
server-owned context; **never** supplied/overridden by `BriefingRequest`, and never
inferred from the process cwd, the requested file, the domain, or promotion artifacts;
never changed between requests. Absent configuration ⇒ `feedback_unavailable`, while the
existing graph briefing stays usable. **Not implemented in Checkpoint 1.**

### 16.2 Empty-domain semantics (frozen)
An empty requested domain is NOT automatically malformed. Frozen distinction:
- a **malformed** domain identity ⇒ `feedback_invalid`;
- an **empty** domain ⇒ no domain-scoped promotion may be admitted;
- a promotion with an empty **verified effective** domain may still qualify when exact
  repository identity + file-scope intersection are established (domain-neutral promotions);
- where another existing boundary requires a domain because repository scope is ambiguous,
  that boundary may reject the request before feedback projection begins;
- no home-domain / basename / prefix / suffix / case / whitespace / single-candidate
  fallback is permitted.

This preserves the accepted domain-neutral promotion behavior (today's `promotionInScope`
admits an unscoped-domain promotion) without letting an unknown domain authorize a
domain-scoped record.

### 16.3 Cross-task relevance ruling (frozen)
A governed promotion is reusable across tasks. Its originating task and session are
**provenance, not consumption scope**. A failed candidate from another originating task
remains relevant when its claimed repository/domain and file scope intersect the requested
scope exactly. Frozen consequences:
- `relevantFailure` imposes **no** originating-task equality requirement — the claimed
  originating task id is never a relevance filter (that would drop genuinely relevant debris
  and break cross-task reuse for the primary task-briefing consumer);
- relevance is exact repository-identity/domain compatibility **plus** file-scope
  intersection, from UNTRUSTED claimed descriptor metadata only;
- untrusted descriptor metadata never admits a record — the verified effective scope from
  `VerifyCommittedPromotion` is the sole admission authority;
- a verified promotion likewise admits regardless of which task originally promoted it; the
  admitted record carries the ORIGINATING task/session as provenance, never the consuming
  task.

### 16.4 Repository-context + identity coherence (frozen for Checkpoint 1)
The canonical owner never derives authority from ambient filesystem context:
- the repository root must be explicitly supplied, unpadded, and **absolute**; a relative
  root is rejected at the owner boundary, never resolved with `filepath.Abs` against the
  process working directory; the established absolute root is symlink-resolved once, verified
  to be an existing directory, and the **same** established root is passed to discovery,
  descriptor loading, and verification; it is never serialized into the projection;
- `RepositoryIdentity` is the established canonical repository domain (the narrower
  Checkpoint-1 contract, absent a distinct repository-ID resolver). For task-scoped requests
  the requested domain, repository identity, and task-binding repository domain must
  correspond **exactly** (no case/whitespace repair, no prefix/suffix/basename/home-domain
  fallback); a mismatch is `feedback_invalid`. A task-scoped request additionally requires a
  non-empty task id and session id. The exact task identity is supplied by task-session
  control, never inferred from task-directory basename, active-task proximity, the requested
  file, cwd, or a promotion receipt.

### 16.5 Verification impact vocabulary (frozen)
`questionpromotion` exposes a closed verification **impact** distinct from the failure cause:
`candidate_local` (a definitive property of this candidate or a read of its own dependency)
versus `facility_unavailable` (a shared verification facility — the persisted-graph reverify
facility or a shared marker dependency — was unavailable, so nothing can be verified). A
relevant candidate-local failure degrades (`feedback_degraded`, excluded); a shared-facility
outage is `feedback_unavailable` (reported once, discovery-order independent, distinct from
malformed identity and candidate invalidity). `briefingfeedback` classifies from the typed
impact only — never by parsing error text or matching reason-code strings.

## 17. Checkpoint-2 design rulings (frozen)

### 17.1 Server repository-context identity
The server feedback context is one immutable startup-owned pair: **repository root +
repository domain**. It is configured together through explicit startup configuration
equivalent to `sensei serve --repo-root <path> --repo-domain <canonical-domain>` (exact
spellings may follow repository conventions; the two pieces form ONE context). Frozen rules:
- neither value is ever supplied by `BriefingRequest`;
- both must be configured together — one without the other is a configuration error;
- neither configured ⇒ feedback is `feedback_unavailable` while the graph briefing stays
  fully usable; only ONE repository is supported;
- the root is resolved (symlinks once) and validated as an existing directory once at
  startup; the domain is exact, unpadded, whitespace-free, and immutable;
- a request whose exact resolved graph domain is not the configured repository domain may
  still receive its graph briefing, but cannot consume the configured filesystem feedback
  context (`feedback_unavailable`, reason `repository_context_domain_mismatch`);
- no domain fallback, basename inference, first-checkout inference, or current-directory
  inference is ever allowed.

`homeDomain` is NOT overloaded as repository identity — it remains the graph scope key for
untagged nodes. `repoDomain` identifies the filesystem repository whose promotions may be
verified.

### 17.2 Task-only honesty
`BriefingRequest.task` is natural-language task text used for pattern matching. It is NOT a
canonical task id, a task session, a verified task binding, or a task file set. Therefore in
the generic server briefing a task-only request:
- must not manufacture an exact task binding, call a `tasksession` active-task fallback,
  infer a task from the only task directory, or add caller-supplied task identity fields;
- reports typed `feedback_unavailable` with reason `canonical_task_scope_not_established`,
  while its existing graph/intent/implementation-pattern briefing remains present.

The exact task-scoped feedback path already exists through `TaskBriefing.FeedbackProjection`
(Checkpoint 1); Checkpoint 2 creates no second task-resolution surface inside the generic
server briefing.

### 17.3 Additive typed wire + prose parity (frozen)
The server is a thin consumer: it never rediscovers promotion artifacts, reimplements
verification, calculates availability, reinterprets findings, accepts caller-selected
filesystem roots, or treats task text as canonical task identity. `BriefingResponse` gains an
ADDITIVE field 7 carrying the canonical projection mirrored as closed typed wire messages
(no existing field renumbered; the canonical digest is preserved; no filesystem repository
root appears on the wire). Prose is rendered ONLY from the same validated canonical
projection that is mapped to the wire — never from protobuf values independently, graph
adjacency, promotion directories, task-session limitations, or server-specific
classification. Combined status composes base ⊕ feedback by a frozen table: feedback never
converts a degraded/unavailable/invalid state into OK, and the base graph briefing content is
always preserved when feedback is degraded, unavailable, or invalid.

---

This document opens Phase 9.6 and freezes the Checkpoint-1 and Checkpoint-2 review rulings.
Checkpoint 1 implemented the canonical owner + typed `questionpromotion` seams + the
tasksession migration; Checkpoint 2 integrates the canonical projection into the server
`Briefing` RPC through an additive typed wire contract + startup-owned repository context,
adding NO editor, GitHub, certification, completion, or Phase 9.5 behavior. Phase 9.5 remains
locked; `CorrectnessCertified` untouched.

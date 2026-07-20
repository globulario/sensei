// SPDX-License-Identifier: AGPL-3.0-only

// gRPC client for the awareness-graph backend.
//
// The extension is a first-class gRPC client of the same `AwarenessGraph`
// service every other consumer uses (the `sensei` CLI, the MCP bridge) — it does
// not shell out to a binary. The contract is loaded dynamically from the
// vendored proto via @grpc/proto-loader, so there is no generated-stub build
// step and no risk of the TypeScript drifting from the wire format.

import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import * as path from 'path';

// proto-loader options chosen so the JS objects read exactly like the proto:
//   keepCase   -> snake_case field names (direct_invariants, risk_class, ...)
//   enums      -> enum fields arrive as their string names, not ints
//   defaults   -> absent scalars are zero-valued rather than undefined
const LOADER_OPTS: protoLoader.Options = {
  keepCase: true,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
};

// Vendored at editor/vscode/proto/, copied from the canonical proto/ by
// scripts/sync-proto.js. __dirname is out/ at runtime, so step up one level.
const PROTO_PATH = path.join(__dirname, '..', 'proto', 'awareness_graph.proto');

// ---- Response shapes (the subset the panel renders) -----------------------

export interface CodeAnchor {
  source_yaml?: string;
  file?: string;
  symbol?: string;
  line_start?: number;
  line_end?: number;
}

export interface KnowledgeNode {
  iri?: string;
  id?: string;
  class?: string;
  label?: string;
  severity?: string; // critical | high | warning | info | degraded
  status?: string; // active | planned | deprecated | superseded
  anchor?: CodeAnchor;
  description?: string;
  related_ids?: string[];
  // Optional UML profile (architectural spine + pattern nodes).
  uml_kind?: string; // Component | Interface | Operation | Node | Artifact | ...
  uml_stereotype?: string; // e.g. «service», «boundary»
  uml_view?: string; // structural | behavioral | interaction | deployment | awareness
  // Literal-valued rules not curated above (a pattern's requiresCall /
  // mustFollow / … ) — rules-about-code, not edges to nodes.
  facts?: Array<{ predicate?: string; value?: string }>;
}

export interface CoverageSummary {
  direct_anchor_count?: number;
  sufficient?: boolean;
  note?: string;
}

export interface GraphAuthority {
  authoritative?: boolean;
  graph_freshness_state?: string;
  graph_freshness_detail?: string;
  build_provenance_state?: string;
  seed_state?: string;
  graph_build_commit?: string;
  graph_build_time_unix?: string;
  source_repo_commit?: string;
  embedded_seed_digest_sha256?: string;
  live_store_graph_digest_sha256?: string;
  live_store_graph_triple_count?: string;
  embedded_transaction_stamp_present?: boolean;
  certified_awareness_graph_commit?: string;
  certified_services_repo_commit?: string;
  embedded_transaction_matches_seed?: boolean;
  embedded_transaction_detail?: string;
}

export type RiskClass =
  | 'RISK_CLASS_UNSPECIFIED'
  | 'LOW_RISK'
  | 'ARCHITECTURE_SENSITIVE'
  | 'CONVERGENCE_RISK'
  | 'SECURITY_RISK'
  | 'DATA_LOSS_RISK'
  | 'UNKNOWN_IMPACT';

export type PreflightStatus =
  | 'PREFLIGHT_STATUS_UNSPECIFIED'
  | 'PREFLIGHT_STATUS_OK'
  | 'PREFLIGHT_STATUS_EMPTY'
  | 'PREFLIGHT_STATUS_DEGRADED';

export interface PreflightResponse {
  status?: PreflightStatus;
  risk_class?: RiskClass;
  confidence?: string;
  authority?: GraphAuthority;
  direct_invariants?: KnowledgeNode[];
  direct_failure_modes?: KnowledgeNode[];
  direct_intents?: KnowledgeNode[];
  direct_forbidden_fixes?: KnowledgeNode[];
  direct_required_tests?: KnowledgeNode[];
  direct_architecture?: KnowledgeNode[];
  required_actions?: string[];
  forbidden_fixes?: string[];
  tests_to_run?: string[];
  files_to_read?: string[];
  blind_spots?: string[];
  coverage?: CoverageSummary;
}

export interface PreflightRequest {
  task?: string;
  files?: string[];
  mode?: 'PREFLIGHT_COMPACT' | 'PREFLIGHT_STANDARD';
  domain?: string;
}

// ---- Client ---------------------------------------------------------------

// gRPC clients are cheap to keep open and manage their own reconnection, so we
// reuse one per address and only rebuild it when the configured address moves.
let client: grpc.Client | undefined;
let clientAddr: string | undefined;

function getClient(addr: string): any {
  if (client && clientAddr === addr) {
    return client;
  }
  if (client) {
    client.close();
  }
  const def = protoLoader.loadSync(PROTO_PATH, LOADER_OPTS);
  const pkg = grpc.loadPackageDefinition(def) as any;
  const ServiceCtor = pkg.globular.awareness_graph.AwarenessGraph;
  client = new ServiceCtor(addr, grpc.credentials.createInsecure());
  clientAddr = addr;
  return client;
}

export function disposeClient(): void {
  if (client) {
    client.close();
    client = undefined;
    clientAddr = undefined;
  }
}

/** A gRPC failure annotated with its status code for friendlier messaging. */
export class AwgError extends Error {
  constructor(message: string, readonly code?: grpc.status) {
    super(message);
    this.name = 'AwgError';
  }

  /** True when the server could not be reached (vs. an application error). */
  get unreachable(): boolean {
    return (
      this.code === grpc.status.UNAVAILABLE ||
      this.code === grpc.status.DEADLINE_EXCEEDED
    );
  }
}

// Generic unary call against the dynamic client. Methods are named exactly as
// the proto rpc (Preflight, Metadata, Query, Resolve).
function unary<T>(
  addr: string,
  method: string,
  req: unknown,
  timeoutMs: number
): Promise<T> {
  return new Promise((resolve, reject) => {
    const c = getClient(addr);
    const deadline = new Date(Date.now() + timeoutMs);
    c[method](
      req,
      { deadline },
      (err: grpc.ServiceError | null, resp: T) => {
        if (err) {
          reject(new AwgError(err.message, err.code));
          return;
        }
        resolve(resp);
      }
    );
  });
}

export function preflight(
  addr: string,
  req: PreflightRequest,
  timeoutMs: number
): Promise<PreflightResponse> {
  return unary<PreflightResponse>(addr, 'Preflight', req, timeoutMs);
}

// ---- Project-level reads (for the dashboard) ------------------------------

// int64 fields arrive as strings (longs: String). The dashboard coerces with
// Number() where it needs arithmetic.
export interface MetadataResponse {
  graph_build_commit?: string;
  graph_build_time_unix?: string;
  source_repo_commit?: string;
  embedded_seed_digest_sha256?: string;
  embedded_seed_marker_iri?: string;
  live_store_contains_embedded_seed_marker?: boolean;
  live_store_graph_digest_sha256?: string;
  live_store_graph_triple_count?: string;
  build_provenance_state?: string;
  coverage_state?: string;
  seed_state?: string;
  graph_freshness_state?: string;
  graph_freshness_detail?: string;
  candidate_queue_state?: string;
  local_candidate_file_count?: string;
  local_candidate_entry_count?: string;
  benchmark_state?: string;
  benchmark_contract_count?: string;
  benchmark_learning_event_count?: string;
  benchmark_latest_learning_event_unix?: string;
  benchmark_latest_task_id?: string;
  benchmark_latest_score?: string;
  benchmark_latest_certification_status?: string;
  server_version?: string;
  triple_count?: string;
  invariant_count?: string;
  failure_mode_count?: string;
  incident_pattern_count?: string;
  intent_count?: string;
  forbidden_fix_count?: string;
  required_test_count?: string;
  source_file_count?: string;
  code_symbol_count?: string;
  // Architectural spine + pattern + UML layer.
  meta_principle_count?: string;
  component_count?: string;
  boundary_count?: string;
  contract_count?: string;
  decision_count?: string;
  evidence_count?: string;
  design_pattern_count?: string;
  implementation_pattern_count?: string;
  pattern_misuse_count?: string;
  // Phase 2 closure/dialogue/evidence graph classes. These are explicit-query
  // only and do not contribute to Phase 1 graph coverage or readiness.
  architecture_claim_count?: string;
  open_question_count?: string;
  architect_answer_count?: string;
  evidence_probe_count?: string;
  server_started_unix?: string;
  generated_in_ms?: string;
  /** Distinct selectable domain keys in the graph (for the domain filter). */
  available_domains?: string[];
}

export interface QueryRow {
  id?: string; // class-qualified, e.g. "invariant:audit.foo"
  class?: string; // "invariant" | "failure_mode" | ...
  label?: string;
  severity?: string;
  status?: string;
  relation?: string; // set for related-mode rows
  source_file?: string;
  uml_kind?: string;
  uml_stereotype?: string;
  uml_view?: string;
}

export interface QueryResponse {
  rows?: QueryRow[];
  generated_in_ms?: string;
  authority?: GraphAuthority;
}

export interface ResolveResult {
  found?: boolean;
  node?: KnowledgeNode;
  authority?: GraphAuthority;
}

export function metadata(
  addr: string,
  timeoutMs: number,
  domain?: string
): Promise<MetadataResponse> {
  const body = domain ? { domain } : {};
  return unary<MetadataResponse>(addr, 'Metadata', body, timeoutMs);
}

/** List all nodes of a class. `cls` is a QueryClass enum name (string). */
export function queryByClass(
  addr: string,
  cls: string,
  limit: number,
  timeoutMs: number,
  domain?: string
): Promise<QueryResponse> {
  const body: Record<string, unknown> = { mode: 'QUERY_MODE_BY_CLASS', class: cls, limit };
  if (domain) {
    body.domain = domain;
  }
  return unary<QueryResponse>(addr, 'Query', body, timeoutMs);
}

/** Neighbors of a class-qualified id, each row carrying its edge `relation`. */
export function queryRelated(
  addr: string,
  id: string,
  limit: number,
  timeoutMs: number,
  domain?: string
): Promise<QueryResponse> {
  const body: Record<string, unknown> = { mode: 'QUERY_MODE_RELATED', id, limit };
  if (domain) {
    body.domain = domain;
  }
  return unary<QueryResponse>(
    addr,
    'Query',
    body,
    timeoutMs
  );
}

/** Resolve full node detail. `rdfClass` is an ontology class name (Invariant…). */
export function resolveNode(
  addr: string,
  rdfClass: string,
  bareId: string,
  domain: string,
  timeoutMs: number
): Promise<ResolveResult> {
  return unary<ResolveResult>(
    addr,
    'Resolve',
    { class: rdfClass, id: bareId, domain },
    timeoutMs
  );
}

// ---- Phase 9.5 architectural control panel (read-only) --------------------
//
// The four controlstate RPCs. Every field below mirrors the protobuf VERBATIM
// (snake_case; enums as their full proto-name strings). The panel RENDERS these
// — it never re-derives closure, severity, lifecycle, class, or availability.
//
// Unknown-versus-zero law: proto3 `optional` count fields decode to `undefined`
// when the source was NOT observed and to a numeric string ("0", "5", …) when
// observed. The renderer MUST treat `undefined` as unknown ("—") and a present
// "0" as a real zero — never `Number(x || 0)`. Absent message fields (coverage,
// active_task, completion, feedback_context) decode to `null` (source
// unavailable), never an empty object. This is proven by controlPanelDecode.test.ts.

export type ArchitectureAvailability =
  | 'ARCHITECTURE_AVAILABILITY_UNSPECIFIED'
  | 'ARCHITECTURE_AVAILABILITY_AVAILABLE'
  | 'ARCHITECTURE_AVAILABILITY_PARTIAL'
  | 'ARCHITECTURE_AVAILABILITY_UNAVAILABLE'
  | 'ARCHITECTURE_AVAILABILITY_INVALID';

export type ArchitectureSourceAvailability =
  | 'ARCHITECTURE_SOURCE_AVAILABILITY_UNSPECIFIED'
  | 'ARCHITECTURE_SOURCE_AVAILABILITY_AVAILABLE'
  | 'ARCHITECTURE_SOURCE_AVAILABILITY_DEGRADED'
  | 'ARCHITECTURE_SOURCE_AVAILABILITY_UNAVAILABLE'
  | 'ARCHITECTURE_SOURCE_AVAILABILITY_INVALID';

export type ArchitectureSourceImpact =
  | 'ARCHITECTURE_SOURCE_IMPACT_UNSPECIFIED'
  | 'ARCHITECTURE_SOURCE_IMPACT_PRIMARY'
  | 'ARCHITECTURE_SOURCE_IMPACT_REQUIRED'
  | 'ARCHITECTURE_SOURCE_IMPACT_RELEVANT'
  | 'ARCHITECTURE_SOURCE_IMPACT_OPTIONAL';

export type ArchitectureArtifactClosure =
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_UNSPECIFIED'
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED'
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_OPEN'
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED'
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN'
  | 'ARCHITECTURE_ARTIFACT_CLOSURE_NOT_APPLICABLE';

export type ArchitectureDimensionState =
  | 'ARCHITECTURE_DIMENSION_STATE_UNSPECIFIED'
  | 'ARCHITECTURE_DIMENSION_STATE_SATISFIED'
  | 'ARCHITECTURE_DIMENSION_STATE_OPEN'
  | 'ARCHITECTURE_DIMENSION_STATE_DEGRADED'
  | 'ARCHITECTURE_DIMENSION_STATE_UNKNOWN'
  | 'ARCHITECTURE_DIMENSION_STATE_NOT_APPLICABLE';

export type ArchitectureLifecycleState =
  | 'ARCHITECTURE_LIFECYCLE_STATE_UNSPECIFIED'
  | 'ARCHITECTURE_LIFECYCLE_STATE_ACTIVE'
  | 'ARCHITECTURE_LIFECYCLE_STATE_PROPOSED'
  | 'ARCHITECTURE_LIFECYCLE_STATE_DEPRECATED'
  | 'ARCHITECTURE_LIFECYCLE_STATE_SUPERSEDED'
  | 'ARCHITECTURE_LIFECYCLE_STATE_REVOKED'
  | 'ARCHITECTURE_LIFECYCLE_STATE_UNKNOWN'
  | 'ARCHITECTURE_LIFECYCLE_STATE_NOT_APPLICABLE';

export type ArchitectureAttentionSeverity =
  | 'ARCHITECTURE_ATTENTION_SEVERITY_UNSPECIFIED'
  | 'ARCHITECTURE_ATTENTION_SEVERITY_INFORMATIONAL'
  | 'ARCHITECTURE_ATTENTION_SEVERITY_ATTENTION'
  | 'ARCHITECTURE_ATTENTION_SEVERITY_WARNING'
  | 'ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL';

export type ArchitectureAssessmentCoverage =
  | 'ARCHITECTURE_ASSESSMENT_COVERAGE_UNSPECIFIED'
  | 'ARCHITECTURE_ASSESSMENT_COVERAGE_ASSESSABLE'
  | 'ARCHITECTURE_ASSESSMENT_COVERAGE_EXPLICITLY_NOT_APPLICABLE'
  | 'ARCHITECTURE_ASSESSMENT_COVERAGE_UNSUPPORTED'
  | 'ARCHITECTURE_ASSESSMENT_COVERAGE_UNKNOWN';

export interface ArchitectureSourceStatus {
  owner?: string;
  schema?: string;
  availability?: ArchitectureSourceAvailability;
  impact?: ArchitectureSourceImpact;
  reason_code?: string;
  identity?: string;
  digest?: string;
}

export interface ArchitectureProjectionMeta {
  schema_version?: string;
  producer_name?: string;
  producer_version?: string;
  repository_identity?: string;
  requested_domain?: string;
  availability?: ArchitectureAvailability;
  sources?: ArchitectureSourceStatus[];
  non_authoritative_projection?: boolean;
  limitations?: string[];
  digest_sha256?: string;
}

export interface ArchitectureArtifactIdentity {
  node_iri?: string;
  canonical_class?: string;
  observed_classes?: string[];
  repository_identity?: string;
  domain_identity?: string;
  graph_authority_identity?: string;
  provenance_identities?: string[];
}

export interface ArchitectureLifecycleAssessment {
  applicable?: boolean;
  vocabulary?: string;
  state?: ArchitectureLifecycleState;
  source_owner?: string;
  source_identity?: string;
  source_availability?: ArchitectureSourceAvailability;
  reason_code?: string;
}

export interface ArchitectureDimensionAssessment {
  dimension?: string;
  label?: string;
  applicable?: boolean;
  required?: boolean;
  state?: ArchitectureDimensionState;
  reason_code?: string;
  blockers?: string[];
  evidence?: string[];
  questions?: string[];
  owner?: string;
  next_action_owner?: string;
}

export interface ArchitectureAttentionItem {
  id?: string;
  source_owner?: string;
  source_schema?: string;
  source_identity?: string;
  attention_class?: string;
  reason_code?: string;
  severity?: ArchitectureAttentionSeverity;
  severity_basis?: string;
  source_digest?: string;
  affected_artifacts?: string[];
  blocking?: boolean;
  evidence?: string[];
  next_action_owner?: string;
  architect_input_required?: boolean;
}

export interface ArchitectureKeyedCount {
  key?: string;
  count?: string; // int64 as string (longs: String)
}

export interface ArchitectureGraphAuthoritySummary {
  observed?: boolean;
  current?: boolean;
  integrity?: boolean;
  identity?: string;
}

export interface ArchitectureCoverageSummary {
  sufficient?: boolean;
  blind_spot_count?: string;
  high_risk_blind_spot_count?: string;
}

export interface ArchitectureTaskSummary {
  task_id?: string;
  session_id?: string;
  closure?: string;
  admission?: string;
}

export interface ArchitectureCompletionSummary {
  terminal_state?: string;
  authoritative_completion?: boolean;
}

export interface ArchitectureFeedbackContext {
  capable?: boolean;
  availability?: string; // Phase 9.6 feedback vocabulary
}

export interface ArchitectureScopedFeedbackRef {
  scope_identity?: string;
  projection_digest?: string;
  availability?: string;
  verified_record_ids?: string[];
  lineage_ids?: string[];
  limitations?: string[];
}

export interface ArchitectureControlSnapshot {
  meta?: ArchitectureProjectionMeta;
  registry_digest?: string;
  graph_authority?: ArchitectureGraphAuthoritySummary;
  counts_by_class?: ArchitectureKeyedCount[];
  assessment_coverage_counts?: ArchitectureKeyedCount[];
  closure_counts?: ArchitectureKeyedCount[];
  // optional int64 → undefined when unobserved, "N" when observed. Never coerce
  // absence to zero.
  lifecycle_unknown_count?: string;
  attention_counts_by_severity?: ArchitectureKeyedCount[];
  top_attention?: ArchitectureAttentionItem[];
  open_question_count?: string;
  contradiction_count?: string;
  missing_evidence_count?: string;
  missing_test_count?: string;
  missing_enforcement_count?: string;
  // Absent (null) when the source was not observed — render "unavailable".
  coverage?: ArchitectureCoverageSummary | null;
  active_task?: ArchitectureTaskSummary | null;
  completion?: ArchitectureCompletionSummary | null;
  feedback_context?: ArchitectureFeedbackContext | null;
}

export interface ArchitectureArtifactSummary {
  identity?: ArchitectureArtifactIdentity;
  label?: string;
  family?: string;
  class?: string;
  assessment_coverage?: ArchitectureAssessmentCoverage;
  lifecycle?: ArchitectureLifecycleState;
  closure?: ArchitectureArtifactClosure;
  open_required_dimensions?: string; // int64 as string
  // optional → absent means "zero attention items", NOT informational.
  highest_severity?: ArchitectureAttentionSeverity;
  attention_count?: string;
  owner_summary?: string;
  availability?: ArchitectureAvailability;
}

export interface ArchitectureArtifactIndex {
  meta?: ArchitectureProjectionMeta;
  registry_digest?: string;
  page?: ArchitectureArtifactSummary[];
  next_cursor?: string; // opaque; echo back unchanged
  truncated?: boolean;
}

// The full artifact state. CP3 renders ONLY the thin header subset
// (identity, canonical_class, assessment_coverage, closure, closure_reason,
// lifecycle, availability via meta, next_action_owner). The dimensions /
// attention / questions / evidence / feedback are typed here but are NOT
// rendered until Checkpoint 4.
export interface ArchitectureArtifactState {
  meta?: ArchitectureProjectionMeta;
  identity?: ArchitectureArtifactIdentity;
  canonical_class?: string;
  assessment_coverage?: ArchitectureAssessmentCoverage;
  closure?: ArchitectureArtifactClosure;
  closure_reason?: string;
  lifecycle?: ArchitectureLifecycleAssessment;
  dimensions?: ArchitectureDimensionAssessment[];
  attention?: ArchitectureAttentionItem[];
  questions?: string[];
  evidence?: string[];
  feedback?: ArchitectureScopedFeedbackRef | null;
  next_action_owner?: string;
}

export interface ArchitectureNavigationClass {
  class_iri?: string;
  label?: string;
  order?: number;
  coverage?: ArchitectureAssessmentCoverage;
  assessable_artifact?: boolean;
  query_capable?: boolean;
  resolve_capable?: boolean;
  inspector_capable?: boolean;
  question_capable?: boolean;
  default_visible?: boolean;
  overview_visible?: boolean;
}

export interface ArchitectureNavigationFamily {
  id?: string;
  label?: string;
  order?: number;
  classes?: ArchitectureNavigationClass[];
}

export interface OntologyNavigationDescriptor {
  meta?: ArchitectureProjectionMeta;
  registry_digest?: string;
  families?: ArchitectureNavigationFamily[];
  unknown_class_fallback?: ArchitectureNavigationClass;
}

/** Visibility filters for the artifact index. Only PRESENT (non-UNSPECIFIED)
 *  values are sent — the server rejects UNSPECIFIED enum filters. */
export interface ArtifactListFilters {
  family_filter?: string;
  class_filter?: string;
  closure_filter?: ArchitectureArtifactClosure;
  severity_filter?: ArchitectureAttentionSeverity;
}

/** ontology.navigation_descriptor/v1 — registry-derived, repository-independent. */
export function getOntologyNavigationDescriptor(
  addr: string,
  timeoutMs: number
): Promise<{ descriptor?: OntologyNavigationDescriptor }> {
  return unary<{ descriptor?: OntologyNavigationDescriptor }>(
    addr,
    'GetOntologyNavigationDescriptor',
    {},
    timeoutMs
  );
}

/** architecture.control_snapshot/v1 for one repository/domain scope. */
export function getArchitectureControlSnapshot(
  addr: string,
  repositoryIdentity: string,
  domain: string,
  timeoutMs: number
): Promise<{ snapshot?: ArchitectureControlSnapshot }> {
  return unary<{ snapshot?: ArchitectureControlSnapshot }>(
    addr,
    'GetArchitectureControlSnapshot',
    { repository_identity: repositoryIdentity, domain },
    timeoutMs
  );
}

/** architecture.artifact_index/v1 — one stable page. `cursor` is opaque and
 *  echoed back from a previous response; filters are applied only when set. */
export function listArchitectureArtifacts(
  addr: string,
  repositoryIdentity: string,
  domain: string,
  pageSize: number,
  cursor: string,
  filters: ArtifactListFilters,
  timeoutMs: number
): Promise<{ index?: ArchitectureArtifactIndex }> {
  const body: Record<string, unknown> = {
    repository_identity: repositoryIdentity,
    domain,
    page_size: Math.min(pageSize > 0 ? pageSize : 100, 250),
  };
  if (cursor) {
    body.cursor = cursor;
  }
  if (filters.family_filter) {
    body.family_filter = filters.family_filter;
  }
  if (filters.class_filter) {
    body.class_filter = filters.class_filter;
  }
  if (filters.closure_filter) {
    body.closure_filter = filters.closure_filter;
  }
  if (filters.severity_filter) {
    body.severity_filter = filters.severity_filter;
  }
  return unary<{ index?: ArchitectureArtifactIndex }>(
    addr,
    'ListArchitectureArtifacts',
    body,
    timeoutMs
  );
}

/** architecture.artifact_state/v1 for one exact node IRI. Optional expected_*
 *  values are preconditions (FailedPrecondition on drift), never authorities. */
export function getArchitectureArtifactState(
  addr: string,
  repositoryIdentity: string,
  domain: string,
  nodeIri: string,
  timeoutMs: number,
  expected?: { graphAuthorityIdentity?: string; registryDigest?: string }
): Promise<{ state?: ArchitectureArtifactState }> {
  const body: Record<string, unknown> = {
    repository_identity: repositoryIdentity,
    domain,
    node_iri: nodeIri,
  };
  if (expected?.graphAuthorityIdentity) {
    body.expected_graph_authority_identity = expected.graphAuthorityIdentity;
  }
  if (expected?.registryDigest) {
    body.expected_registry_digest = expected.registryDigest;
  }
  return unary<{ state?: ArchitectureArtifactState }>(
    addr,
    'GetArchitectureArtifactState',
    body,
    timeoutMs
  );
}

// ---- Phase 9.5 Checkpoint 5: guarded architect-answer mutation family --------
//
// The ONLY mutation RPCs. Each delegates to a guarded owner; a refusal is typed
// response data (mutation_applied=false), never a thrown error. The raw answer
// travels as opaque bytes and is never echoed back.

export type ArchitectureDisposition =
  | 'ARCHITECTURE_DISPOSITION_ANSWERED'
  | 'ARCHITECTURE_DISPOSITION_DISMISSED'
  | 'ARCHITECTURE_DISPOSITION_DEFERRED'
  | 'ARCHITECTURE_DISPOSITION_TASK_LOCAL';

export type ArchitectureReusability =
  | 'ARCHITECTURE_REUSABILITY_NONE'
  | 'ARCHITECTURE_REUSABILITY_REUSABLE_CANDIDATE'
  | 'ARCHITECTURE_REUSABILITY_TASK_LOCAL';

export interface ArchitectureMutationAudit {
  operation_identity?: string;
  operation_kind?: string;
  actor_identity?: string;
  repository_identity?: string;
  domain?: string;
  task_id?: string;
  session_id?: string;
  question_id?: string;
  previous_ledger_head_sha256?: string;
  resulting_ledger_head_sha256?: string;
  owner_outcome?: string;
  replay_status?: string;
  mutation_applied?: boolean;
}

export interface ArchitectureMutationRefusal {
  reason_code?: string;
  detail?: string;
  owner?: string;
  mutation_applied?: boolean;
  audit?: ArchitectureMutationAudit;
}

export interface ArchitectureDispositionInput {
  repository_identity: string;
  domain?: string;
  task_id?: string;
  session_id?: string;
  question_id: string;
  actor_identity?: string;
  disposition: ArchitectureDisposition;
  reusability: ArchitectureReusability;
  rationale?: string;
  answer_id?: string;
  answer_bytes?: Uint8Array;
  effective_scope_domain?: string;
  effective_scope_files?: string[];
  evidence_refs?: string[];
}

export interface ArchitectureDispositionCandidate {
  question_id?: string;
  receipt_digest_sha256?: string;
  receipt_byte_digest_sha256?: string;
  expected_ledger_head_digest_sha256?: string;
  anchor_entry_digest_sha256?: string;
}

export interface ArchitectureDispositionReceipt {
  outcome?: string;
  question_id?: string;
  receipt_digest_sha256?: string;
  entry_digest_sha256?: string;
  previous_ledger_head_sha256?: string;
  current_ledger_head_sha256?: string;
  ledger_sequence?: string;
  contested_prior_digests?: string[];
  projection_state?: string;
  audit?: ArchitectureMutationAudit;
}

/** Pure prepare — writes nothing; returns a candidate XOR a typed refusal. */
export function prepareArchitectAnswerDisposition(
  addr: string,
  input: ArchitectureDispositionInput,
  timeoutMs: number
): Promise<{ candidate?: ArchitectureDispositionCandidate; refusal?: ArchitectureMutationRefusal }> {
  return unary(addr, 'PrepareArchitectAnswerDisposition', { input }, timeoutMs);
}

/** Commit exactly one disposition against the expected ledger head precondition. */
export function recordArchitectAnswerDisposition(
  addr: string,
  input: ArchitectureDispositionInput,
  expectedLedgerHeadDigestSha256: string,
  timeoutMs: number
): Promise<{ receipt?: ArchitectureDispositionReceipt; refusal?: ArchitectureMutationRefusal }> {
  return unary(
    addr,
    'RecordArchitectAnswerDisposition',
    { input, expected_ledger_head_digest_sha256: expectedLedgerHeadDigestSha256 },
    timeoutMs
  );
}

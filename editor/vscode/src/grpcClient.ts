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
  server_started_unix?: string;
  generated_in_ms?: string;
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

export function metadata(addr: string, timeoutMs: number): Promise<MetadataResponse> {
  return unary<MetadataResponse>(addr, 'Metadata', {}, timeoutMs);
}

/** List all nodes of a class. `cls` is a QueryClass enum name (string). */
export function queryByClass(
  addr: string,
  cls: string,
  limit: number,
  timeoutMs: number
): Promise<QueryResponse> {
  return unary<QueryResponse>(
    addr,
    'Query',
    { mode: 'QUERY_MODE_BY_CLASS', class: cls, limit },
    timeoutMs
  );
}

/** Neighbors of a class-qualified id, each row carrying its edge `relation`. */
export function queryRelated(
  addr: string,
  id: string,
  limit: number,
  timeoutMs: number
): Promise<QueryResponse> {
  return unary<QueryResponse>(
    addr,
    'Query',
    { mode: 'QUERY_MODE_RELATED', id, limit },
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

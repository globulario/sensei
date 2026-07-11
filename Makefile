# awareness-graph — developer convenience targets.
#
# This Makefile is deliberately small: it captures the *exact* commands
# the README documents, so an operator who runs `make proto` or
# `make test` gets the same result a fresh-from-git contributor would.
# It is not a build system — the Go toolchain is.
#
# Targets:
#   make proto    Regenerate awarenesspb Go bindings from
#                 proto/awareness_graph.proto. Re-run after any proto
#                 contract change. Output goes to golang/pb/.
#   make test     Run the full test sweep (importer, vocab drift, CLI,
#                 generated-code smoke).
#   make build    Compile every package. Catches type / import issues
#                 the test command would also catch, but faster on
#                 large changes.
#   make tools    One-shot installer for the two Go-managed plugins
#                 (protoc-gen-go, protoc-gen-go-grpc). protoc itself
#                 must come from the system package manager (libprotobuf-dev
#                 on Debian/Ubuntu, brew install protobuf on macOS).
#   make clean    Remove generated proto code. Use sparingly — `make proto`
#                 overwrites in place, so a clean step is rarely needed.

.PHONY: proto proto-contracts proto-contracts-check import-graph import-graph-check test build tools clean server service-build service-smoke service-dist service-package mcp oxigraph oxigraph-health smoke-local graph-fixture graph-self load-fixture load-self load-release-seed principle-check principle-check-workflow-service principle-check-fallback principle-check-ruleguard-tree principle-check-positive principle-check-declarations principle-check-artifacts principle-check-coverage principle-check-all sensei sensei-cli sensei-smoke awg awg-cli awg-build awg-smoke scip

PROTO_SRC   := proto/awareness_graph.proto
PROTO_DIR   := proto
PB_OUT_DIR  := golang/pb
SERVER_ADDR  ?= :10120
OXIGRAPH_URL ?= http://localhost:7878/query
OXIGRAPH_QUERY_URL ?= http://localhost:7878/query
OXIGRAPH_UPDATE_URL ?= http://localhost:7878/store?default
GRAPH_FIXTURE_OUT ?= /tmp/awareness-test.nt
GRAPH_SELF_OUT ?= /tmp/awareness-graph-self.nt
SERVICE_SPEC ?= packaging/specs/awareness_graph_service.yaml
SERVICE_METADATA ?= packaging/metadata/awareness-graph/package.json
SERVICE_DIST_ROOT ?= /tmp/awareness-graph-service-root
SERVICE_PACKAGE_OUT ?= /tmp/awareness-graph-packages
SERVICE_PUBLISHER ?= core@globular.io
SERVICE_PLATFORM ?= linux_amd64
SERVICE_VERSION ?= 0.0.6

proto: $(PB_OUT_DIR)
	protoc \
		--proto_path=$(PROTO_DIR) \
		--go_out=$(PB_OUT_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(PB_OUT_DIR) --go-grpc_opt=paths=source_relative \
		$(notdir $(PROTO_SRC))
	@echo "proto: regenerated $(PB_OUT_DIR)/awareness_graph.pb.go + awareness_graph_grpc.pb.go"

$(PB_OUT_DIR):
	mkdir -p $@

# proto-contracts — regenerate the inferred Contract nodes (architectural spine
# Stage B) from the .proto sources. The generated YAML is committed and ingested
# by the awareness importer; proto-contracts-check is the CI freshness gate.
PROTO_CONTRACTS_OUT := docs/awareness/architecture/awareness_graph_proto_contracts.yaml
PROTO_CONTRACTS_ARGS := -proto $(PROTO_SRC) -repo-root . \
	-component AwarenessGraph=component.awareness_graph_service \
	-output $(PROTO_CONTRACTS_OUT)

proto-contracts:
	go run ./cmd/proto-scan $(PROTO_CONTRACTS_ARGS)

proto-contracts-check:
	go run ./cmd/proto-scan $(PROTO_CONTRACTS_ARGS) -check

# http-contracts — regenerate the inferred HTTP implementation contracts from
# the Globular gateway's mux.Handle/HandleFunc routes (Phase 2 edge surfaces).
# The committed YAML is ingested by the awareness importer; the -check variant
# is the CI freshness gate.
HTTP_CONTRACTS_REPO ?= ../Globular
HTTP_CONTRACTS_OUT  ?= ../services/docs/awareness/generated/http_contracts.yaml
HTTP_CONTRACTS_ARGS := -repo-root $(HTTP_CONTRACTS_REPO) -output $(HTTP_CONTRACTS_OUT)

http-contracts:
	go run ./cmd/http-scan $(HTTP_CONTRACTS_ARGS)

http-contracts-check:
	go run ./cmd/http-scan $(HTTP_CONTRACTS_ARGS) -check

# suggest-realizations — regenerate conservative candidateRealizesContract
# proposals (impl → architectural contract). Candidates only; promotion is a
# separate human step. -check is the CI freshness gate.
suggest-realizations:
	go run ./cmd/awg suggest-realizations

suggest-realizations-check:
	go run ./cmd/awg suggest-realizations -check

# import-graph — regenerate the inferred component dependency edges from source
# imports. Generic multi-language extractor (no project-specific paths); the
# committed per-language YAML is ingested by the awareness importer;
# import-graph-check is the CI freshness gate. Run with no -config so AWG's own
# self-graph carries no project conventions. One target per language (Go today).
IMPORT_GRAPH_GO_OUT := docs/awareness/generated/awareness_graph_go_import_graph.yaml
IMPORT_GRAPH_GO_ARGS := -repo-root . -lang go -output $(IMPORT_GRAPH_GO_OUT)
IMPORT_GRAPH_TS_OUT := docs/awareness/generated/awareness_graph_typescript_import_graph.yaml
IMPORT_GRAPH_TS_ARGS := -repo-root . -lang typescript -output $(IMPORT_GRAPH_TS_OUT)
IMPORT_GRAPH_PY_OUT := docs/awareness/generated/awareness_graph_python_import_graph.yaml
IMPORT_GRAPH_PY_ARGS := -repo-root . -lang python -output $(IMPORT_GRAPH_PY_OUT)

import-graph:
	go run ./cmd/import-scan $(IMPORT_GRAPH_GO_ARGS)
	go run ./cmd/import-scan $(IMPORT_GRAPH_TS_ARGS)
	go run ./cmd/import-scan $(IMPORT_GRAPH_PY_ARGS)

import-graph-check:
	go run ./cmd/import-scan $(IMPORT_GRAPH_GO_ARGS) -check
	go run ./cmd/import-scan $(IMPORT_GRAPH_TS_ARGS) -check
	go run ./cmd/import-scan $(IMPORT_GRAPH_PY_ARGS) -check

test:
	go test ./...

build:
	go build ./...

# Run the gRPC server in the foreground.
# RPCs return codes.Unimplemented until Resolve/Impact/Briefing/Query
# handlers land. -oxigraph-url controls the RDF backend; with the
# default require-store=false, a missing backend just logs a warning
# and the server still starts. Override either knob:
#   make server SERVER_ADDR=:10121
#   make server OXIGRAPH_URL=http://my-oxigraph.internal:7878/query
server:
	go run ./golang/server -addr $(SERVER_ADDR) -oxigraph-url $(OXIGRAPH_URL)

# Build awareness-graph server binary for service packaging/install flows.
service-build:
	@BUILD_COMMIT="$$(git rev-parse --short=12 HEAD 2>/dev/null || echo '')"; \
	BUILD_TIME="$$(date -u +%s)"; \
	SRC_COMMIT="$$(git -C $${SERVICES_REPO:-../services} rev-parse --short=12 HEAD 2>/dev/null || echo '')"; \
	go build \
	  -ldflags "-X main.Version=$(SERVICE_VERSION) -X main.BuildCommit=$$BUILD_COMMIT -X main.BuildTimeUnix=$$BUILD_TIME -X main.SourceCommit=$$SRC_COMMIT" \
	  -o ./bin/awareness-graph ./golang/server

# Smoke service metadata and health surfaces.
service-smoke:
	go run ./golang/server --describe
	go run ./golang/server --health

# Stage service payload root for Globular package build.
service-dist: service-build
	@case "$(SERVICE_DIST_ROOT)" in \
	  ""|/|/home|/root|/usr|/etc|/var|/bin|/boot|"$(HOME)") \
	    echo "service-dist: refusing to 'rm -rf' unsafe SERVICE_DIST_ROOT='$(SERVICE_DIST_ROOT)'" >&2; exit 1;; \
	esac
	rm -rf $(SERVICE_DIST_ROOT)
	mkdir -p $(SERVICE_DIST_ROOT)/bin $(SERVICE_DIST_ROOT)/specs $(SERVICE_DIST_ROOT)/metadata/awareness-graph $(SERVICE_DIST_ROOT)/seed $(SERVICE_DIST_ROOT)/proto
	cp ./bin/awareness-graph $(SERVICE_DIST_ROOT)/bin/awareness-graph
	cp ./golang/server/embeddata/awareness.nt $(SERVICE_DIST_ROOT)/seed/awareness.nt
	cp ./proto/awareness_graph.proto $(SERVICE_DIST_ROOT)/proto/awareness_graph.proto
	cp $(SERVICE_SPEC) $(SERVICE_DIST_ROOT)/specs/awareness_graph_service.yaml
	cp $(SERVICE_METADATA) $(SERVICE_DIST_ROOT)/metadata/awareness-graph/package.json
	sha256sum $(SERVICE_DIST_ROOT)/bin/awareness-graph | awk '{print $$1}' > $(SERVICE_DIST_ROOT)/metadata/awareness-graph/entrypoint.sha256
	@echo "service-dist: staged $(SERVICE_DIST_ROOT)"

# Build Globular package artifact from staged service payload.
service-package: service-dist
	mkdir -p $(SERVICE_PACKAGE_OUT)
	globular pkg build \
		--spec $(SERVICE_DIST_ROOT)/specs/awareness_graph_service.yaml \
		--root $(SERVICE_DIST_ROOT) \
		--version $(SERVICE_VERSION) \
		--publisher $(SERVICE_PUBLISHER) \
		--platform $(SERVICE_PLATFORM) \
		--out $(SERVICE_PACKAGE_OUT) \
		--skip-missing-config=true \
		--skip-missing-systemd=true
	@echo "service-package: wrote package(s) under $(SERVICE_PACKAGE_OUT)"

# Run MCP bridge in foreground (stdio JSON-RPC).
mcp:
	go run ./cmd/awareness-mcp -awareness-addr localhost:10120

# Start local Oxigraph for development (native binary, no Docker).
oxigraph:
	./scripts/bootstrap_oxigraph.sh

# Verify Oxigraph query endpoint with ASK {} health check.
oxigraph-health:
	curl -sS -f -X POST \
		-H "Content-Type: application/sparql-query" \
		--data 'ASK {}' \
		$(OXIGRAPH_QUERY_URL)
	@echo "oxigraph-health: query endpoint healthy at $(OXIGRAPH_QUERY_URL)"

# Build fixture graph from extractor test data.
graph-fixture:
	go run ./cmd/yaml2nt -input ./golang/extractor/testdata -output $(GRAPH_FIXTURE_OUT)

# Build self-awareness graph from docs/awareness (self + generic).
graph-self:
	go run ./cmd/yaml2nt -input ./docs/awareness -input ./docs/awareness/generic -output $(GRAPH_SELF_OUT)

# Load fixture graph into Oxigraph Graph Store endpoint.
load-fixture: graph-fixture
	go run ./cmd/loadnt -input $(GRAPH_FIXTURE_OUT) -oxigraph-url $(OXIGRAPH_UPDATE_URL)

# Load self-awareness graph into Oxigraph Graph Store endpoint.
load-self: graph-self
	go run ./cmd/loadnt -input $(GRAPH_SELF_OUT) -oxigraph-url $(OXIGRAPH_UPDATE_URL)

# Load the binary's currently-embedded release seed into a live Oxigraph
# additively (Graph Store POST merges into the default graph). Run this
# AFTER deploying a new awareness-graph version — seedIfEmpty skips load
# on a non-empty store by design, so a fresh binary needs this step to
# activate its new anchors. See docs/release-runbook.md step 6.
#
# Override OXIGRAPH_UPDATE_URL if Oxigraph is not on localhost:7878.
load-release-seed:
	go run ./cmd/loadnt -input ./golang/server/embeddata/awareness.nt -oxigraph-url $(OXIGRAPH_UPDATE_URL)

# Local operational smoke: start/verify Oxigraph, load self graph, then
# run a short require-store server startup smoke.
smoke-local: oxigraph graph-self
	go run ./cmd/loadnt -input $(GRAPH_SELF_OUT) -oxigraph-url $(OXIGRAPH_UPDATE_URL)
	timeout 2s go run ./golang/server -addr :19090 -oxigraph-url $(OXIGRAPH_QUERY_URL) -require-store; \
		code=$$?; \
		if [ $$code -ne 0 ] && [ $$code -ne 124 ]; then exit $$code; fi

tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "tools: installed protoc-gen-go + protoc-gen-go-grpc"
	@echo "note: protoc itself must come from your system package manager"

# Run the conformance scanner against the services repo. Implements
# step 3 of the CLAUDE.md PRINCIPLE EXTRACTION PROTOCOL ("SEARCH FOR
# SIBLINGS"). Exit 1 if any DRIFT or HIDDEN_WORKFLOW sites are found.
#
# `make principle-check` runs the primary principle. `make principle-check-all`
# runs every principle the scanner knows about. Adding a new principle
# is a YAML edit (declare a per-instance invariant with actor_writer_dirs +
# scan_pattern + exception_files), then add a target here.
#
# Override SERVICES_REPO if the services checkout is not at ../services.
SERVICES_REPO ?= ../services

# The ruleguard per-instance invariants whose matcher is proven functional
# by the positive-control attestation (principle-check-positive) AND which
# scan the services tree clean today. Gating them turns each from an
# authored-but-unrun scanner into an enforced regression tripwire.
#
# EXCLUDED on purpose:
#   - doctor_rule_evaluate_must_consult_snap_errors — too broad (flags
#     ~55 of 111 cluster_doctor Evaluate methods); the snap-error contract
#     is already gated behaviorally in services CI (TestNoRuleEmits...,
#     TestEvaluateAll_EmptyFindings_SurfacesSourceUnavailable). Static rule
#     stays exploratory, not gated.
RULEGUARD_INSTANCES := \
	connection.dial_err_must_carry_class_through_return_path \
	errorf_must_use_w_verb_to_preserve_err_chain \
	canskip_predicates_must_check_multiple_fields \
	heartbeat_must_not_take_non_critical_dependencies \
	isbootstrap_consumer_must_check_window \
	expected_sha256_param_must_carry_subject_name \
	cluster_event_must_carry_node_or_cluster_scope \
	deploy_self_install_must_not_be_break_glass \
	defer_in_for_loop_must_not_accumulate \
	installed_package.timestamp_writers_must_preserve_during_observe \
	convergence.restart_must_go_through_singleflight_gate \
	netutil.identity_getter_must_express_vip_ambiguity \
	rbac.getitem_consumers_must_handle_empty_payload \
	release_type_switch_must_have_default \
	panic_recovery_must_not_be_silent \
	error_path.no_unbounded_fire_and_forget_goroutine \
	retry_loop.repeated_error_log_must_be_deduplicated \
	ddl_statements_must_use_if_not_exists \
	deadline_exceeded_must_not_drive_definitive_node_state \
	ambiguous_int_typed_time_field_must_carry_unit_qualifier \
	timestamp_field_must_carry_semantic_qualifier \
	backend_must_check_originating_principal_not_only_peer_cert \
	cross_node_staleness_must_use_server_clock \
	hardcoded_set_must_derive_from_source

principle-check:
	go run ./cmd/principle-check \
		-principle meta.state_mutations_must_be_durably_committed_before_side_effects \
		-repo $(SERVICES_REPO) \
		-mode summary

# Multi-principle proof (v1, 2026-06-05): scan the workflow service
# itself for direct etcd writes. The workflow service is a router, not
# a writer; only SeedCoreWorkflows legitimately writes (exception 4).
principle-check-workflow-service:
	go run ./cmd/principle-check \
		-principle workflow.workflow_service_writes_only_own_runtime_state \
		-repo $(SERVICES_REPO) \
		-mode summary

# Fallback must degrade semantics: a fallback path must emit a DEGRADED
# finding, not a value shaped like authoritative truth. regex-mode scanner
# with real coverage (scans the fallback write sites). Gated 2026-06-11
# after confirming it scans live sites and passes clean.
principle-check-fallback:
	go run ./cmd/principle-check \
		-principle fallback.must_emit_degraded_finding \
		-repo $(SERVICES_REPO) \
		-mode summary

# Positive-control attestation for the ruleguard rules. Proves each rule
# fires on a known-bad fixture, so its zero-findings result against real
# code means "attested clean" rather than "uncharted / rule is dead."
# Requires `ruleguard` on PATH (the test skips, loudly, if it is absent —
# CI MUST install it for this to actually enforce). See
# meta.negative_result_requires_coverage_attestation.
principle-check-positive:
	go test ./cmd/principle-check -run TestRuleguardRulesHavePositiveControl -v

# Run every positive-control-attested ruleguard scanner against the
# services tree. Each exits non-zero on DRIFT; set -e fails the target on
# the first one. Builds the binary once, then loops (cheaper than `go run`
# per id). Requires ruleguard on PATH (CI installs it); without it
# principle-check errors loudly rather than passing — you cannot attest
# coverage with a missing analyzer.
principle-check-ruleguard-tree:
	@go build -o bin/principle-check ./cmd/principle-check
	@set -e; for id in $(RULEGUARD_INSTANCES); do \
		echo "== ruleguard scan: $$id =="; \
		bin/principle-check -principle $$id -repo $(SERVICES_REPO) -mode summary >/dev/null; \
	done; \
	echo "ruleguard-tree: all $(words $(RULEGUARD_INSTANCES)) attested scanners conform"

# Declaration-completeness gate for ARCHITECTURAL meta-principles that no
# code-shape scanner can check (graceful_degradation, and future ones like
# partition_response / bounded_staleness). Every in-scope service must NAME its
# stance in services docs/awareness/architectural_declarations.yaml; a missing
# declaration fails. `none` is allowed (with a reason) — a tracked gap, not a
# silent one. The companion intent node surfaces the requirement in briefings
# at edit time. See
# docs/intent/availability.serving_services_declare_degradation_mode.yaml.
principle-check-declarations:
	SERVICES_REPO=$(SERVICES_REPO) go test ./cmd/principle-check -run TestArchitecturalDeclarationsComplete -v

# Coverage attestation for the meta-principle SET itself (declare-then-conform
# one level up). Every meta.* must have a known enforcement tier: auto-derived
# code_scanner, or declared declaration / behavioral / planned / review_only in
# docs/awareness/meta_principle_coverage.yaml. A new principle cannot land
# unclassified — the AWG-owned coverage map maintains itself instead of needing
# a cross-repo roundtrip to find gaps. Run with -v to print the full map.
principle-check-coverage:
	SERVICES_REPO=$(SERVICES_REPO) go test ./cmd/principle-check -run TestMetaPrincipleCoverage -v

# Artifact-level gates: alert invariant naming, metric aggregation scope
# preservation, topology-change event emission. These scan YAML and Go
# source artifacts (not ASTs via ruleguard). Each is a Go test with
# carve-out lists for documented exceptions.
principle-check-artifacts:
	SERVICES_REPO=$(SERVICES_REPO) go test ./cmd/principle-check -run 'TestAlertsMustNameInvariant|TestMetricAggregationPreservesActionableLabels|TestTopologyChangeEmitsEvent|TestDestructiveTestGate' -v

# Run every declared principle in sequence. Each is its own scanner
# invocation; the combined exit status is the worst across them.
# Order: regex scanners, then the ruleguard-tree sweep, then the
# positive-control attestation that proves those ruleguard rules are alive
# (not silently dead) — then the declaration-completeness gate for the
# architectural principles that live above the code — then the artifact
# gates — then the coverage attestation that closes the loop.
# See meta.negative_result_requires_coverage_attestation.
principle-check-all: principle-check principle-check-workflow-service principle-check-fallback principle-check-ruleguard-tree principle-check-positive principle-check-declarations principle-check-artifacts principle-check-coverage

# ── Sensei standalone CLI ───────────────────────────────────────────

# Build the standalone sensei CLI binary. Also installs the deprecated `awg`
# alias (same binary; invoking it as awg prints a deprecation notice) so CI
# scripts and muscle memory keep working for one release.
sensei-cli:
	go build -ldflags "-X main.Version=$(SERVICE_VERSION)" -o ./bin/sensei ./cmd/awg
	cp ./bin/sensei ./bin/awg

# Backwards-compatible alias for the CLI-only build.
awg-cli: sensei-cli

# Build the sensei CLI and the awareness-graph server together.
# This is the canonical full build so the server binary always tracks the
# embedded seed and other server-side runtime behavior.
sensei: sensei-cli service-build

# Backwards-compatible aliases for the full build.
awg: sensei
awg-build: sensei

# Smoke test: init a temp project, check, build to file.
sensei-smoke: sensei
	@rm -rf /tmp/sensei-smoke-test
	@mkdir -p /tmp/sensei-smoke-test
	./bin/sensei init --dir /tmp/sensei-smoke-test --hooks=false --claude-md=false
	cd /tmp/sensei-smoke-test && $(CURDIR)/bin/sensei check
	cd /tmp/sensei-smoke-test && $(CURDIR)/bin/sensei build --output /tmp/sensei-smoke-test/.sensei/graph.nt
	@echo "sensei-smoke: PASS"
	@rm -rf /tmp/sensei-smoke-test

# Backwards-compatible alias for the smoke target.
awg-smoke: sensei-smoke

# scip — index the Go source with scip-go and ingest symbol-level nodes
# (functions, methods, types, and aw:references edges) into the curated
# awareness corpus as awareness_graph_scip_{symbols,references}.yaml, following
# the same generated-artifact convention as import-graph / proto-contracts.
#
# It uses the TARGETED `sensei scip-ingest` (not `sensei bootstrap`) so it only adds
# the symbol layer and leaves the hand-curated corpus — import graphs, proto
# contracts, annotation-based code symbols, tests — untouched. --exclude-tests
# drops *_test.go symbols, which are already modeled as Test nodes and would
# otherwise collide with the curated code_symbols.
#
# scip-go is used from PATH when present, else fetched via `go run`. Non-Go
# repos: run the matching indexer to produce index.scip, then `make scip`
# (SKIP_INDEX=1 reuses an existing index).
SCIP_REPO ?= .
SCIP_GO ?= go run github.com/scip-code/scip-go/cmd/scip-go@latest
scip: sensei-cli
ifneq ($(SKIP_INDEX),1)
	cd $(SCIP_REPO) && ( command -v scip-go >/dev/null 2>&1 && scip-go --output index.scip || $(SCIP_GO) --output index.scip )
endif
	./bin/sensei scip-ingest --scip $(SCIP_REPO)/index.scip --exclude-tests --out $(SCIP_REPO)/docs/awareness/generated
	mv $(SCIP_REPO)/docs/awareness/generated/code_symbols.yaml $(SCIP_REPO)/docs/awareness/generated/awareness_graph_scip_symbols.yaml
	mv $(SCIP_REPO)/docs/awareness/generated/code_references.yaml $(SCIP_REPO)/docs/awareness/generated/awareness_graph_scip_references.yaml
	@echo "scip: symbol-level nodes ingested → docs/awareness/generated/awareness_graph_scip_{symbols,references}.yaml"

clean:
	rm -f $(PB_OUT_DIR)/awareness_graph.pb.go
	rm -f $(PB_OUT_DIR)/awareness_graph_grpc.pb.go
	@echo "clean: removed generated proto bindings (run \`make proto\` to regenerate)"

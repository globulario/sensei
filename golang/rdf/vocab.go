// SPDX-License-Identifier: AGPL-3.0-only

// Package rdf carries the Go-side constants that mirror ontology/awareness.ttl.
//
// Drift between this file and the Turtle source is a real risk — the planned
// CI check (ontology_drift_test.go, not yet written) will diff IRIs in
// ontology/awareness.ttl against the constants below and fail the build on
// mismatch. Until that test lands, the convention is: edit the .ttl first,
// then update vocab.go to match. Never the reverse.
package rdf

// Namespace IRIs. The aw: namespace is owned by Globular; the rest are
// W3C-standard.
const (
	AwNS   = "https://globular.io/awareness#"
	RdfNS  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	RdfsNS = "http://www.w3.org/2000/01/rdf-schema#"
	OwlNS  = "http://www.w3.org/2002/07/owl#"
	XsdNS  = "http://www.w3.org/2001/XMLSchema#"
)

// Class IRIs. Use IRI() to render with angle brackets when emitting N-Triples.
const (
	ClassInvariant       = AwNS + "Invariant"
	ClassFailureMode     = AwNS + "FailureMode"
	ClassIncidentPattern = AwNS + "IncidentPattern"
	ClassIntent          = AwNS + "Intent"
	ClassForbiddenFix    = AwNS + "ForbiddenFix"
	ClassTest            = AwNS + "Test"
	ClassSourceFile      = AwNS + "SourceFile"
	ClassSymbol          = AwNS + "Symbol"
	ClassEtcdKey         = AwNS + "EtcdKey"
	ClassSystemdUnit     = AwNS + "SystemdUnit"

	// Phase B classes — added when importers were implemented.
	// The Turtle ontology (ontology/awareness.ttl) should be updated to
	// declare these as owl:Class entries.
	ClassContract  = AwNS + "Contract"  // versioned authority or service contract
	ClassDecision  = AwNS + "Decision"  // architecture decision record
	ClassGuardrail = AwNS + "Guardrail" // operational guardrail
	ClassPattern   = AwNS + "Pattern"   // design pattern (architectural intent)
	// ImplementationPattern is project-specific code recipe — distinct from
	// the abstract design pattern above. v1 keeps these as separate top-level
	// classes (no subClassOf relation) so existing Pattern queries are
	// unaffected. Subclass relation can be added later if/when queries need
	// to span both.
	ClassImplementationPattern = AwNS + "ImplementationPattern"
	ClassService               = AwNS + "Service"  // service catalogue entry
	ClassIncident              = AwNS + "Incident" // individual incident record

	// Architectural-spine classes (Stage A). MetaPrinciple is the existing
	// meta.* invariants, dual-typed by importInvariants — it keeps its
	// invariant IRI, so a meta-principle node is resolved against
	// ClassInvariant, not ClassMetaPrinciple. Component/Boundary/Evidence are
	// new authorable classes; Contract and Decision reuse the Phase-B classes.
	ClassMetaPrinciple = AwNS + "MetaPrinciple" // reusable architectural law (dual-typed meta.* invariant)
	ClassComponent     = AwNS + "Component"     // architectural unit of ownership
	ClassBoundary      = AwNS + "Boundary"      // place where architecture can be violated
	ClassEvidence      = AwNS + "Evidence"      // proof a rule/contract/component state is alive

	// Design-pattern awareness (the "how" layer). DesignPattern = general shape;
	// ImplementationPattern (declared above) = project realisation; PatternMisuse
	// = visible misuse. Distinct from the legacy generic ClassPattern catalogue.
	ClassDesignPattern = AwNS + "DesignPattern" // grounded project design pattern
	ClassPatternMisuse = AwNS + "PatternMisuse" // dangerous misuse of a pattern

	// Phase C intent subclasses — all are rdfs:subClassOf aw:Intent.
	ClassDesignIntent      = AwNS + "DesignIntent"      // principle, pattern
	ClassOperationalIntent = AwNS + "OperationalIntent" // mechanism, operator_model, implementation
	ClassProductIntent     = AwNS + "ProductIntent"     // vision
	ClassConstraintIntent  = AwNS + "ConstraintIntent"  // invariant, contract, safety_rule, constraint

	// Phase C code-annotation classes — added when code_symbols importer landed.
	ClassCodeSymbol = AwNS + "CodeSymbol" // annotated code symbol (function, method, type, file)
	ClassTestSymbol = AwNS + "TestSymbol" // test reference (file:TestName format)

	// Phase 2 outcome-feedback classes. Compiled/indexed knowledge that records
	// what was done and what happened, linking to authority nodes via
	// PropUsedKnowledgeNode. These NEVER assert authority themselves. AgentDecision,
	// ChangeSet, TestOutcome, RuntimeOutcome are reserved for richer v2 modelling;
	// the v1 importer emits OutcomeFeedback with scalar context + links.
	ClassOutcomeFeedback     = AwNS + "OutcomeFeedback"
	ClassAgentDecision       = AwNS + "AgentDecision"
	ClassChangeSet           = AwNS + "ChangeSet"
	ClassTestOutcome         = AwNS + "TestOutcome"
	ClassRuntimeOutcome      = AwNS + "RuntimeOutcome"
	ClassGraphUpdateProposal = AwNS + "GraphUpdateProposal"

	// Phase 3 authority-ownership classes. AuthorityDomain is the v1 queryable
	// node (flat literals, same flattening as ImplementationPattern); the
	// satellite classes are reserved for a v2 reified model.
	ClassAuthorityDomain  = AwNS + "AuthorityDomain"
	ClassAuthoritySurface = AwNS + "AuthoritySurface"
	ClassStateObject      = AwNS + "StateObject"
	ClassOwnerService     = AwNS + "OwnerService"
	ClassAllowedWriter    = AwNS + "AllowedWriter"
	ClassAllowedReader    = AwNS + "AllowedReader"
	ClassMutationPath     = AwNS + "MutationPath"
	ClassObservationPath  = AwNS + "ObservationPath"
	ClassTrustBoundary    = AwNS + "TrustBoundary"
	ClassEvidenceSource   = AwNS + "EvidenceSource"

	// Phase 2A repair-plan classes. RepairPlan is the v1 queryable node (flat
	// literals + link edges); the satellite classes are reserved for a v2
	// reified model.
	ClassRepairPlan          = AwNS + "RepairPlan"
	ClassRepairStep          = AwNS + "RepairStep"
	ClassPrecondition        = AwNS + "Precondition"
	ClassPostcondition       = AwNS + "Postcondition"
	ClassVerificationStep    = AwNS + "VerificationStep"
	ClassRollbackStep        = AwNS + "RollbackStep"
	ClassApprovalGate        = AwNS + "ApprovalGate"
	ClassBlastRadius         = AwNS + "BlastRadius"
	ClassRepairConfidence    = AwNS + "RepairConfidence"
	ClassRepairApplicability = AwNS + "RepairApplicability"

	// Phase 2C runtime-evidence classes. RuntimeEvidence is the v1 queryable
	// node; satellites are reserved.
	ClassRuntimeEvidence         = AwNS + "RuntimeEvidence"
	ClassEvidenceProbe           = AwNS + "EvidenceProbe"
	ClassEvidenceFreshnessWindow = AwNS + "EvidenceFreshnessWindow"
	ClassEvidenceTrustLevel      = AwNS + "EvidenceTrustLevel"
	ClassEvidenceConflict        = AwNS + "EvidenceConflict"
	ClassEvidenceOwnerPath       = AwNS + "EvidenceOwnerPath"

	// Phase 2G agent-accountability classes. AgentRun is the v1 queryable node;
	// satellites are reserved.
	ClassAgentRun        = AwNS + "AgentRun"
	ClassAgentScorecard  = AwNS + "AgentScorecard"
	ClassPreflightUse    = AwNS + "PreflightUse"
	ClassWarningIgnored  = AwNS + "WarningIgnored"
	ClassTestRunEvidence = AwNS + "TestRunEvidence"
	ClassPatchOutcome    = AwNS + "PatchOutcome"
	ClassLearningEvent   = AwNS + "LearningEvent"
	ClassProofObligation = AwNS + "ProofObligation"
	ClassProofSlot       = AwNS + "ProofSlot"
)

// Property IRIs.
const (
	PropType    = RdfNS + "type"
	PropLabel   = RdfsNS + "label"
	PropComment = RdfsNS + "comment"

	// Datatype properties — short literals only.
	//
	// PropAuthoredIn and PropAtFile are orthogonal, not aliases. AuthoredIn
	// = YAML/document origin (knowledge node's source-of-truth file).
	// AtFile = code location where an @-annotation was extracted. A node
	// may carry both. See ontology/awareness.ttl for the contract.
	PropSeverity   = AwNS + "severity"
	PropStatus     = AwNS + "status"
	PropAuthoredIn = AwNS + "authoredIn"
	PropAtFile     = AwNS + "atFile"
	PropAtSymbol   = AwNS + "atSymbol"
	PropAtLine     = AwNS + "atLine"

	// Domain-scope properties. A knowledge node belongs to exactly one domain
	// so truth never crosses repos accidentally. Globular's own self-knowledge
	// is DomainShared/its own repo domain; foreign repos (e.g. a cold-source
	// pilot for caddy) get their own repo domain. A query scoped to repo X
	// returns X's nodes PLUS DomainShared meta-principles, never another repo's.
	// See golang/server/scope.go for the resolution + fail-closed rules.
	PropDomain    = AwNS + "domain"    // "repo" | "shared"
	PropRepo      = AwNS + "repo"      // e.g. "github.com/caddyserver/caddy"
	PropSourceSet = AwNS + "sourceSet" // namespace within a domain, e.g. "pilot/caddy"
	PropOrigin    = AwNS + "origin"    // "coldsource" | "authored" | ...

	// Promotion provenance. A repo-scoped rule that entered the graph through
	// the pilot promotion path carries the receipt of HOW it was earned: the
	// cold-source bundle it came from, the commit range that bundle scanned,
	// the literal citations (PR review comment / commit refs) that support it,
	// and the human review label assigned at promotion. These are bounded
	// literals attached to the node so a briefing can show provenance and an
	// auditor can trace any foreign rule back to its evidence. They are NOT
	// read by the scope filter (only aw:repo / aw:domain are) — they preserve
	// the chain of custody, they do not grant authority.
	PropProvenanceBundleID    = AwNS + "provenanceBundleId"    // cold-source bundle id, e.g. "caddy-reverseproxy-2026-06"
	PropProvenanceCommitRange = AwNS + "provenanceCommitRange" // git range scanned, e.g. "HEAD~500..HEAD"
	PropProvenanceCitation    = AwNS + "provenanceCitation"    // repeatable: a PR/comment/commit reference supporting the rule
	PropReviewLabel           = AwNS + "reviewLabel"           // human label at promotion: "load-bearing" | "shallow" | ...

	// Detect block — advisory rule metadata for warning-level enforcement.
	// A rule MAY declare a narrow, deterministic bad-shape signal that the
	// EditCheck path matches against proposed edit content. This is advisory
	// (warning-only): no blocking, no CI gate, no automatic edits. The
	// patterns are Go regexps; an unparsable pattern is skipped, never fatal.
	PropDetectForbiddenPattern = AwNS + "detectForbiddenPattern" // regexp whose presence in the edit warns
	PropDetectRequiredPattern  = AwNS + "detectRequiredPattern"  // regexp whose ABSENCE in the edit warns
	PropDetectAppliesToPath    = AwNS + "detectAppliesToPath"    // repeatable glob; restricts which files the rule evaluates
	PropDetectMessage          = AwNS + "detectMessage"          // operator-facing advice shown with the warning
	PropDetectEnforcement      = AwNS + "detectEnforcement"      // "warn" (default/advisory) | "block" (would-block under a hard gate)

	// DomainShared is the reserved domain value for portable meta-principles
	// that may surface in any repo's briefing. Repo-specific instances must NOT
	// use it — they carry their own repo domain.
	DomainShared = "shared"
	DomainRepo   = "repo"

	// Anchoring object properties.
	//
	// Forward direction (Invariant/Pattern → file/symbol/etc) carries the
	// role distinction. The single reverse predicate aw:implements is
	// emitted from the file back to the knowledge node so impact queries
	// starting at a file can land its Direct anchors uniformly. See
	// ontology/awareness.ttl for the rationale.
	PropProtects   = AwNS + "protects"
	PropEnforces   = AwNS + "enforces"
	PropConfigures = AwNS + "configures"
	PropObserves   = AwNS + "observes"
	PropMayAffect  = AwNS + "mayAffect"
	PropImplements = AwNS + "implements"

	// Cross-node object properties.
	//
	// PropAffects is the single generic edge between knowledge nodes —
	// invariant→failure_mode, failure_mode→invariant, and
	// incident_pattern→invariant all use it. Type-filter the object class
	// in SPARQL when a query needs to disambiguate. The v0.0 ontology had
	// a parallel PropRelatedInvariant predicate; v0.1 eliminates it.
	PropForbids      = AwNS + "forbids"
	PropRequiresTest = AwNS + "requiresTest"
	PropAffects      = AwNS + "affects"
	PropExemplifies  = AwNS + "exemplifies"

	// Intent hierarchy — populated by Phase C importer.
	PropZoomsInto         = AwNS + "zoomsInto"
	PropZoomsOutTo        = AwNS + "zoomsOutTo"
	PropExpressedBy       = AwNS + "expressedBy"
	PropRelatedTo         = AwNS + "relatedTo"
	PropLevel             = AwNS + "level"
	PropActivationTrigger = AwNS + "activationTrigger"
	PropBadSmell          = AwNS + "badSmell"

	// Code-annotation properties — added with code_symbols importer.
	// PropProtectsAgainst is distinct from PropProtects: the latter anchors
	// an Invariant to a SourceFile; the former records that a CodeSymbol
	// guards against a specific FailureMode.
	PropRisk            = AwNS + "risk"            // low | medium | high
	PropTestedBy        = AwNS + "testedBy"        // CodeSymbol → TestSymbol
	PropDefinedInFile   = AwNS + "definedInFile"   // CodeSymbol → SourceFile
	PropProtectsAgainst = AwNS + "protectsAgainst" // CodeSymbol → FailureMode/Invariant
	PropLanguage        = AwNS + "language"        // SourceFile/CodeSymbol → go | typescript | ...
	PropMemberOfGroup   = AwNS + "memberOfGroup"   // SourceFile → RenderingGroup
	PropReferences      = AwNS + "references"      // CodeSymbol → CodeSymbol (call/use edge, from SCIP occurrences)

	// PropPartiallyViolates records a code site that KNOWINGLY violates
	// part of an Invariant. Distinct from PropRelatedTo (which carries
	// no judgment) and PropForbids (which marks code that MUST NOT
	// exist). A partially_violates edge is documentation: the author
	// has decided the trade-off and named the violated principle so a
	// future reader can locate the gap and either lift it or extend
	// the exception with new evidence.
	PropPartiallyViolates = AwNS + "partiallyViolates" // CodeSymbol/SourceFile → Invariant

	// Implementation-pattern properties — added with ImplementationPattern
	// class. These are object-shape rules the pattern declares as required
	// or forbidden when an agent writes code matching the pattern's domain.
	PropMustFollow    = AwNS + "mustFollow"    // human-readable step the pattern enforces
	PropRequiresCall  = AwNS + "requiresCall"  // symbol/function name that MUST appear (e.g. globular.InitClient)
	PropForbidsCall   = AwNS + "forbidsCall"   // symbol/function name that MUST NOT appear (e.g. grpc.Dial)
	PropReferenceFile = AwNS + "referenceFile" // canonical example: literal path "role:repo-relative/path.go"

	// Outcome-feedback properties (Phase 2). PropUsedKnowledgeNode is the one
	// object property — OutcomeFeedback → authority node IRI. The rest are
	// scalar context literals.
	PropUsedKnowledgeNode    = AwNS + "usedKnowledgeNode"    // OutcomeFeedback → Invariant/FailureMode/ImplementationPattern/Test/... IRI
	PropForTask              = AwNS + "forTask"              // task text the outcome pertains to
	PropForFinding           = AwNS + "forFinding"           // cluster-doctor FindingID
	PropForWorkflowRun       = AwNS + "forWorkflowRun"       // workflow run id
	PropForStep              = AwNS + "forStep"              // workflow step id
	PropUsedPreflightStatus  = AwNS + "usedPreflightStatus"  // preflight status in effect (OK|DEGRADED|EMPTY)
	PropUsedRiskClass        = AwNS + "usedRiskClass"        // preflight risk class in effect
	PropDecision             = AwNS + "decision"             // applied | rejected | deferred | reverted
	PropOutcomeStatus        = AwNS + "outcomeStatus"        // success | failure | blocked | reverted
	PropFailureClass         = AwNS + "failureClass"         // classified failure category
	PropReasonCode           = AwNS + "reasonCode"           // machine-readable reason code
	PropObservedAt           = AwNS + "observedAt"           // ISO date/time literal
	PropSuggestsCandidate    = AwNS + "suggestsCandidate"    // candidate node for human review (inert)
	PropPromotedFromIncident = AwNS + "promotedFromIncident" // source incident id

	// Authority-domain properties (Phase 3). v1 flattening: all literals on
	// the AuthorityDomain node so Preflight surfaces them without traversal.
	PropCoversPath                 = AwNS + "coversPath"                 // repo-relative path prefix matched against touched files
	PropOwnerService               = AwNS + "ownerService"               // service that owns the domain's state
	PropOwnsState                  = AwNS + "ownsState"                  // a state object the domain owns
	PropMayWrite                   = AwNS + "mayWrite"                   // writer allowed to mutate the state
	PropMayRead                    = AwNS + "mayRead"                    // reader allowed to read the state
	PropMustMutateVia              = AwNS + "mustMutateVia"              // legal mutation path (typed RPC / workflow)
	PropMustReadVia                = AwNS + "mustReadVia"                // legal read path
	PropObservesVia                = AwNS + "observesVia"                // legal observation path for runtime evidence
	PropHasTruthLayer              = AwNS + "hasTruthLayer"              // repository | desired | installed | runtime
	PropHasEvidenceFreshnessWindow = AwNS + "hasEvidenceFreshnessWindow" // freshness requirement for evidence about this domain
	PropForbidsBypass              = AwNS + "forbidsBypass"              // named illegal shortcut around the authority

	// Repair-plan properties (Phase 2A). Object props (FailureMode/AuthorityDomain/
	// ImplementationPattern/Invariant IRIs) bind the plan; datatype props carry
	// ordered steps and risk/approval labels. PropRequiresTest is reused.
	PropRepairsFailureMode        = AwNS + "repairsFailureMode"        // RepairPlan -> FailureMode IRI
	PropRepairsFindingClass       = AwNS + "repairsFindingClass"       // RepairPlan -> finding-class literal
	PropAppliesToAuthorityDomain  = AwNS + "appliesToAuthorityDomain"  // RepairPlan -> AuthorityDomain IRI
	PropRequiresPrecondition      = AwNS + "requiresPrecondition"      // RepairPlan -> precondition literal
	PropHasRepairStep             = AwNS + "hasRepairStep"             // RepairPlan -> ordered repair-step literal
	PropRequiresVerification      = AwNS + "requiresVerification"      // RepairPlan -> verification literal
	PropHasRollbackStep           = AwNS + "hasRollbackStep"           // RepairPlan -> rollback literal
	PropRequiresApprovalGate      = AwNS + "requiresApprovalGate"      // RepairPlan -> approval-gate label
	PropHasBlastRadius            = AwNS + "hasBlastRadius"            // RepairPlan -> blast-radius label
	PropHasConfidence             = AwNS + "hasConfidence"             // RepairPlan -> confidence label
	PropUsesImplementationPattern = AwNS + "usesImplementationPattern" // RepairPlan -> ImplementationPattern IRI
	PropMustNotViolateInvariant   = AwNS + "mustNotViolateInvariant"   // RepairPlan -> Invariant IRI
	PropGovernedByContract        = AwNS + "governedByContract"        // RepairPlan -> Contract IRI
	PropProducesOutcomeFeedback   = AwNS + "producesOutcomeFeedback"   // RepairPlan -> outcome note literal
	PropRequiresRuntimeEvidence   = AwNS + "requiresRuntimeEvidence"   // RepairPlan -> runtime-evidence requirement literal

	// Runtime-evidence properties (Phase 2C). Object props bind to the
	// invariant/repair-plan/authority domain verified; datatype props carry the
	// owner path, freshness, trust, and the stale-must-not-PASS rule.
	PropEvidenceForInvariant         = AwNS + "evidenceForInvariant"
	PropEvidenceForRepairPlan        = AwNS + "evidenceForRepairPlan"
	PropEvidenceForAuthorityDomain   = AwNS + "evidenceForAuthorityDomain"
	PropObservedFromService          = AwNS + "observedFromService"
	PropObservedViaPath              = AwNS + "observedViaPath"
	PropHasFreshnessWindow           = AwNS + "hasFreshnessWindow"
	PropHasTrustLevel                = AwNS + "hasTrustLevel"
	PropExpiresAfter                 = AwNS + "expiresAfter"
	PropConflictsWithEvidence        = AwNS + "conflictsWithEvidence"
	PropMustComeFromOwnerPath        = AwNS + "mustComeFromOwnerPath"
	PropCannotPromoteToPassWhenStale = AwNS + "cannotPromoteToPassWhenStale"
	PropAppliesToAuthoritySurface    = AwNS + "appliesToAuthoritySurface"
	PropDerivedFromAuthoritySurface  = AwNS + "derivedFromAuthoritySurface"
	PropDerivedFromStatus            = AwNS + "derivedFromStatus"
	PropHasEvidenceLane              = AwNS + "hasEvidenceLane"
	PropRequiresProofSlot            = AwNS + "requiresProofSlot"
	PropSlotKind                     = AwNS + "slotKind"
	PropRequired                     = AwNS + "required"

	// Knowledge scoring properties (Phase 2D). Generic across node classes.
	PropConfidence      = AwNS + "confidence"      // high | medium | low | unknown
	PropFreshness       = AwNS + "freshness"       // current | stale | unknown | historical
	PropSourceKind      = AwNS + "sourceKind"      // manual|incident|outcome|scanner|test|runtime|generated_candidate
	PropSourcePath      = AwNS + "sourcePath"      // derivation source path
	PropAcceptedBy      = AwNS + "acceptedBy"      // who promoted it
	PropLastValidatedAt = AwNS + "lastValidatedAt" // ISO literal
	PropStaleAfter      = AwNS + "staleAfter"      // re-validation window
	PropPromotionStatus = AwNS + "promotionStatus" // candidate|proposed|accepted|active|deprecated|superseded
	PropSupersededBy    = AwNS + "supersededBy"    // node -> replacement node IRI
	PropConflictsWith   = AwNS + "conflictsWith"   // node -> conflicting node IRI

	// Agent-accountability properties (Phase 2G).
	PropAgentName                 = AwNS + "agentName"
	PropModelName                 = AwNS + "modelName"
	PropTaskSummary               = AwNS + "taskSummary"
	PropUsedPreflight             = AwNS + "usedPreflight"
	PropPreflightStatus           = AwNS + "preflightStatus"
	PropWarningsIgnored           = AwNS + "warningsIgnored"
	PropTestsRequired             = AwNS + "testsRequired"
	PropTestsRun                  = AwNS + "testsRun"
	PropTestsSkipped              = AwNS + "testsSkipped"
	PropPatchStatus               = AwNS + "patchStatus"
	PropCausedIncident            = AwNS + "causedIncident"
	PropResolvedIncident          = AwNS + "resolvedIncident"
	PropCreatedOutcomeFeedback    = AwNS + "createdOutcomeFeedback" // AgentRun -> OutcomeFeedback IRI
	PropCreatedCandidateKnowledge = AwNS + "createdCandidateKnowledge"
	PropMode                      = AwNS + "mode"
	PropRunSignature              = AwNS + "runSignature"
	PropLearningEvidence          = AwNS + "learningEvidence"
	PropLearningAllowed           = AwNS + "learningAllowed"
	PropPromotionAllowed          = AwNS + "promotionAllowed"
	PropCertificationStatus       = AwNS + "certificationStatus"
	PropCertifiable               = AwNS + "certifiable"
	PropHumanReviewRequired       = AwNS + "humanReviewRequired"
	PropPrimaryFailureMode        = AwNS + "primaryFailureMode"
	PropCurrentScore              = AwNS + "currentScore"
	PropPreviousScore             = AwNS + "previousScore"
	PropMissingEvidence           = AwNS + "missingEvidence"
	PropProofRequired             = AwNS + "proofRequired"
	PropRequiredTestPath          = AwNS + "requiredTestPath"
	PropRequiredTestSymbol        = AwNS + "requiredTestSymbol"
	PropNoNewTestsMeans           = AwNS + "noNewTestsMeans"

	// Architectural-spine properties (Stage A). Datatype facets + object edges
	// that wire the spine. Edges reuse PropProtects/PropForbids/PropRequiresTest/
	// PropAffects/PropSupersededBy where one already fits; linking never types
	// the target.
	PropKind            = AwNS + "kind"            // component/boundary/contract/evidence kind facet
	PropAssertionMethod = AwNS + "assertionMethod" // declared | inferred
	PropReadOrWrite     = AwNS + "readOrWrite"     // contract: read | write | read_write | unknown
	PropStability       = AwNS + "stability"       // contract: stable | experimental | internal | deprecated
	PropCommand         = AwNS + "command"         // evidence: CLI command/probe that produced it
	PropAnchoredIn      = AwNS + "anchoredIn"      // spine node -> SourceFile | CodeSymbol

	// MetaPrinciple edges (attached to the meta.* invariant IRI).
	PropGenerates  = AwNS + "generates"  // MetaPrinciple -> Invariant
	PropConstrains = AwNS + "constrains" // MetaPrinciple -> Decision
	PropAppliesTo  = AwNS + "appliesTo"  // MetaPrinciple -> Component | Boundary | Contract
	PropExplains   = AwNS + "explains"   // MetaPrinciple -> Intent

	// Component edges.
	PropOwnsInvariant          = AwNS + "ownsInvariant"          // Component -> Invariant
	PropImplementsIntent       = AwNS + "implementsIntent"       // Component -> Intent
	PropExposesContract        = AwNS + "exposesContract"        // Component | Boundary -> Contract
	PropDependsOn              = AwNS + "dependsOn"              // Component -> Component
	PropReadsFrom              = AwNS + "readsFrom"              // Component -> Component
	PropWritesTo               = AwNS + "writesTo"               // Component -> Component
	PropProtectedByBoundary    = AwNS + "protectedByBoundary"    // Component -> Boundary
	PropSatisfiesMetaPrinciple = AwNS + "satisfiesMetaPrinciple" // Component | Contract -> MetaPrinciple (meta.* invariant IRI)
	PropViolatesMetaPrinciple  = AwNS + "violatesMetaPrinciple"  // Component -> MetaPrinciple (meta.* invariant IRI)

	// Boundary edges.
	PropSeparates    = AwNS + "separates"    // Boundary -> Component
	PropVulnerableTo = AwNS + "vulnerableTo" // Boundary -> FailureMode

	// Contract edges.
	PropExposedBy              = AwNS + "exposedBy"              // Contract -> Component
	PropConsumedBy             = AwNS + "consumedBy"             // Contract -> Component
	PropConstrainedByInvariant = AwNS + "constrainedByInvariant" // Contract -> Invariant

	// Spine ligament — the contract layering missing between layers.
	// Phase 1 (failure side): a known failure mode is the violation of an
	// architectural contract, so a resolved failure points up to the contract it
	// breaks, which in turn points to its constraining invariant and required
	// evidence ("not just a failing test — a violation of contract X").
	PropViolatesContract = AwNS + "violatesContract" // FailureMode -> Contract (architectural)
	PropViolatedBy       = AwNS + "violatedBy"       // Contract -> FailureMode (reverse of violatesContract)
	// Phase 2 (implementation side): the gRPC/REST surface is the executable
	// exposure of a semantic promise. realizesContract is authoritative; the
	// candidate form is produced by path/name overlap and must be promoted, never
	// auto-trusted, so the graph does not hallucinate architecture.
	PropRealizesContract          = AwNS + "realizesContract"          // ImplementationContract -> ArchitecturalContract
	PropRealizedByContract        = AwNS + "realizedByContract"        // ArchitecturalContract -> ImplementationContract (reverse of realizesContract)
	PropCandidateRealizesContract = AwNS + "candidateRealizesContract" // candidate impl->arch link (promote-only)

	// Decision edges (aw:affects -> Invariant stays from the existing importer).
	PropDefinesBoundary  = AwNS + "definesBoundary"  // Decision -> Boundary
	PropDefinesContract  = AwNS + "definesContract"  // Decision -> Contract
	PropAffectsComponent = AwNS + "affectsComponent" // Decision -> Component
	PropMitigates        = AwNS + "mitigates"        // Decision -> FailureMode
	PropRejects          = AwNS + "rejects"          // Decision -> ForbiddenFix

	// Evidence edges + its inverse.
	PropSupportedByEvidence = AwNS + "supportedByEvidence" // Invariant|Decision|Contract|Component -> Evidence
	PropSupports            = AwNS + "supports"            // Evidence -> Invariant | Decision | Contract
	PropValidatesComponent  = AwNS + "validatesComponent"  // Evidence -> Component
	PropConfirms            = AwNS + "confirms"            // Evidence -> FailureMode
	PropProducedByTest      = AwNS + "producedByTest"      // Evidence -> Test
	PropStaleFor            = AwNS + "staleFor"            // Evidence -> Invariant | Contract | Component

	// UML profile — optional classification metadata (literals). UML is
	// metadata, never authority: the AWG class/relations stay canonical.
	PropUmlKind       = AwNS + "umlKind"       // UML metaclass: Component | Interface | Operation | Constraint | Artifact | Signal | ...
	PropUmlStereotype = AwNS + "umlStereotype" // free-form stereotype (snake_case when generated)
	PropUmlView       = AwNS + "umlView"       // structural | behavioral | interaction | deployment | awareness
	PropUmlNotes      = AwNS + "umlNotes"      // optional short note
	PropUmlConfidence = AwNS + "umlConfidence" // declared | inferred

	// Design-pattern relations + facets. Reuse aw:appliesTo/protects/mitigates/
	// forbids/requiresTest/supportedByEvidence/anchoredIn/satisfiesMetaPrinciple/
	// kind(category)/assertionMethod(confidence)/mustFollow(required steps).
	PropAppliesWhen       = AwNS + "appliesWhen"       // DesignPattern: when to use
	PropDoesNotApplyWhen  = AwNS + "doesNotApplyWhen"  // DesignPattern: mandatory negative rule
	PropTradeoffs         = AwNS + "tradeoffs"         // DesignPattern: forces/tradeoffs
	PropForbiddenShortcut = AwNS + "forbiddenShortcut" // ImplementationPattern: shortcut to avoid

	PropRecommends         = AwNS + "recommends"         // MetaPrinciple -> DesignPattern
	PropRealizes           = AwNS + "realizes"           // ImplementationPattern -> DesignPattern
	PropRealizedBy         = AwNS + "realizedBy"         // DesignPattern -> ImplementationPattern (reverse)
	PropShapes             = AwNS + "shapes"             // DesignPattern -> Contract
	PropSatisfiesInvariant = AwNS + "satisfiesInvariant" // DesignPattern|ImplementationPattern -> Invariant
	PropChosenBy           = AwNS + "chosenBy"           // DesignPattern -> Decision
	PropUsedByComponent    = AwNS + "usedByComponent"    // ImplementationPattern -> Component
	PropImplementsDecision = AwNS + "implementsDecision" // ImplementationPattern -> Decision
	PropEnforcesContract   = AwNS + "enforcesContract"   // ImplementationPattern -> Contract
	PropPrevents           = AwNS + "prevents"           // ImplementationPattern -> FailureMode
	PropBlocks             = AwNS + "blocks"             // ImplementationPattern -> ForbiddenFix|PatternMisuse
	PropMisuses            = AwNS + "misuses"            // PatternMisuse -> DesignPattern
	PropSaferPattern       = AwNS + "saferPattern"       // PatternMisuse -> DesignPattern
	PropForbiddenBy        = AwNS + "forbiddenBy"        // PatternMisuse -> MetaPrinciple|ForbiddenFix
	PropViolatesInvariant  = AwNS + "violatesInvariant"  // PatternMisuse -> Invariant
	PropCauses             = AwNS + "causes"             // PatternMisuse -> FailureMode
	PropAvoidedBy          = AwNS + "avoidedBy"          // PatternMisuse -> ImplementationPattern
	PropRelatedPattern     = AwNS + "relatedPattern"     // any node -> DesignPattern|ImplementationPattern|PatternMisuse (reverse)
)

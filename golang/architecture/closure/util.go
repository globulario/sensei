// SPDX-License-Identifier: Apache-2.0

package closure

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/rdf"
)

var (
	sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)
	hexLenRE = map[int]*regexp.Regexp{}
)

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func isDimension(v string) bool {
	for _, d := range DimensionOrder {
		if v == d {
			return true
		}
	}
	return false
}

func cleanList(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func cleanPathList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = normalizePath(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return cleanList(out)
}

func normalizePath(s string) string {
	s = strings.TrimSpace(filepath.ToSlash(s))
	if s == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(s)))
}

func safeRelPath(path string) bool {
	path = normalizePath(path)
	return path != "" && !filepath.IsAbs(path) && path != ".." && !strings.HasPrefix(path, "../") && !strings.Contains(path, "/../")
}

func normalizeSinglePath(s string) string {
	out := cleanPathList([]string{s})
	if len(out) == 0 {
		return ""
	}
	return out[0]
}

func isSHA256(v string) bool {
	return sha256RE.MatchString(strings.TrimSpace(v))
}

func isHexLen(v string, n int) bool {
	re, ok := hexLenRE[n]
	if !ok {
		re = regexp.MustCompile(`^[a-f0-9]{` + strconv.Itoa(n) + `}$`)
		hexLenRE[n] = re
	}
	return re.MatchString(strings.TrimSpace(v))
}

func hasDuplicates(in []string) bool {
	seen := map[string]bool{}
	for _, s := range in {
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}

func duplicateNormalized(in []string) bool {
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}

func duplicateNormalizedPaths(in []string) bool {
	seen := map[string]bool{}
	for _, s := range in {
		s = normalizePath(s)
		if s == "" {
			continue
		}
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}

func contains(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		if seen[x] {
			return true
		}
	}
	return false
}

func dimensionRank(dim string) int {
	for i, d := range DimensionOrder {
		if d == dim {
			return i
		}
	}
	return len(DimensionOrder)
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func dedupeReasons(in []Reason) []Reason {
	seen := map[string]Reason{}
	var keys []string
	for _, r := range in {
		r.Code = strings.TrimSpace(r.Code)
		r.Detail = strings.TrimSpace(r.Detail)
		if r.Code == "" {
			continue
		}
		key := r.Code + "\x00" + r.Detail
		if _, ok := seen[key]; !ok {
			seen[key] = r
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]Reason, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func normalizeScopeReceipt(in ScopeReceipt) ScopeReceipt {
	r := in
	r.Files = cleanPathList(r.Files)
	r.RepresentedFiles = normalizeFileRepresentations(r.RepresentedFiles)
	r.Symbols = cleanList(r.Symbols)
	r.Components = cleanList(r.Components)
	r.ClaimIDs = cleanList(r.ClaimIDs)
	r.PropositionKeys = cleanList(r.PropositionKeys)
	r.NodeIDs = cleanList(r.NodeIDs)
	r.MissingFiles = cleanPathList(r.MissingFiles)
	r.MissingSymbols = cleanList(r.MissingSymbols)
	r.MissingComponents = cleanList(r.MissingComponents)
	r.MissingClaims = cleanList(r.MissingClaims)
	r.MissingPropositions = cleanList(r.MissingPropositions)
	return r
}

func normalizeFileRepresentations(in []FileRepresentationReceipt) []FileRepresentationReceipt {
	if len(in) == 0 {
		return nil
	}
	type key struct {
		path string
		kind string
	}
	merged := map[key]FileRepresentationReceipt{}
	for _, item := range in {
		path := normalizePath(item.Path)
		kind := strings.TrimSpace(item.RepresentationKind)
		if path == "" || kind == "" {
			continue
		}
		k := key{path: path, kind: kind}
		cur := merged[k]
		cur.Path = path
		cur.RepresentationKind = kind
		cur.AnchorNodeIDs = append(cur.AnchorNodeIDs, item.AnchorNodeIDs...)
		merged[k] = cur
	}
	if len(merged) == 0 {
		return nil
	}
	keys := make([]key, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		return keys[i].kind < keys[j].kind
	})
	out := make([]FileRepresentationReceipt, 0, len(keys))
	for _, k := range keys {
		item := merged[k]
		item.AnchorNodeIDs = cleanList(item.AnchorNodeIDs)
		out = append(out, item)
	}
	return out
}

func indexedClass(iri string) string {
	switch iri {
	case rdf.ClassSourceFile:
		return "source_file"
	case rdf.ClassSymbol:
		return "symbol"
	case rdf.ClassCodeSymbol:
		return "code_symbol"
	case rdf.ClassComponent:
		return "component"
	case rdf.ClassBoundary:
		return "boundary"
	case rdf.ClassContract:
		return "contract"
	case rdf.ClassInvariant:
		return "invariant"
	case rdf.ClassFailureMode:
		return "failure_mode"
	case rdf.ClassForbiddenFix:
		return "forbidden_fix"
	case rdf.ClassTest:
		return "test"
	case rdf.ClassIntent, rdf.ClassDesignIntent, rdf.ClassOperationalIntent, rdf.ClassProductIntent, rdf.ClassConstraintIntent:
		return "intent"
	case rdf.ClassDecision:
		return "decision"
	case rdf.ClassIncident:
		return "incident"
	case rdf.ClassEvidence:
		return "evidence"
	case rdf.ClassRuntimeEvidence:
		return "runtime_evidence"
	case rdf.ClassAuthorityDomain:
		return "authority_domain"
	case rdf.ClassStateObject:
		return "state_object"
	case rdf.ClassRepairPlan:
		return "repair_plan"
	default:
		return ""
	}
}

func sortedClassSet(set map[string]bool) []string {
	order := []string{"source_file", "code_symbol", "symbol", "component", "boundary", "contract", "invariant", "failure_mode", "forbidden_fix", "test", "intent", "decision", "incident", "evidence", "runtime_evidence", "authority_domain", "state_object", "repair_plan"}
	var out []string
	for _, c := range order {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

func nodeID(iri string, classes []string) string {
	for _, class := range classes {
		prefix := classPrefix(class)
		if prefix != "" && strings.HasPrefix(iri, prefix) {
			return rdf.DecodeIRIPath(iri[len(prefix):])
		}
	}
	if i := strings.LastIndex(iri, "/"); i >= 0 {
		return iri[i+1:]
	}
	if i := strings.LastIndex(iri, "#"); i >= 0 {
		return iri[i+1:]
	}
	return iri
}

func classPrefix(class string) string {
	classIRI := ""
	switch class {
	case "source_file":
		classIRI = rdf.ClassSourceFile
	case "symbol":
		classIRI = rdf.ClassSymbol
	case "code_symbol":
		classIRI = rdf.ClassCodeSymbol
	case "component":
		classIRI = rdf.ClassComponent
	case "boundary":
		classIRI = rdf.ClassBoundary
	case "contract":
		classIRI = rdf.ClassContract
	case "invariant":
		classIRI = rdf.ClassInvariant
	case "failure_mode":
		classIRI = rdf.ClassFailureMode
	case "forbidden_fix":
		classIRI = rdf.ClassForbiddenFix
	case "test":
		classIRI = rdf.ClassTest
	case "intent":
		classIRI = rdf.ClassIntent
	case "decision":
		classIRI = rdf.ClassDecision
	case "incident":
		classIRI = rdf.ClassIncident
	case "evidence":
		classIRI = rdf.ClassEvidence
	case "runtime_evidence":
		classIRI = rdf.ClassRuntimeEvidence
	case "authority_domain":
		classIRI = rdf.ClassAuthorityDomain
	case "state_object":
		classIRI = rdf.ClassStateObject
	case "repair_plan":
		classIRI = rdf.ClassRepairPlan
	}
	if classIRI == "" {
		return ""
	}
	return strings.TrimSuffix(strings.Trim(rdf.MintIRI(classIRI, ""), "<>"), "/") + "/"
}

func normalizeNode(n Node) Node {
	n.Classes = cleanList(n.Classes)
	n.Status = strings.TrimSpace(strings.ToLower(n.Status))
	n.PromotionStatus = strings.TrimSpace(strings.ToLower(n.PromotionStatus))
	n.ReviewStatus = strings.TrimSpace(strings.ToLower(n.ReviewStatus))
	n.SourceKind = strings.TrimSpace(strings.ToLower(n.SourceKind))
	n.AuthoredIn = cleanPathList(n.AuthoredIn)
	n.AnchoredIn = cleanList(n.AnchoredIn)
	n.CoversPath = cleanPathList(n.CoversPath)
	n.OwnerServices = cleanList(n.OwnerServices)
	n.OwnsStates = cleanList(n.OwnsStates)
	n.MayWrite = cleanList(n.MayWrite)
	n.MayRead = cleanList(n.MayRead)
	n.MustMutateVia = cleanList(n.MustMutateVia)
	n.MustReadVia = cleanList(n.MustReadVia)
	n.ObservesVia = cleanList(n.ObservesVia)
	n.TruthLayers = cleanList(n.TruthLayers)
	n.ForbidsBypass = cleanList(n.ForbidsBypass)
	n.DependsOn = cleanList(n.DependsOn)
	n.ReadsFrom = cleanList(n.ReadsFrom)
	n.WritesTo = cleanList(n.WritesTo)
	n.ProtectedByBoundaries = cleanList(n.ProtectedByBoundaries)
	n.ExposesContracts = cleanList(n.ExposesContracts)
	n.Separates = cleanList(n.Separates)
	n.ExposedBy = cleanList(n.ExposedBy)
	n.ConsumedBy = cleanList(n.ConsumedBy)
	n.ConstrainedByInvariants = cleanList(n.ConstrainedByInvariants)
	n.RequiresTests = cleanList(n.RequiresTests)
	n.SupportedByEvidence = cleanList(n.SupportedByEvidence)
	n.Forbids = cleanList(n.Forbids)
	n.VulnerableTo = cleanList(n.VulnerableTo)
	return n
}

func hasClass(n Node, class string) bool { return contains(n.Classes, class) }

func CanonicallyRepresentsFile(graph GraphIndex, node Node, requestedPath, repoRoot string) bool {
	_, ok := CanonicalFileRepresentation(graph, node, requestedPath, repoRoot)
	return ok
}

func CanonicalFileRepresentation(graph GraphIndex, node Node, requestedPath, repoRoot string) (FileRepresentationReceipt, bool) {
	_ = graph
	path := normalizePath(requestedPath)
	if path == "" {
		return FileRepresentationReceipt{}, false
	}
	if hasClass(node, "source_file") && node.SourcePath == path {
		return FileRepresentationReceipt{
			Path:               path,
			RepresentationKind: "source_file",
			AnchorNodeIDs:      []string{node.ID},
		}, true
	}
	if !eligibleGovernedAuthoredSource(node) || !contains(node.AuthoredIn, path) || !repoHasRegularFile(repoRoot, path) {
		return FileRepresentationReceipt{}, false
	}
	return FileRepresentationReceipt{
		Path:               path,
		RepresentationKind: "governed_authored_source",
		AnchorNodeIDs:      []string{node.ID},
	}, true
}

func eligibleGovernedAuthoredSource(node Node) bool {
	if len(node.AuthoredIn) == 0 || !hasAnyClass(node, "decision", "intent", "invariant", "failure_mode", "authority_domain", "component", "contract", "boundary") {
		return false
	}
	switch node.Status {
	case "candidate", "machine_adopted", "contested", "rejected", "stale", "superseded", "deprecated", "retired", "historical":
		return false
	}
	switch node.PromotionStatus {
	case "candidate", "machine_adopted", "rejected", "superseded":
		return false
	}
	switch node.ReviewStatus {
	case "review_required", "rejected", "superseded", "not_human_reviewed":
		return false
	}
	switch node.SourceKind {
	case "generated_candidate", "neural_candidate", "neural_prediction":
		return false
	}
	return true
}

func hasAnyClass(n Node, classes ...string) bool {
	for _, class := range classes {
		if hasClass(n, class) {
			return true
		}
	}
	return false
}

func repoHasRegularFile(repoRoot, requestedPath string) bool {
	if strings.TrimSpace(repoRoot) == "" {
		return false
	}
	full := filepath.Join(repoRoot, filepath.FromSlash(requestedPath))
	info, err := os.Stat(full)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func objectIDFromObject(object string, isIRI bool) string {
	object = strings.TrimSpace(object)
	if !isIRI {
		return object
	}
	for _, class := range []string{"source_file", "code_symbol", "symbol", "component", "boundary", "contract", "invariant", "failure_mode", "forbidden_fix", "test", "intent", "decision", "incident", "evidence", "runtime_evidence", "authority_domain", "state_object", "repair_plan"} {
		prefix := classPrefix(class)
		if prefix != "" && strings.HasPrefix(object, prefix) {
			return rdf.DecodeIRIPath(object[len(prefix):])
		}
	}
	if i := strings.LastIndex(object, "/"); i >= 0 {
		return object[i+1:]
	}
	if i := strings.LastIndex(object, "#"); i >= 0 {
		return object[i+1:]
	}
	return object
}

func claimDomainMatches(c architecture.Claim, domain string) bool {
	return c.Scope.Repository == domain || c.Scope.Repo == domain || c.Scope.Domain == domain
}

func sortedClaimMap(m map[string]architecture.Claim) []architecture.Claim {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]architecture.Claim, 0, len(ids))
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}

func sortedNodeMap(m map[string]Node) []Node {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Node, 0, len(ids))
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}

func oneEdgeIDs(n Node) []string {
	var out []string
	out = append(out, n.DependsOn...)
	out = append(out, n.ReadsFrom...)
	out = append(out, n.WritesTo...)
	out = append(out, n.ProtectedByBoundaries...)
	out = append(out, n.ExposesContracts...)
	out = append(out, n.Separates...)
	out = append(out, n.ExposedBy...)
	out = append(out, n.ConsumedBy...)
	out = append(out, n.ConstrainedByInvariants...)
	out = append(out, n.RequiresTests...)
	out = append(out, n.SupportedByEvidence...)
	out = append(out, n.VulnerableTo...)
	return cleanList(out)
}

func findNode(idx GraphIndex, id string) (Node, bool) {
	if iri, ok := idx.NodesByID[id]; ok {
		return idx.Nodes[iri], true
	}
	for _, n := range idx.Nodes {
		if n.ID == id || contains(n.Classes, strings.Split(id, ":")[0]) && strings.HasSuffix(id, ":"+n.ID) {
			return n, true
		}
	}
	return Node{}, false
}

func filterNodes(nodes []Node, class string) []Node {
	var out []Node
	for _, n := range nodes {
		if hasClass(n, class) {
			out = append(out, n)
		}
	}
	return out
}

func crossingPresent(nodes []Node) bool {
	for _, n := range nodes {
		if hasClass(n, "component") && len(n.DependsOn)+len(n.ReadsFrom)+len(n.WritesTo) > 0 {
			return true
		}
	}
	return false
}

func crossWithoutBoundaryOrContract(nodes []Node) bool {
	if !crossingPresent(nodes) {
		return false
	}
	return len(filterNodes(nodes, "boundary")) == 0 && len(filterNodes(nodes, "contract")) == 0
}

func bindingResolved(b architecture.ClaimDocumentBinding) bool {
	return b.RepositoryDomain != "" && b.RevisionStatus == architecture.RevisionResolved && b.Revision != "" && b.GraphDigestStatus == architecture.GraphDigestResolved && b.GraphDigestSHA256 != ""
}

func bindingsEqual(a, b architecture.ClaimDocumentBinding) bool {
	return a.RepositoryDomain == b.RepositoryDomain &&
		a.Revision == b.Revision &&
		a.RevisionStatus == b.RevisionStatus &&
		a.GraphDigestSHA256 == b.GraphDigestSHA256 &&
		a.GraphDigestStatus == b.GraphDigestStatus
}

func observedBinding(ctx Context) architecture.ClaimDocumentBinding {
	b := ctx.Request.Binding
	if ctx.GraphReceipt.Verified {
		b.GraphDigestSHA256 = ctx.GraphReceipt.DigestSHA256
		b.GraphDigestStatus = architecture.GraphDigestResolved
	}
	if ctx.RepositoryStatus == architecture.RevisionResolved {
		b.Revision = ctx.RepositoryRev
		b.RevisionStatus = architecture.RevisionResolved
	}
	return b
}

func claimReportBinding(b plane.ClaimBindingReport) architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain: b.RepositoryDomain, Revision: b.Revision, RevisionStatus: b.RevisionStatus,
		GraphDigestSHA256: b.GraphDigestSHA256, GraphDigestStatus: b.GraphDigestStatus,
	}
}

func questionRelevant(q architecture.OpenQuestion, scope resolvedScope) bool {
	for _, id := range q.BlocksClaims {
		if _, ok := scope.ByClaimID[id]; ok {
			return true
		}
	}
	for _, ref := range q.BlocksNodes {
		_, id, ok := architecture.ParseClassQualifiedReference(ref)
		if ok {
			if _, found := scope.ByNodeID[id]; found {
				return true
			}
		}
	}
	return intersects(q.Scope.Files, scope.Receipt.Files) || intersects(q.Scope.Symbols, scope.Receipt.Symbols) || intersects(q.Scope.Components, scope.Receipt.Components)
}

func questionPriorityBlocks(priority string) bool {
	return priority == architecture.QuestionPriorityCritical || priority == architecture.QuestionPriorityHigh
}

func severityForPriority(priority string) string {
	if oneOf(priority, "critical", "high", "medium", "low") {
		return priority
	}
	return "high"
}

func splitRefs(refs []string) ([]string, []string) {
	var claims, questions []string
	for _, r := range refs {
		if strings.HasPrefix(r, "question.") {
			questions = append(questions, r)
		} else if strings.HasPrefix(r, "claim.") {
			claims = append(claims, r)
		}
	}
	return cleanList(claims), cleanList(questions)
}

func (b *assessmentBuilder) dimensionRequired(dim string) bool {
	if contains(b.policy.RequiredDimensions, dim) {
		return true
	}
	return contains(b.ctx.Request.Scope.AdditionalDimensions, dim)
}

func (b *assessmentBuilder) dimensionApplicable(dim string) bool {
	switch dim {
	case DimensionStructural, DimensionEvidence, DimensionContradiction, DimensionAgent:
		return true
	case DimensionAuthority:
		if oneOf(b.ctx.Request.Scope.AccessMode, AccessWrite, AccessReadWrite) {
			return true
		}
		for _, c := range b.scope.Claims {
			if oneOf(c.Statement.Predicate, "writes", "mutates_state", "has_observed_writer_set", "owns_state", "reads") {
				return true
			}
		}
		return len(filterNodes(b.scope.Nodes, "authority_domain")) > 0
	case DimensionContract:
		return crossingPresent(b.scope.Nodes) || len(filterNodes(b.scope.Nodes, "boundary")) > 0 || len(filterNodes(b.scope.Nodes, "contract")) > 0
	case DimensionBehavioral:
		if b.ctx.Request.Scope.RiskClass != RiskLowRisk {
			return true
		}
		for _, c := range b.scope.Claims {
			if oneOf(c.Statement.Predicate, "requires_guard", "transitions_to", "reads", "writes", "mutates_state", "controls_lifecycle", "requires_test") {
				return true
			}
		}
		return false
	case DimensionDirection:
		if b.ctx.Request.Scope.RiskClass != RiskLowRisk {
			return true
		}
		return b.ctx.Request.Scope.DirectionRequirement != DirectionNotApplicable || b.hasDirectionalSignal()
	default:
		return false
	}
}

func (b *assessmentBuilder) conditionAllowed(dim string, q architecture.OpenQuestion) bool {
	return b.policy.ConditionalAllowed &&
		contains(b.policy.ConditionalDimensions, dim) &&
		!oneOf(dim, DimensionAuthority, DimensionEvidence, DimensionContradiction) &&
		!questionPriorityBlocks(q.Priority)
}

func (b *assessmentBuilder) blockerIDsFor(dim string) []string {
	var ids []string
	for _, bl := range b.blockers {
		if bl.Dimension == dim {
			ids = append(ids, bl.ID)
		}
	}
	return cleanList(ids)
}

func (b *assessmentBuilder) conditionIDsFor(dim string) []string {
	var ids []string
	for _, c := range b.conditions {
		if c.Dimension == dim {
			ids = append(ids, c.ID)
		}
	}
	return cleanList(ids)
}

func (b *assessmentBuilder) hasUncertifiable(dim string) bool {
	return b.uncertifiable[dim]
}

func (b *assessmentBuilder) nodeExists(id, class string) bool {
	if n, ok := findNode(b.ctx.Graph, id); ok {
		return class == "" || hasClass(n, class)
	}
	return false
}

func (b *assessmentBuilder) hasCurrentTestOrEvidence() bool {
	for _, n := range b.scope.Nodes {
		if hasClass(n, "test") || hasClass(n, "evidence") || hasClass(n, "runtime_evidence") {
			return true
		}
	}
	if b.ctx.Evidence != nil {
		for _, ev := range b.ctx.Evidence.Evidence {
			if ev.Status == maintenance.EvidenceStatusPass && ev.Freshness == maintenance.EvidenceFreshnessCurrent {
				return true
			}
		}
	}
	return false
}

func (b *assessmentBuilder) hasAnyRequiredTest() bool {
	for _, n := range b.scope.Nodes {
		if len(n.RequiresTests) > 0 || hasClass(n, "test") {
			return true
		}
	}
	return false
}

func (b *assessmentBuilder) hasPlane(want string) bool {
	for _, c := range b.scope.Claims {
		if c.ArchitecturalPlane == want && c.EpistemicStatus == architecture.StatusSupported {
			if a, ok := b.planeByClaim[c.ID]; !ok || a.PlaneState == plane.StateJustified {
				return true
			}
		}
	}
	return false
}

func (b *assessmentBuilder) hasDirectionalSignal() bool {
	for _, c := range b.scope.Claims {
		if oneOf(c.ArchitecturalPlane, architecture.PlaneIntended, architecture.PlaneHistorical, architecture.PlaneDesired) {
			return true
		}
	}
	return hasNodePlane(b.scope.Nodes, architecture.PlaneIntended) || hasNodePlane(b.scope.Nodes, architecture.PlaneHistorical) || hasNodePlane(b.scope.Nodes, architecture.PlaneDesired)
}

func hasNodePlane(nodes []Node, plane string) bool {
	for _, n := range nodes {
		if n.ArchitecturalPlane == plane {
			return true
		}
	}
	return false
}

func claimIDs(claims []architecture.Claim) []string {
	var ids []string
	for _, c := range claims {
		ids = append(ids, c.ID)
	}
	return cleanList(ids)
}

func claimReceipts(claims []architecture.Claim, planes map[string]plane.ClaimAssessment) []ClaimReceipt {
	out := make([]ClaimReceipt, 0, len(claims))
	for _, c := range claims {
		r := ClaimReceipt{ID: c.ID, PropositionKey: plane.PropositionKey(c), ArchitecturalPlane: c.ArchitecturalPlane, EpistemicStatus: c.EpistemicStatus}
		if a, ok := planes[c.ID]; ok {
			r.PlaneState = a.PlaneState
		}
		out = append(out, r)
	}
	return out
}

func nodeReceipts(nodes []Node) []NodeReceipt {
	out := make([]NodeReceipt, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, NodeReceipt{ID: n.ID, IRI: n.IRI, Classes: cleanList(n.Classes)})
	}
	return out
}

func (b *assessmentBuilder) limitations() []architecture.Limitation {
	var out []architecture.Limitation
	if b.ctx.Claims.Limitations != nil {
		out = append(out, b.ctx.Claims.Limitations...)
	}
	if b.ctx.Maintenance != nil {
		out = append(out, b.ctx.Maintenance.Limitations...)
	}
	if b.ctx.Plane != nil {
		out = append(out, b.ctx.Plane.Limitations...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Source+out[i].Scope+out[i].Reason < out[j].Source+out[j].Scope+out[j].Reason
	})
	return out
}

// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"gopkg.in/yaml.v3"
)

var (
	sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)
	hexLenRE = map[int]*regexp.Regexp{}
)

type AwarenessMutationEnforcementPlan struct {
	SourcePath            string   `json:"source_path" yaml:"source_path"`
	SourceClass           string   `json:"source_class" yaml:"source_class"`
	ImporterID            string   `json:"importer_id" yaml:"importer_id"`
	RequiredPreconditions []string `json:"required_preconditions,omitempty" yaml:"required_preconditions,omitempty"`
	RequiredVerification  []string `json:"required_verification,omitempty" yaml:"required_verification,omitempty"`
	Limitations           []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type AwarenessMutationEnforcementDocument struct {
	SchemaVersion      string                             `json:"schema_version" yaml:"schema_version"`
	PolicyID           string                             `json:"policy_id" yaml:"policy_id"`
	TaskID             string                             `json:"task_id" yaml:"task_id"`
	RepositoryRevision string                             `json:"repository_revision" yaml:"repository_revision"`
	GraphDigestSHA256  string                             `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	Plans              []AwarenessMutationEnforcementPlan `json:"plans" yaml:"plans"`
}

type awarenessMutationEnforcementEnvelope struct {
	AwarenessMutationEnforcement AwarenessMutationEnforcementDocument `json:"awareness_mutation_enforcement" yaml:"awareness_mutation_enforcement"`
}

type authorityBindingStatus string

const (
	authorityApplicable    authorityBindingStatus = "applicable"
	authorityBackground    authorityBindingStatus = "background"
	authorityDominated     authorityBindingStatus = "dominated"
	authorityContradictory authorityBindingStatus = "contradictory"
	authorityUnmapped      authorityBindingStatus = "unmapped"
	authorityStale         authorityBindingStatus = "stale"
)

type AuthoritySpecificity int

const (
	AuthorityRepository AuthoritySpecificity = iota
	AuthorityComponent
	AuthorityStateSurface
	AuthoritySymbol
	AuthorityOperation
)

type authorityBinding struct {
	AuthorityNodeID string
	TargetFile      string
	TargetState     string
	RelationPath    []string
	Specificity     AuthoritySpecificity
	Status          authorityBindingStatus
}

type authorityContradiction struct {
	TargetFile       string
	TargetState      string
	AuthorityNodeIDs []string
}

type authorityGap struct {
	TargetFile     string
	TargetState    string
	RelationPath   []string
	SurfaceNodeIDs []string
	Status         authorityBindingStatus
}

type authorityProjection struct {
	Bindings       []authorityBinding
	Background     []authorityBinding
	Dominated      []authorityBinding
	Contradictions []authorityContradiction
	Unmapped       []authorityGap
}

type behavioralBindingStatus string

const (
	behavioralApplicable    behavioralBindingStatus = "applicable"
	behavioralBackground    behavioralBindingStatus = "background"
	behavioralDominated     behavioralBindingStatus = "dominated"
	behavioralContradictory behavioralBindingStatus = "contradictory"
	behavioralUnsupported   behavioralBindingStatus = "unsupported"
)

type behavioralBinding struct {
	ClaimID        string
	OperationKind  string
	TargetFile     string
	TargetSymbol   string
	ComponentID    string
	BehavioralKind string
	RelationPath   []string
	Plane          string
	PlaneState     string
	Status         behavioralBindingStatus
	SourceClaimIDs []string
	SourceNodeIDs  []string
	Explanation    string
}

type behavioralProjection struct {
	Applicable     []behavioralBinding
	Background     []behavioralBinding
	Dominated      []behavioralBinding
	Contradictions []behavioralBinding
}

type failureSurfaceBindingStatus string

const (
	failureSurfaceApplicable failureSurfaceBindingStatus = "applicable"
	failureSurfaceBackground failureSurfaceBindingStatus = "background"
	failureSurfaceIneligible failureSurfaceBindingStatus = "ineligible"
)

type failureSurfaceBinding struct {
	FailureModeID string
	TargetFile    string
	RelationPath  []string
	Status        failureSurfaceBindingStatus
	Explanation   string
}

type failureSurfaceProjection struct {
	Applicable []failureSurfaceBinding
	Background []failureSurfaceBinding
	Ineligible []failureSurfaceBinding
}

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

func firstIntersect(a, b []string) string {
	seen := map[string]bool{}
	for _, x := range cleanList(b) {
		seen[x] = true
	}
	for _, x := range cleanList(a) {
		if seen[x] {
			return x
		}
	}
	return ""
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

func projectApplicableAuthority(req Scope, scope resolvedScope, graph GraphIndex) authorityProjection {
	authNodes := allNodesByClass(graph, "authority_domain")
	if len(authNodes) == 0 && len(req.Files) == 0 {
		return authorityProjection{}
	}

	out := authorityProjection{}
	direct := taskAnchorNodes(req, scope.Nodes)
	fileGroups := map[string][]authorityBinding{}
	stateGroups := map[string][]authorityBinding{}
	stateGaps := map[string]authorityGap{}
	used := map[string]bool{}

	for _, file := range cleanPathList(req.Files) {
		for _, auth := range authNodes {
			if spec, ok := authorityMatchesFile(auth, file); ok {
				fileGroups[file] = append(fileGroups[file], authorityBinding{
					AuthorityNodeID: auth.ID,
					TargetFile:      file,
					RelationPath:    []string{"task_modifies_file", "authority_covers_path"},
					Specificity:     spec,
					Status:          authorityBindingStatusForNode(auth),
				})
			}
		}
	}

	for _, surface := range taskStateSurfaces(req, direct) {
		matched := false
		for _, auth := range authNodes {
			if contains(auth.OwnsStates, surface.TargetState) {
				matched = true
				stateGroups[surface.TargetState] = append(stateGroups[surface.TargetState], authorityBinding{
					AuthorityNodeID: auth.ID,
					TargetFile:      surface.TargetFile,
					TargetState:     surface.TargetState,
					RelationPath:    []string{"task_modifies_file", "surface_reaches_state", "authority_owns_state"},
					Specificity:     AuthorityStateSurface,
					Status:          authorityBindingStatusForNode(auth),
				})
			}
		}
		if !matched {
			stateGaps[surface.TargetState] = authorityGap{
				TargetFile:     surface.TargetFile,
				TargetState:    surface.TargetState,
				RelationPath:   []string{"task_modifies_file", "surface_reaches_state"},
				SurfaceNodeIDs: surface.SurfaceNodeIDs,
				Status:         authorityUnmapped,
			}
		}
	}

	for _, key := range sortedMapKeys(fileGroups) {
		bindings, dominated, contradictions := resolveAuthorityBindings(fileGroups[key], authNodes)
		out.Bindings = append(out.Bindings, bindings...)
		out.Dominated = append(out.Dominated, dominated...)
		out.Contradictions = append(out.Contradictions, contradictions...)
		for _, binding := range bindings {
			used[binding.AuthorityNodeID] = true
		}
		for _, binding := range dominated {
			used[binding.AuthorityNodeID] = true
		}
	}

	for _, key := range sortedMapKeys(stateGroups) {
		bindings, dominated, contradictions := resolveAuthorityBindings(stateGroups[key], authNodes)
		out.Bindings = append(out.Bindings, bindings...)
		out.Dominated = append(out.Dominated, dominated...)
		out.Contradictions = append(out.Contradictions, contradictions...)
		for _, binding := range bindings {
			used[binding.AuthorityNodeID] = true
		}
		for _, binding := range dominated {
			used[binding.AuthorityNodeID] = true
		}
		delete(stateGaps, key)
	}

	for _, key := range sortedMapKeys(stateGaps) {
		out.Unmapped = append(out.Unmapped, stateGaps[key])
	}

	for _, auth := range authNodes {
		if used[auth.ID] {
			continue
		}
		out.Background = append(out.Background, authorityBinding{
			AuthorityNodeID: auth.ID,
			Status:          authorityBackground,
		})
	}

	out.Bindings = normalizeAuthorityBindings(out.Bindings)
	out.Background = normalizeAuthorityBindings(out.Background)
	out.Dominated = normalizeAuthorityBindings(out.Dominated)
	out.Contradictions = normalizeAuthorityContradictions(out.Contradictions)
	out.Unmapped = normalizeAuthorityGaps(out.Unmapped)
	return out
}

type taskStateSurface struct {
	TargetFile     string
	TargetState    string
	SurfaceNodeIDs []string
}

func taskAnchorNodes(req Scope, nodes []Node) []Node {
	var out []Node
	for _, node := range nodes {
		if taskNodeTargetsScope(req, node) {
			out = append(out, node)
		}
	}
	return sortedNodeSlice(out)
}

func taskNodeTargetsScope(req Scope, node Node) bool {
	if node.SourcePath != "" && contains(req.Files, node.SourcePath) {
		return true
	}
	if intersects(node.AuthoredIn, req.Files) || intersects(node.AnchoredIn, req.Symbols) {
		return true
	}
	if hasClass(node, "component") && contains(req.Components, node.ID) {
		return true
	}
	for _, file := range req.Files {
		for _, prefix := range node.CoversPath {
			if authorityPathCoversFile(prefix, file) {
				return true
			}
		}
	}
	return false
}

func taskStateSurfaces(req Scope, nodes []Node) []taskStateSurface {
	byState := map[string]taskStateSurface{}
	for _, node := range nodes {
		files := taskFilesForNode(req, node)
		if len(files) == 0 {
			files = []string{""}
		}
		stateIDs := cleanList(append(append([]string{}, node.WritesTo...), append(node.ReadsFrom, node.OwnsStates...)...))
		if hasClass(node, "state_object") {
			stateIDs = cleanList(append(stateIDs, node.ID))
		}
		for _, stateID := range stateIDs {
			if strings.TrimSpace(stateID) == "" {
				continue
			}
			for _, file := range files {
				key := stateID + "\x00" + file
				cur := byState[key]
				cur.TargetFile = file
				cur.TargetState = stateID
				cur.SurfaceNodeIDs = append(cur.SurfaceNodeIDs, node.ID)
				byState[key] = cur
			}
		}
	}
	keys := sortedMapKeys(byState)
	out := make([]taskStateSurface, 0, len(keys))
	for _, key := range keys {
		item := byState[key]
		item.SurfaceNodeIDs = cleanList(item.SurfaceNodeIDs)
		out = append(out, item)
	}
	return out
}

func taskFilesForNode(req Scope, node Node) []string {
	var out []string
	if node.SourcePath != "" && contains(req.Files, node.SourcePath) {
		out = append(out, node.SourcePath)
	}
	for _, path := range node.AuthoredIn {
		if contains(req.Files, path) {
			out = append(out, path)
		}
	}
	for _, file := range req.Files {
		for _, prefix := range node.CoversPath {
			if authorityPathCoversFile(prefix, file) {
				out = append(out, file)
			}
		}
	}
	return cleanPathList(out)
}

func authorityMatchesFile(node Node, file string) (AuthoritySpecificity, bool) {
	if !hasClass(node, "authority_domain") {
		return AuthorityRepository, false
	}
	best := -1
	for _, prefix := range node.CoversPath {
		if !authorityPathCoversFile(prefix, file) {
			continue
		}
		norm := normalizePath(prefix)
		if norm == file {
			return AuthorityOperation, true
		}
		if l := len(norm); l > best {
			best = l
		}
	}
	if best >= 0 {
		return AuthorityRepository, true
	}
	return AuthorityRepository, false
}

func authorityPathCoversFile(prefix, file string) bool {
	prefix = normalizePath(prefix)
	file = normalizePath(file)
	if prefix == "" || file == "" {
		return false
	}
	if file == prefix {
		return true
	}
	return strings.HasPrefix(file, strings.TrimSuffix(prefix, "/")+"/")
}

func authorityBindingStatusForNode(node Node) authorityBindingStatus {
	switch node.Status {
	case "stale", "superseded", "historical", "deprecated", "retired":
		return authorityStale
	default:
		return authorityApplicable
	}
}

func resolveAuthorityBindings(in []authorityBinding, nodes []Node) ([]authorityBinding, []authorityBinding, []authorityContradiction) {
	if len(in) == 0 {
		return nil, nil, nil
	}
	index := map[string]Node{}
	for _, node := range nodes {
		index[node.ID] = node
	}
	sort.SliceStable(in, func(i, j int) bool {
		return authorityBindingLess(in[i], in[j], index)
	})
	best := in[0]
	bindings := []authorityBinding{best}
	var dominated []authorityBinding
	var contradictions []authorityContradiction
	for _, item := range in[1:] {
		if comparableAuthorityBinding(best, item) && authorityFactsEqual(index[best.AuthorityNodeID], index[item.AuthorityNodeID]) {
			dominated = append(dominated, markAuthorityBindingStatus(item, authorityDominated))
			continue
		}
		if comparableAuthorityBinding(best, item) {
			contradictions = append(contradictions, authorityContradiction{
				TargetFile:       best.TargetFile,
				TargetState:      best.TargetState,
				AuthorityNodeIDs: cleanList([]string{best.AuthorityNodeID, item.AuthorityNodeID}),
			})
			continue
		}
		dominated = append(dominated, markAuthorityBindingStatus(item, authorityDominated))
	}
	return normalizeAuthorityBindings(bindings), normalizeAuthorityBindings(dominated), normalizeAuthorityContradictions(contradictions)
}

func authorityBindingLess(a, b authorityBinding, nodes map[string]Node) bool {
	// A stale precise record must not dominate a fresh broader record.
	if authorityFreshnessRank(a, nodes) != authorityFreshnessRank(b, nodes) {
		return authorityFreshnessRank(a, nodes) > authorityFreshnessRank(b, nodes)
	}
	if a.Specificity != b.Specificity {
		return a.Specificity > b.Specificity
	}
	return a.AuthorityNodeID < b.AuthorityNodeID
}

func authorityFreshnessRank(binding authorityBinding, nodes map[string]Node) int {
	if node, ok := nodes[binding.AuthorityNodeID]; ok && authorityBindingStatusForNode(node) == authorityApplicable {
		return 1
	}
	return 0
}

func comparableAuthorityBinding(a, b authorityBinding) bool {
	return a.TargetFile == b.TargetFile &&
		a.TargetState == b.TargetState &&
		a.Specificity == b.Specificity &&
		a.Status == b.Status
}

func authorityFactsEqual(a, b Node) bool {
	return strings.Join(a.OwnerServices, "\x00") == strings.Join(b.OwnerServices, "\x00") &&
		strings.Join(a.MayWrite, "\x00") == strings.Join(b.MayWrite, "\x00") &&
		strings.Join(a.MayRead, "\x00") == strings.Join(b.MayRead, "\x00") &&
		strings.Join(a.MustMutateVia, "\x00") == strings.Join(b.MustMutateVia, "\x00") &&
		strings.Join(a.MustReadVia, "\x00") == strings.Join(b.MustReadVia, "\x00") &&
		strings.Join(a.ObservesVia, "\x00") == strings.Join(b.ObservesVia, "\x00") &&
		strings.Join(a.TruthLayers, "\x00") == strings.Join(b.TruthLayers, "\x00")
}

func markAuthorityBindingStatus(binding authorityBinding, status authorityBindingStatus) authorityBinding {
	binding.Status = status
	return binding
}

func normalizeAuthorityBindings(in []authorityBinding) []authorityBinding {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]authorityBinding{}
	var keys []string
	for _, item := range in {
		key := strings.Join([]string{
			item.AuthorityNodeID,
			item.TargetFile,
			item.TargetState,
			strconv.Itoa(int(item.Specificity)),
			string(item.Status),
		}, "\x00")
		cur := seen[key]
		cur.AuthorityNodeID = item.AuthorityNodeID
		cur.TargetFile = normalizePath(item.TargetFile)
		cur.TargetState = strings.TrimSpace(item.TargetState)
		cur.RelationPath = append(cur.RelationPath, item.RelationPath...)
		cur.Specificity = item.Specificity
		cur.Status = item.Status
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
		seen[key] = cur
	}
	sort.Strings(keys)
	out := make([]authorityBinding, 0, len(keys))
	for _, key := range keys {
		item := seen[key]
		item.RelationPath = cleanList(item.RelationPath)
		out = append(out, item)
	}
	return out
}

func normalizeAuthorityContradictions(in []authorityContradiction) []authorityContradiction {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]authorityContradiction{}
	var keys []string
	for _, item := range in {
		item.TargetFile = normalizePath(item.TargetFile)
		item.TargetState = strings.TrimSpace(item.TargetState)
		item.AuthorityNodeIDs = cleanList(item.AuthorityNodeIDs)
		key := item.TargetFile + "\x00" + item.TargetState + "\x00" + strings.Join(item.AuthorityNodeIDs, "\x00")
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
			seen[key] = item
		}
	}
	sort.Strings(keys)
	out := make([]authorityContradiction, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func normalizeAuthorityGaps(in []authorityGap) []authorityGap {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]authorityGap{}
	var keys []string
	for _, item := range in {
		item.TargetFile = normalizePath(item.TargetFile)
		item.TargetState = strings.TrimSpace(item.TargetState)
		item.RelationPath = cleanList(item.RelationPath)
		item.SurfaceNodeIDs = cleanList(item.SurfaceNodeIDs)
		key := item.TargetFile + "\x00" + item.TargetState
		cur := seen[key]
		cur.TargetFile = item.TargetFile
		cur.TargetState = item.TargetState
		cur.RelationPath = append(cur.RelationPath, item.RelationPath...)
		cur.SurfaceNodeIDs = append(cur.SurfaceNodeIDs, item.SurfaceNodeIDs...)
		cur.Status = item.Status
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
		seen[key] = cur
	}
	sort.Strings(keys)
	out := make([]authorityGap, 0, len(keys))
	for _, key := range keys {
		item := seen[key]
		item.RelationPath = cleanList(item.RelationPath)
		item.SurfaceNodeIDs = cleanList(item.SurfaceNodeIDs)
		out = append(out, item)
	}
	return out
}

func sortedNodeSlice(in []Node) []Node {
	if len(in) == 0 {
		return nil
	}
	byID := map[string]Node{}
	for _, node := range in {
		byID[node.ID] = node
	}
	return sortedNodeMap(byID)
}

func sortedMapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func factReceiptsByID(doc architecture.ClaimDocument) map[string]architecture.ClaimFactReceipt {
	if len(doc.FactReceipts) == 0 {
		return map[string]architecture.ClaimFactReceipt{}
	}
	out := make(map[string]architecture.ClaimFactReceipt, len(doc.FactReceipts))
	for _, receipt := range doc.FactReceipts {
		if strings.TrimSpace(receipt.Fact.ID) == "" {
			continue
		}
		out[receipt.Fact.ID] = receipt
	}
	return out
}

func projectApplicableBehavioral(req Scope, scope resolvedScope, facts map[string]architecture.ClaimFactReceipt, planes map[string]plane.ClaimAssessment) behavioralProjection {
	out := behavioralProjection{}
	for _, claim := range scope.Claims {
		binding := behavioralBinding{
			ClaimID:        claim.ID,
			OperationKind:  req.AccessMode,
			Plane:          claim.ArchitecturalPlane,
			Status:         behavioralBackground,
			SourceClaimIDs: []string{claim.ID},
		}
		if assessment, ok := planes[claim.ID]; ok {
			binding.PlaneState = assessment.PlaneState
		}
		kind, ok := behavioralClaimKind(claim)
		if !ok {
			binding.Status = behavioralUnsupported
			binding.Explanation = "claim predicate does not denote current behavioral proof"
			out.Background = append(out.Background, binding)
			continue
		}
		binding.BehavioralKind = kind
		if !oneOf(claim.ArchitecturalPlane, architecture.PlaneObserved, architecture.PlaneEnforced) {
			binding.Explanation = "directional or historical claim does not satisfy current behavioral closure"
			out.Background = append(out.Background, binding)
			continue
		}
		if file := firstIntersect(cleanPathList(claim.Scope.Files), req.Files); file != "" {
			binding.TargetFile = file
			binding.RelationPath = []string{"task_modifies_file", "claim_scoped_to_file", "claim_asserts_behavioral_predicate"}
			binding.Status = behavioralApplicable
			binding.Explanation = "behavioral predicate is scoped directly to an exact task file"
			out.Applicable = append(out.Applicable, binding)
			continue
		}
		if target := firstIntersect(claimPremiseTargetFiles(claim, facts), req.Files); target != "" {
			binding.TargetFile = target
			binding.RelationPath = []string{"task_modifies_file", "premise_targets_file", "claim_asserts_behavioral_predicate"}
			binding.Status = behavioralApplicable
			binding.Explanation = "behavioral premise targets an exact task file"
			out.Applicable = append(out.Applicable, binding)
			continue
		}
		if symbol := firstIntersect(cleanList(claim.Scope.Symbols), req.Symbols); symbol != "" {
			binding.TargetSymbol = symbol
			binding.RelationPath = []string{"task_targets_symbol", "claim_scoped_to_symbol", "claim_asserts_behavioral_predicate"}
			binding.Status = behavioralApplicable
			binding.Explanation = "behavioral predicate is scoped directly to an exact task symbol"
			out.Applicable = append(out.Applicable, binding)
			continue
		}
		if component := firstIntersect(cleanList(claim.Scope.Components), req.Components); component != "" {
			binding.ComponentID = component
			binding.RelationPath = []string{"task_targets_component", "claim_scoped_to_component", "claim_asserts_behavioral_predicate"}
			binding.Status = behavioralApplicable
			binding.Explanation = "behavioral predicate is scoped directly to an exact task component"
			out.Applicable = append(out.Applicable, binding)
			continue
		}
		binding.Explanation = "claim lacks an explainable task-to-behavior anchor"
		out.Background = append(out.Background, binding)
	}
	out.Applicable = normalizeBehavioralBindings(out.Applicable)
	out.Background = normalizeBehavioralBindings(out.Background)
	out.Dominated = normalizeBehavioralBindings(out.Dominated)
	out.Contradictions = normalizeBehavioralBindings(out.Contradictions)
	return out
}

func behavioralClaimKind(claim architecture.Claim) (string, bool) {
	switch strings.TrimSpace(claim.Statement.Predicate) {
	case "refuses_when":
		return "guard", true
	case "rejects_transition_when":
		return "transition", true
	case "asserts_rule":
		return "asserted_rule", true
	case "has_tested_failure_boundary":
		return "failure_boundary", true
	case "has_tested_monotonic_transition":
		return "monotonic_transition", true
	case "controls_lifecycle":
		return "lifecycle_control", true
	default:
		return "", false
	}
}

func claimPremiseTargetFiles(claim architecture.Claim, facts map[string]architecture.ClaimFactReceipt) []string {
	var out []string
	for _, id := range claim.PremiseFacts {
		receipt, ok := facts[id]
		if !ok {
			continue
		}
		if target := normalizeSinglePath(receipt.Fact.Meta["target_file"]); target != "" {
			out = append(out, target)
		}
	}
	return cleanPathList(out)
}

func normalizeBehavioralBindings(in []behavioralBinding) []behavioralBinding {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]behavioralBinding{}
	var keys []string
	for _, item := range in {
		key := strings.Join([]string{
			item.ClaimID,
			item.OperationKind,
			item.TargetFile,
			item.TargetSymbol,
			item.ComponentID,
			item.BehavioralKind,
			item.Plane,
			item.PlaneState,
			string(item.Status),
		}, "\x00")
		cur := seen[key]
		cur.ClaimID = item.ClaimID
		cur.OperationKind = item.OperationKind
		cur.TargetFile = normalizePath(item.TargetFile)
		cur.TargetSymbol = strings.TrimSpace(item.TargetSymbol)
		cur.ComponentID = strings.TrimSpace(item.ComponentID)
		cur.BehavioralKind = strings.TrimSpace(item.BehavioralKind)
		cur.RelationPath = append(cur.RelationPath, item.RelationPath...)
		cur.Plane = item.Plane
		cur.PlaneState = item.PlaneState
		cur.Status = item.Status
		cur.SourceClaimIDs = append(cur.SourceClaimIDs, item.SourceClaimIDs...)
		cur.SourceNodeIDs = append(cur.SourceNodeIDs, item.SourceNodeIDs...)
		if cur.Explanation == "" {
			cur.Explanation = item.Explanation
		}
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
		seen[key] = cur
	}
	sort.Strings(keys)
	out := make([]behavioralBinding, 0, len(keys))
	for _, key := range keys {
		item := seen[key]
		item.RelationPath = cleanList(item.RelationPath)
		item.SourceClaimIDs = cleanList(item.SourceClaimIDs)
		item.SourceNodeIDs = cleanList(item.SourceNodeIDs)
		out = append(out, item)
	}
	return out
}

func projectApplicableFailureModes(req Scope, scope resolvedScope, graph GraphIndex) failureSurfaceProjection {
	out := failureSurfaceProjection{}
	used := map[string]bool{}
	for _, file := range cleanPathList(req.Files) {
		iri, ok := graph.FilesByPath[file]
		if !ok {
			continue
		}
		sourceNode := graph.Nodes[iri]
		for _, failureID := range sourceNode.VulnerableTo {
			node, ok := findNode(graph, failureID)
			if !ok || !hasClass(node, "failure_mode") {
				continue
			}
			binding := failureSurfaceBinding{
				FailureModeID: node.ID,
				TargetFile:    file,
				RelationPath:  []string{"task_modifies_file", "source_file_vulnerable_to_failure_mode"},
			}
			if eligibleFailureSurfaceNode(node) {
				binding.Status = failureSurfaceApplicable
				binding.Explanation = "current governed failure mode binds to the exact task file"
				out.Applicable = append(out.Applicable, binding)
			} else {
				binding.Status = failureSurfaceIneligible
				binding.Explanation = "failure mode is not current governed failure-surface authority for this task"
				out.Ineligible = append(out.Ineligible, binding)
			}
			used[node.ID] = true
		}
	}
	for _, node := range scope.Nodes {
		if !hasClass(node, "failure_mode") || used[node.ID] {
			continue
		}
		out.Background = append(out.Background, failureSurfaceBinding{
			FailureModeID: node.ID,
			Status:        failureSurfaceBackground,
			Explanation:   "failure mode is relevant context but does not bind to an exact task file through vulnerableTo",
		})
	}
	out.Applicable = normalizeFailureSurfaceBindings(out.Applicable)
	out.Background = normalizeFailureSurfaceBindings(out.Background)
	out.Ineligible = normalizeFailureSurfaceBindings(out.Ineligible)
	return out
}

func eligibleFailureSurfaceNode(node Node) bool {
	if !hasClass(node, "failure_mode") {
		return false
	}
	switch node.Status {
	case "candidate", "contested", "rejected", "stale", "superseded", "deprecated", "retired", "historical":
		return false
	}
	switch node.PromotionStatus {
	case "candidate", "rejected", "superseded":
		return false
	}
	switch node.ReviewStatus {
	case "review_required", "rejected", "superseded":
		return false
	}
	switch node.SourceKind {
	case "generated_candidate", "neural_candidate", "neural_prediction":
		return false
	}
	return true
}

func normalizeFailureSurfaceBindings(in []failureSurfaceBinding) []failureSurfaceBinding {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]failureSurfaceBinding{}
	var keys []string
	for _, item := range in {
		key := strings.Join([]string{item.FailureModeID, item.TargetFile, string(item.Status)}, "\x00")
		cur := seen[key]
		cur.FailureModeID = item.FailureModeID
		cur.TargetFile = normalizePath(item.TargetFile)
		cur.RelationPath = append(cur.RelationPath, item.RelationPath...)
		cur.Status = item.Status
		if cur.Explanation == "" {
			cur.Explanation = item.Explanation
		}
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
		seen[key] = cur
	}
	sort.Strings(keys)
	out := make([]failureSurfaceBinding, 0, len(keys))
	for _, key := range keys {
		item := seen[key]
		item.RelationPath = cleanList(item.RelationPath)
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

func allNodesByClass(graph GraphIndex, class string) []Node {
	var out []Node
	for _, node := range graph.Nodes {
		if hasClass(node, class) {
			out = append(out, node)
		}
	}
	return sortedNodeSlice(out)
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
		proj := b.authorityProjection()
		return len(proj.Bindings) > 0 || len(proj.Contradictions) > 0 || len(proj.Unmapped) > 0
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

func awarenessBehavioralPlans(in *AwarenessMutationReceipt) []AwarenessMutationPlanReceipt {
	if in == nil || in.Status != "consumed" {
		return nil
	}
	return append([]AwarenessMutationPlanReceipt{}, in.Plans...)
}

func awarenessPlanPaths(in []AwarenessMutationPlanReceipt) []string {
	var out []string
	for _, plan := range in {
		out = append(out, plan.SourcePath)
	}
	return cleanPathList(out)
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

func normalizeAwarenessMutationReceipt(in *AwarenessMutationReceipt) *AwarenessMutationReceipt {
	if in == nil {
		return nil
	}
	out := *in
	out.Status = strings.TrimSpace(out.Status)
	out.PolicyID = strings.TrimSpace(out.PolicyID)
	out.PlanDigestSHA256 = strings.TrimSpace(out.PlanDigestSHA256)
	out.Limitations = cleanList(out.Limitations)
	out.Plans = append([]AwarenessMutationPlanReceipt{}, out.Plans...)
	for i := range out.Plans {
		out.Plans[i].SourcePath = normalizePath(out.Plans[i].SourcePath)
		out.Plans[i].SourceClass = strings.TrimSpace(out.Plans[i].SourceClass)
		out.Plans[i].ImporterID = strings.TrimSpace(out.Plans[i].ImporterID)
		out.Plans[i].RequiredVerification = cleanList(out.Plans[i].RequiredVerification)
	}
	sort.SliceStable(out.Plans, func(i, j int) bool { return out.Plans[i].SourcePath < out.Plans[j].SourcePath })
	return &out
}

func NormalizeAwarenessMutationReceiptForExternal(in *AwarenessMutationReceipt) *AwarenessMutationReceipt {
	return normalizeAwarenessMutationReceipt(in)
}

func normalizeAwarenessMutationDocument(in AwarenessMutationEnforcementDocument) AwarenessMutationEnforcementDocument {
	out := in
	out.SchemaVersion = strings.TrimSpace(out.SchemaVersion)
	out.PolicyID = strings.TrimSpace(out.PolicyID)
	out.TaskID = strings.TrimSpace(out.TaskID)
	out.RepositoryRevision = strings.TrimSpace(out.RepositoryRevision)
	out.GraphDigestSHA256 = strings.TrimSpace(out.GraphDigestSHA256)
	out.Plans = append([]AwarenessMutationEnforcementPlan{}, out.Plans...)
	for i := range out.Plans {
		out.Plans[i].SourcePath = normalizePath(out.Plans[i].SourcePath)
		out.Plans[i].SourceClass = strings.TrimSpace(out.Plans[i].SourceClass)
		out.Plans[i].ImporterID = strings.TrimSpace(out.Plans[i].ImporterID)
		out.Plans[i].RequiredPreconditions = cleanList(out.Plans[i].RequiredPreconditions)
		out.Plans[i].RequiredVerification = cleanList(out.Plans[i].RequiredVerification)
		out.Plans[i].Limitations = cleanList(out.Plans[i].Limitations)
	}
	sort.SliceStable(out.Plans, func(i, j int) bool { return out.Plans[i].SourcePath < out.Plans[j].SourcePath })
	return out
}

func MarshalCanonicalAwarenessMutationEnforcementYAML(in AwarenessMutationEnforcementDocument) ([]byte, error) {
	doc := normalizeAwarenessMutationDocument(in)
	return yaml.Marshal(awarenessMutationEnforcementEnvelope{AwarenessMutationEnforcement: doc})
}

func LoadAwarenessMutationEnforcement(path string) (AwarenessMutationEnforcementDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AwarenessMutationEnforcementDocument{}, err
	}
	var env awarenessMutationEnforcementEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return AwarenessMutationEnforcementDocument{}, err
	}
	if env.AwarenessMutationEnforcement.SchemaVersion == "" {
		return AwarenessMutationEnforcementDocument{}, errors.New("missing awareness_mutation_enforcement document")
	}
	return normalizeAwarenessMutationDocument(env.AwarenessMutationEnforcement), nil
}

func AwarenessMutationEnforcementDigest(doc AwarenessMutationEnforcementDocument) (string, error) {
	data, err := MarshalCanonicalAwarenessMutationEnforcementYAML(doc)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

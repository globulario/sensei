// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	MaxBriefingRootBlockers = 3
	MaxBriefingQuestions    = 3
	MaxBriefingProbes       = 3
	MaxBriefingClaims       = 12
	MaxBriefingFailures     = 5
	MaxBriefingConstraints  = 8
)

type BriefingClaim struct {
	ID         string `json:"id" yaml:"id"`
	Statement  string `json:"statement" yaml:"statement"`
	Plane      string `json:"plane" yaml:"plane"`
	Status     string `json:"status" yaml:"status"`
	TestBacked bool   `json:"test_backed" yaml:"test_backed"`
}

type TaskBriefing struct {
	SchemaVersion      string                            `json:"schema_version" yaml:"schema_version"`
	TaskID             string                            `json:"task_id" yaml:"task_id"`
	File               string                            `json:"file" yaml:"file"`
	Component          string                            `json:"component,omitempty" yaml:"component,omitempty"`
	Binding            architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Inspect            string                            `json:"inspect" yaml:"inspect"`
	Modify             string                            `json:"modify" yaml:"modify"`
	RelevantClaims     []BriefingClaim                   `json:"relevant_claims,omitempty" yaml:"relevant_claims,omitempty"`
	RelevantClaimCount int                               `json:"relevant_claim_count" yaml:"relevant_claim_count"`
	TestBackedCount    int                               `json:"test_backed_count" yaml:"test_backed_count"`
	FailureModes       []string                          `json:"failure_modes,omitempty" yaml:"failure_modes,omitempty"`
	Constraints        []string                          `json:"constraints,omitempty" yaml:"constraints,omitempty"`
	// PromotedGovernedKnowledge is committed governed truth promoted from architect
	// answers (Phase 8.1b), independently re-proven and scope-relevant to this task.
	// It is categorically distinct from RelevantClaims (task-local) and questions
	// (unresolved dialogue), and implies no certification or completion.
	PromotedGovernedKnowledge []PromotedGovernedRecord `json:"promoted_governed_knowledge,omitempty" yaml:"promoted_governed_knowledge,omitempty"`
	// FeedbackProjection is the exact canonical briefing.feedback_projection/v1 returned by the
	// briefingfeedback owner. PromotedGovernedKnowledge and the promotion limitations are
	// mechanically derived from it, so new consumers can distinguish available, empty, degraded,
	// unavailable, and invalid (and read typed findings) without parsing prose, while old
	// consumers keep the two legacy surfaces.
	FeedbackProjection  *briefingfeedback.Projection    `json:"feedback_projection,omitempty" yaml:"feedback_projection,omitempty"`
	PrimaryBlocker      *taskcontrol.ClassifiedBlocker  `json:"primary_blocker,omitempty" yaml:"primary_blocker,omitempty"`
	PrimaryQuestion     *taskcontrol.ClassifiedQuestion `json:"primary_question,omitempty" yaml:"primary_question,omitempty"`
	PrimaryNextAction   taskcontrol.NextAction          `json:"primary_next_action" yaml:"primary_next_action"`
	AdditionalBlockers  int                             `json:"additional_blockers" yaml:"additional_blockers"`
	AdditionalQuestions int                             `json:"additional_questions" yaml:"additional_questions"`
	AdditionalProbes    int                             `json:"additional_probes" yaml:"additional_probes"`
	Limitations         []string                        `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type briefingClaimIndex struct {
	Binding architecture.ClaimDocumentBinding
	ByFile  map[string][]architecture.Claim
	Facts   map[string]architecture.ClaimFactReceipt
}

var briefingClaimsCache sync.Map
var briefingComponentsCache sync.Map

func BuildTaskBriefing(repoRoot, taskDir, file string, active bool) (TaskBriefing, error) {
	file = filepath.ToSlash(strings.TrimSpace(file))
	if file == "" || filepath.IsAbs(file) || file == ".." || strings.HasPrefix(file, "../") || strings.Contains(file, "/../") {
		return TaskBriefing{}, errors.New("task briefing file must be repository-relative")
	}
	state, resolvedTaskDir, err := ControlStatus(repoRoot, taskDir, active)
	if err != nil {
		return TaskBriefing{}, err
	}
	if state.BindingHealth != "current" {
		return TaskBriefing{}, errors.New("task briefing refused: repair task binding before using architectural context")
	}
	paths, _, err := currentControlPaths(resolvedTaskDir)
	if err != nil {
		return TaskBriefing{}, err
	}
	claimsDigest, err := digestFile(paths.Claims)
	if err != nil {
		return TaskBriefing{}, err
	}
	index, err := loadBriefingClaimIndex(state.TaskID+":"+claimsDigest, paths.Claims)
	if err != nil {
		return TaskBriefing{}, err
	}
	if index.Binding != state.Binding {
		return TaskBriefing{}, errors.New("task briefing refused: claim binding does not match task binding")
	}
	decisionPath := filepath.Join(resolvedTaskDir, "admission", "decision.yaml")
	if paths.Results != "" {
		decisionPath = filepath.Join(filepath.Dir(paths.Results), "admission-decision.yaml")
	}
	decision, err := admission.LoadDecision(decisionPath)
	if err != nil {
		return TaskBriefing{}, err
	}
	brief := TaskBriefing{
		SchemaVersion: SchemaVersion, TaskID: state.TaskID, File: file, Binding: state.Binding,
		Inspect: state.Permission.Inspect, Modify: state.Permission.Modify,
		PrimaryBlocker: state.PrimaryBlocker, PrimaryQuestion: state.PrimaryQuestion,
		PrimaryNextAction: state.NextAction, Limitations: append([]string{}, state.Limitations...),
	}
	relevant := append([]architecture.Claim{}, index.ByFile[file]...)
	sort.SliceStable(relevant, func(i, j int) bool {
		if relevant[i].EpistemicStatus != relevant[j].EpistemicStatus {
			return relevant[i].EpistemicStatus == architecture.StatusSupported
		}
		return relevant[i].ID < relevant[j].ID
	})
	brief.RelevantClaimCount = len(relevant)
	for _, claim := range relevant {
		testBacked := false
		for _, factID := range claim.PremiseFacts {
			receipt := index.Facts[factID]
			if strings.HasPrefix(receipt.Fact.Kind, "test_") || receipt.Fact.Evidence.TestName != "" {
				testBacked = true
				break
			}
		}
		if testBacked {
			brief.TestBackedCount++
		}
		if brief.Component == "" && len(claim.Scope.Components) > 0 {
			brief.Component = claim.Scope.Components[0]
		}
		if len(brief.RelevantClaims) < MaxBriefingClaims {
			brief.RelevantClaims = append(brief.RelevantClaims, BriefingClaim{ID: claim.ID, Statement: claim.Statement.Subject + " " + claim.Statement.Predicate + " " + claim.Statement.Object, Plane: claim.ArchitecturalPlane, Status: claim.EpistemicStatus, TestBacked: testBacked})
		}
	}
	if brief.Component == "" {
		brief.Component = componentForFile(filepath.Join(resolvedTaskDir, "source", "graph.nt"), state.Binding.GraphDigestSHA256, file)
	}
	for _, item := range append(append([]admission.GuidanceItem{}, decision.MustPreserve...), decision.Authority...) {
		if len(item.Paths) > 0 && !containsString(item.Paths, file) {
			continue
		}
		if (item.Class == "invariant" || item.Class == "contract" || item.Class == "boundary") && len(brief.Constraints) < MaxBriefingConstraints {
			brief.Constraints = append(brief.Constraints, item.ID)
		}
	}
	for _, item := range decision.ForbiddenMoves {
		if len(item.Paths) > 0 && !containsString(item.Paths, file) {
			continue
		}
		if (item.Class == "failure_mode" || item.Class == "forbidden_fix") && len(brief.FailureModes) < MaxBriefingFailures {
			brief.FailureModes = append(brief.FailureModes, item.ID)
		}
	}
	brief.Constraints = sortedUnique(brief.Constraints)
	brief.FailureModes = sortedUnique(brief.FailureModes)

	// Consume committed governed promotions relevant to the task scope. Discovery
	// is non-authoritative; each is independently re-proven and scope-filtered.
	taskFiles := map[string]bool{file: true}
	for f := range index.ByFile {
		taskFiles[f] = true
	}
	// Bind the EXACT canonical task identity established by task-session control — never
	// inferred from the task directory, active-task proximity, the requested file, or cwd.
	promoted := collectPromotedKnowledge(repoRoot, file, taskFiles, state.Binding.RepositoryDomain, state.TaskID, stableTaskSessionID(state.TaskID))
	feedback := promoted.Projection
	brief.FeedbackProjection = &feedback
	brief.PromotedGovernedKnowledge = promoted.Records
	brief.Limitations = append(brief.Limitations, promoted.Limitations...)

	brief.AdditionalBlockers = maxInt(0, state.Summary.ActiveRootBlockers-1)
	architectQuestions := 0
	for _, question := range state.Questions {
		if question.ResolutionClass == taskcontrol.ClassArchitectJudgementRequired {
			architectQuestions++
		}
	}
	brief.AdditionalQuestions = maxInt(0, architectQuestions-1)
	brief.AdditionalProbes = maxInt(0, state.Evidence.Eligible-MaxBriefingProbes)
	return brief, nil
}

func loadBriefingClaimIndex(key, path string) (briefingClaimIndex, error) {
	if cached, ok := briefingClaimsCache.Load(key); ok {
		return cached.(briefingClaimIndex), nil
	}
	doc, err := architecture.LoadClaimDocument(path)
	if err != nil {
		return briefingClaimIndex{}, err
	}
	index := briefingClaimIndex{Binding: doc.Binding, ByFile: map[string][]architecture.Claim{}, Facts: map[string]architecture.ClaimFactReceipt{}}
	for _, receipt := range doc.FactReceipts {
		index.Facts[receipt.Fact.ID] = receipt
	}
	for _, claim := range doc.Claims {
		for _, scopedFile := range claim.Scope.Files {
			scopedFile = filepath.ToSlash(scopedFile)
			index.ByFile[scopedFile] = append(index.ByFile[scopedFile], claim)
		}
	}
	briefingClaimsCache.Store(key, index)
	return index, nil
}

func componentForFile(graphPath, graphDigest, file string) string {
	key := graphDigest + ":" + file
	if cached, ok := briefingComponentsCache.Load(key); ok {
		return cached.(string)
	}
	triples, err := graphsnapshot.Load(graphPath)
	if err != nil {
		return ""
	}
	subjectMarker := "#codeSymbol/" + filepath.ToSlash(file) + ":"
	for _, triple := range triples {
		if triple.Predicate != rdf.PropComment || !strings.Contains(triple.Subject, subjectMarker) || !strings.HasPrefix(triple.Object, "component: ") {
			continue
		}
		component := strings.TrimSpace(strings.TrimPrefix(triple.Object, "component: "))
		briefingComponentsCache.Store(key, component)
		return component
	}
	briefingComponentsCache.Store(key, "")
	return ""
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if filepath.ToSlash(item) == target {
			return true
		}
	}
	return false
}

func sortedUnique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

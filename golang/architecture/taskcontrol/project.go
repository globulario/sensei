// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/probe"
	"gopkg.in/yaml.v3"
)

type Inputs struct {
	TaskID         string
	Iteration      int
	Binding        architecture.ClaimDocumentBinding
	Permission     PermissionSummary
	Closure        closure.Report
	Dialogue       architecture.DialogueDocument
	Claims         architecture.ClaimDocument
	Probes         probe.ProbeDocument
	Results        *probe.ResultDocument
	BindingHealthy bool
	BindingErrors  []string
	GeneratedAt    string
	Receipts       []string
	DominanceEdges []DominanceEdge
}

func Project(in Inputs) (TaskControlState, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return TaskControlState{}, errors.New("task ID is required")
	}
	state := TaskControlState{
		SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, Binding: in.Binding,
		TaskID: strings.TrimSpace(in.TaskID), Iteration: in.Iteration,
		GeneratedAt: strings.TrimSpace(in.GeneratedAt), Permission: in.Permission,
		Receipts: clean(in.Receipts),
	}
	if in.BindingHealthy {
		state.BindingHealth = "current"
	} else {
		state.BindingHealth = "stale"
	}
	claimByID := make(map[string]architecture.Claim, len(in.Claims.Claims))
	for _, claim := range in.Claims.Claims {
		claimByID[claim.ID] = claim
	}
	probeByQuestion := map[string][]probe.EvidenceProbe{}
	resultByProbe := map[string]probe.ProbeResult{}
	if in.Results != nil {
		for _, result := range in.Results.Results {
			resultByProbe[result.ProbeID] = result
		}
	}
	for _, p := range in.Probes.Probes {
		probeByQuestion[p.QuestionID] = append(probeByQuestion[p.QuestionID], p)
		state.Probes = append(state.Probes, classifyProbe(p, resultByProbe[p.ID], in.Binding, in.Probes.Binding))
	}
	questionByID := map[string]ClassifiedQuestion{}
	for _, q := range in.Dialogue.OpenQuestions {
		cq := classifyQuestion(q, probeByQuestion[q.ID], resultByProbe)
		questionByID[q.ID] = cq
		state.Questions = append(state.Questions, cq)
	}
	dimensionRequired := map[string]bool{}
	dimensionUncertifiable := map[string]bool{}
	for _, dim := range in.Closure.Dimensions {
		dimensionRequired[dim.Dimension] = dim.Required && dim.Applicable
		dimensionUncertifiable[dim.Dimension] = dim.State == closure.StateUncertifiable
	}
	duplicateOf := duplicateBlockers(in.Closure.Blockers, claimByID)
	dominator, err := dominanceRoots(in.Closure.Blockers, in.DominanceEdges)
	if err != nil {
		state.Limitations = append(state.Limitations, ReasonDominanceCycle)
		in.BindingHealthy = false
	}
	groupMembers := map[string][]string{}
	groupForBlocker := map[string]string{}
	for _, blocker := range in.Closure.Blockers {
		groupID := blockerGroupID(state.TaskID, blocker, claimByID)
		groupForBlocker[blocker.ID] = groupID
		groupMembers[groupID] = append(groupMembers[groupID], blocker.ID)
	}
	probeIDsByBlocker := map[string][]string{}
	for _, p := range in.Probes.Probes {
		for _, blockerID := range p.ClosureBlockerIDs {
			probeIDsByBlocker[blockerID] = append(probeIDsByBlocker[blockerID], p.ID)
		}
	}
	for _, blocker := range in.Closure.Blockers {
		cb := ClassifiedBlocker{
			ID: blocker.ID, Dimension: blocker.Dimension, Severity: blocker.Severity,
			Code: blocker.Code, Statement: blocker.Summary, Consequence: blocker.RequiredNextAction,
			GroupID: groupForBlocker[blocker.ID], QuestionIDs: clean(blocker.QuestionIDs),
			ProbeIDs: clean(probeIDsByBlocker[blocker.ID]), ClaimIDs: clean(blocker.ClaimIDs),
			NodeIDs: clean(blocker.NodeIDs), Files: clean(blocker.Files),
			LoadBearing: dimensionRequired[blocker.Dimension],
		}
		switch {
		case !in.BindingHealthy || dimensionUncertifiable[blocker.Dimension]:
			cb.Disposition = ClassUncertifiable
			cb.ReasonCodes = append(cb.ReasonCodes, in.BindingErrors...)
		case duplicateOf[blocker.ID] != "":
			cb.Disposition = ClassDuplicate
			cb.DuplicateOf = duplicateOf[blocker.ID]
		case dominator[blocker.ID] != "" && dominator[blocker.ID] != blocker.ID:
			cb.Disposition = ClassDominated
			cb.DominatorID = dominator[blocker.ID]
		case !cb.LoadBearing:
			cb.Disposition = ClassNonBlockingUnknown
		default:
			cb.Disposition = blockerClassFromQuestions(cb.QuestionIDs, questionByID)
		}
		state.Blockers = append(state.Blockers, cb)
	}
	state.Questions = classifyDominatedQuestions(state.Questions, state.Blockers)
	for i := range state.Questions {
		var groupIDs []string
		for _, blockerID := range state.Questions[i].BlockerIDs {
			if groupID := groupForBlocker[blockerID]; groupID != "" {
				groupIDs = append(groupIDs, groupID)
			}
		}
		groupIDs = clean(groupIDs)
		if len(groupIDs) > 0 {
			state.Questions[i].GroupID = groupIDs[0]
		}
		questionByID[state.Questions[i].ID] = state.Questions[i]
	}
	state.Groups = buildGroups(state.Blockers, state.Questions, state.Probes, groupMembers, groupForBlocker)
	sortState(&state)
	state.Summary = summarize(state.Blockers, state.Groups)
	state.Evidence = summarizeEvidence(state.Probes)
	state.PrimaryBlocker = primaryBlocker(state.Blockers)
	state.PrimaryQuestion = primaryQuestion(state.Questions)
	state.NextAction = selectNextAction(state, in.BindingHealthy)
	if err := validateAccounting(state); err != nil {
		state.Limitations = append(state.Limitations, err.Error())
		state.NextAction = NextAction{Kind: ActionProvideMissingInput, Summary: "repair task-control accounting before proceeding"}
	}
	state.Limitations = clean(append(state.Limitations, in.BindingErrors...))
	state.ReceiptDigestSHA256 = StateDigest(state)
	return state, nil
}

func classifyDominatedQuestions(questions []ClassifiedQuestion, blockers []ClassifiedBlocker) []ClassifiedQuestion {
	questionByBlocker := map[string][]string{}
	blockerByID := map[string]ClassifiedBlocker{}
	for _, blocker := range blockers {
		blockerByID[blocker.ID] = blocker
		for _, questionID := range blocker.QuestionIDs {
			questionByBlocker[blocker.ID] = append(questionByBlocker[blocker.ID], questionID)
		}
	}
	for i := range questions {
		var targets []string
		allReduced := len(questions[i].BlockerIDs) > 0
		duplicateOnly := true
		for _, blockerID := range questions[i].BlockerIDs {
			blocker := blockerByID[blockerID]
			targetBlocker := blocker.DominatorID
			if targetBlocker == "" {
				targetBlocker = blocker.DuplicateOf
			}
			if targetBlocker == "" {
				allReduced = false
				break
			}
			if blocker.Disposition != ClassDuplicate {
				duplicateOnly = false
			}
			for _, targetQuestion := range questionByBlocker[targetBlocker] {
				if targetQuestion != questions[i].ID {
					targets = append(targets, targetQuestion)
				}
			}
		}
		targets = clean(targets)
		if !allReduced || len(targets) == 0 {
			continue
		}
		questions[i].ResolutionClass = ClassDominated
		if duplicateOnly {
			questions[i].ResolutionClass = ClassDuplicate
		}
		questions[i].BlockingEffect = "reduced"
		questions[i].DominantQuestionID = targets[0]
		questions[i].RequiredActor = "none"
		questions[i].AnswerabilityBasis = clean(append(questions[i].AnswerabilityBasis, "explicit blocker relationship routes this question through "+targets[0]))
	}
	return questions
}

func classifyProbe(p probe.EvidenceProbe, result probe.ProbeResult, taskBinding, probeBinding architecture.ClaimDocumentBinding) ClassifiedProbe {
	cp := ClassifiedProbe{ID: p.ID, QuestionID: p.QuestionID, Kind: p.ProbeKind, SafetyClass: p.SafetyClass}
	for _, step := range p.Steps {
		if step.Target != "" {
			cp.TargetFiles = append(cp.TargetFiles, step.Target)
		}
		if step.Command != "" {
			cp.Disposition = ProbeRejected
			cp.ReasonCodes = append(cp.ReasonCodes, "task.probe.command_forbidden")
		}
	}
	if result.ProbeID != "" {
		cp.Disposition = result.ResultStatus
		return normalizeClassifiedProbe(cp)
	}
	if cp.Disposition == ProbeRejected {
		return normalizeClassifiedProbe(cp)
	}
	switch {
	case !bindingEqual(taskBinding, probeBinding):
		cp.Disposition = ProbeRejected
		cp.ReasonCodes = append(cp.ReasonCodes, "task.probe.input_stale")
	case p.Status == probe.StatusSuperseded:
		cp.Disposition = ProbeSuperseded
	case p.Status == probe.StatusUnavailable:
		cp.Disposition = ProbeUnavailable
	case p.SafetyClass != probe.SafetyStaticRead || p.ApprovalGate != probe.GateNone || !p.AutomaticExecutionAllowed:
		cp.Disposition = ProbeRejected
		cp.ReasonCodes = append(cp.ReasonCodes, "task.probe.safety_not_automatic")
	case p.ProbeKind != probe.KindSourceReceiptVerification:
		cp.Disposition = ProbeRejected
		cp.ReasonCodes = append(cp.ReasonCodes, "task.probe.kind_unsupported")
	default:
		cp.Disposition = ProbeEligible
	}
	return normalizeClassifiedProbe(cp)
}

func classifyQuestion(q architecture.OpenQuestion, probes []probe.EvidenceProbe, results map[string]probe.ProbeResult) ClassifiedQuestion {
	cq := ClassifiedQuestion{
		ID: q.ID, BlockingEffect: "load_bearing", Priority: q.Priority,
		QuestionText: q.QuestionText, BlockerIDs: clean(q.BlocksClosureBlockers),
		ClaimIDs: clean(q.BlocksClaims),
	}
	for _, p := range probes {
		cq.ProbeIDs = append(cq.ProbeIDs, p.ID)
	}
	cq.ProbeIDs = clean(cq.ProbeIDs)
	switch q.Status {
	case architecture.QuestionStatusAnswered, architecture.QuestionStatusResolved, architecture.QuestionStatusSuperseded:
		cq.ResolutionClass, cq.RequiredActor = ClassMechanicallyAnswerable, "system"
		cq.AnswerabilityBasis = []string{"question lifecycle is already resolved"}
		return cq
	}
	for _, p := range probes {
		if result := results[p.ID]; result.ProbeID != "" {
			cq.AnswerabilityBasis = append(cq.AnswerabilityBasis, result.ResultStatus+" static observation "+result.ID+" awaits closure interpretation")
			continue
		}
		if p.Status == probe.StatusProposed && p.SafetyClass == probe.SafetyStaticRead && p.AutomaticExecutionAllowed && p.ApprovalGate == probe.GateNone && p.ProbeKind == probe.KindSourceReceiptVerification {
			cq.ResolutionClass, cq.RequiredActor = ClassStaticProbeAnswerable, "system"
			cq.AnswerabilityBasis = append(cq.AnswerabilityBasis, "eligible static-read probe "+p.ID)
		}
	}
	if cq.ResolutionClass != "" {
		return cq
	}
	if q.ArchitectRequired && hasNormativeAnswerType(q.AcceptedAnswerTypes) {
		cq.ResolutionClass, cq.RequiredActor = ClassArchitectJudgementRequired, "architect"
		cq.AnswerabilityBasis = []string{"repository-local evidence cannot select the required normative direction"}
		return cq
	}
	if len(q.BlocksClosureBlockers) == 0 {
		cq.ResolutionClass, cq.RequiredActor, cq.BlockingEffect = ClassNonBlockingUnknown, "none", "non_blocking"
		cq.AnswerabilityBasis = []string{"question is not linked to a closure blocker"}
		return cq
	}
	cq.ResolutionClass, cq.RequiredActor = ClassActiveUnresolved, "evidence_provider"
	cq.AnswerabilityBasis = clean(append(cq.AnswerabilityBasis, "no completed determining evidence or eligible static probe"))
	return cq
}

func duplicateBlockers(blockers []closure.Blocker, claims map[string]architecture.Claim) map[string]string {
	first := map[string]string{}
	out := map[string]string{}
	for _, blocker := range blockers {
		key := blockerIdentity(blocker, claims)
		if prior := first[key]; prior != "" {
			out[blocker.ID] = prior
		} else {
			first[key] = blocker.ID
		}
	}
	return out
}

func blockerIdentity(b closure.Blocker, claims map[string]architecture.Claim) string {
	return strings.Join([]string{b.Dimension, b.Code, b.RequiredNextAction, blockerSubject(b, claims), strings.Join(clean(b.ClaimIDs), ","), strings.Join(clean(b.NodeIDs), ","), strings.Join(clean(b.Files), ",")}, "\x1f")
}

func blockerGroupID(taskID string, b closure.Blocker, claims map[string]architecture.Claim) string {
	parts := []string{taskID, b.Dimension, b.Code, blockerSubject(b, claims), b.RequiredNextAction}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "blocker-group." + hex.EncodeToString(sum[:])[:16]
}

func blockerSubject(b closure.Blocker, claims map[string]architecture.Claim) string {
	var subjects []string
	for _, id := range b.ClaimIDs {
		if claim, ok := claims[id]; ok {
			subjects = append(subjects, claim.Statement.Subject+"|"+claim.Statement.Predicate+"|"+claim.ArchitecturalPlane)
		}
	}
	if len(subjects) == 0 {
		subjects = append(subjects, b.NodeIDs...)
	}
	if len(subjects) == 0 {
		subjects = append(subjects, b.Files...)
	}
	return strings.Join(clean(subjects), ",")
}

func dominanceRoots(blockers []closure.Blocker, edges []DominanceEdge) (map[string]string, error) {
	known := map[string]bool{}
	for _, blocker := range blockers {
		known[blocker.ID] = true
	}
	parent := map[string]string{}
	for _, edge := range edges {
		if !known[edge.DominatorID] || !known[edge.DominatedID] || edge.DominatorID == edge.DominatedID || strings.TrimSpace(edge.ReasonCode) == "" {
			return nil, errors.New(ReasonDominanceCycle)
		}
		if prior := parent[edge.DominatedID]; prior != "" && prior != edge.DominatorID {
			return nil, errors.New(ReasonDominanceCycle)
		}
		parent[edge.DominatedID] = edge.DominatorID
	}
	out := map[string]string{}
	for id := range known {
		seen := map[string]bool{id: true}
		root := id
		for parent[root] != "" {
			root = parent[root]
			if seen[root] {
				return nil, errors.New(ReasonDominanceCycle)
			}
			seen[root] = true
		}
		out[id] = root
	}
	return out, nil
}

func blockerClassFromQuestions(ids []string, questions map[string]ClassifiedQuestion) string {
	best := ClassActiveUnresolved
	for _, id := range ids {
		switch questions[id].ResolutionClass {
		case ClassStaticProbeAnswerable:
			return ClassStaticProbeAnswerable
		case ClassArchitectJudgementRequired:
			best = ClassArchitectJudgementRequired
		case ClassMechanicallyAnswerable:
			if best == ClassActiveUnresolved {
				best = ClassMechanicallyAnswerable
			}
		case ClassUncertifiable:
			return ClassUncertifiable
		}
	}
	return best
}

func buildGroups(blockers []ClassifiedBlocker, questions []ClassifiedQuestion, probes []ClassifiedProbe, members map[string][]string, groupFor map[string]string) []BlockerGroup {
	byID := map[string]ClassifiedBlocker{}
	for _, blocker := range blockers {
		byID[blocker.ID] = blocker
	}
	questionGroups := map[string][]string{}
	for _, q := range questions {
		for _, blockerID := range q.BlockerIDs {
			questionGroups[groupFor[blockerID]] = append(questionGroups[groupFor[blockerID]], q.ID)
		}
	}
	probeGroups := map[string][]string{}
	questionByID := map[string]ClassifiedQuestion{}
	for _, q := range questions {
		questionByID[q.ID] = q
	}
	for _, p := range probes {
		for _, blockerID := range questionByID[p.QuestionID].BlockerIDs {
			probeGroups[groupFor[blockerID]] = append(probeGroups[groupFor[blockerID]], p.ID)
		}
	}
	var out []BlockerGroup
	for groupID, ids := range members {
		ids = clean(ids)
		root := ids[0]
		var files, consequences []string
		for _, id := range ids {
			b := byID[id]
			files = append(files, b.Files...)
			consequences = append(consequences, b.Consequence)
			if b.Disposition != ClassDuplicate && b.Disposition != ClassDominated && blockerLess(b, byID[root]) {
				root = id
			}
		}
		var dependent []string
		for _, id := range ids {
			if id != root {
				dependent = append(dependent, id)
			}
		}
		out = append(out, BlockerGroup{ID: groupID, RootBlockerID: root, DependentBlockerIDs: clean(dependent), QuestionIDs: clean(questionGroups[groupID]), ProbeIDs: clean(probeGroups[groupID]), AffectedFiles: clean(files), AdmissionConsequences: clean(consequences)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func summarize(blockers []ClassifiedBlocker, groups []BlockerGroup) Summary {
	s := Summary{TotalBlockers: len(blockers)}
	byID := make(map[string]ClassifiedBlocker, len(blockers))
	for _, blocker := range blockers {
		byID[blocker.ID] = blocker
		switch blocker.Disposition {
		case ClassMechanicallyAnswerable:
			s.MechanicallyResolved++
		case ClassStaticProbeAnswerable:
			s.StaticProbeAnswerable++
		case ClassArchitectJudgementRequired:
			s.ArchitectJudgementRequired++
		case ClassNonBlockingUnknown:
			s.NonBlockingUnknown++
		case ClassDominated:
			s.Dominated++
		case ClassDuplicate:
			s.Duplicate++
		case ClassActiveUnresolved:
			s.ActiveUnresolved++
		case ClassUncertifiable:
			s.Uncertifiable++
		}
	}
	for _, group := range groups {
		root := byID[group.RootBlockerID]
		if root.LoadBearing && root.Disposition != ClassDominated && root.Disposition != ClassDuplicate && root.Disposition != ClassMechanicallyAnswerable {
			s.ActiveRootBlockers++
		}
	}
	return s
}

func summarizeEvidence(probes []ClassifiedProbe) EvidenceProgress {
	s := EvidenceProgress{Total: len(probes)}
	for _, p := range probes {
		switch p.Disposition {
		case ProbeEligible:
			s.Eligible++
		case ProbeCompleted:
			s.Completed++
		case ProbeInconclusive:
			s.Inconclusive++
		case ProbeFailed:
			s.Failed++
		case ProbeRejected:
			s.Rejected++
		case ProbeUnavailable:
			s.Unavailable++
		}
	}
	return s
}

func primaryBlocker(blockers []ClassifiedBlocker) *ClassifiedBlocker {
	var candidates []ClassifiedBlocker
	for _, blocker := range blockers {
		if blocker.LoadBearing && blocker.Disposition != ClassDuplicate && blocker.Disposition != ClassDominated && blocker.Disposition != ClassMechanicallyAnswerable {
			candidates = append(candidates, blocker)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return blockerLess(candidates[i], candidates[j]) })
	return &candidates[0]
}

func primaryQuestion(questions []ClassifiedQuestion) *ClassifiedQuestion {
	var candidates []ClassifiedQuestion
	for _, q := range questions {
		if q.ResolutionClass == ClassArchitectJudgementRequired {
			candidates = append(candidates, q)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if priorityRank(candidates[i].Priority) != priorityRank(candidates[j].Priority) {
			return priorityRank(candidates[i].Priority) < priorityRank(candidates[j].Priority)
		}
		if len(candidates[i].BlockerIDs) != len(candidates[j].BlockerIDs) {
			return len(candidates[i].BlockerIDs) > len(candidates[j].BlockerIDs)
		}
		return candidates[i].ID < candidates[j].ID
	})
	return &candidates[0]
}

func selectNextAction(state TaskControlState, bindingHealthy bool) NextAction {
	if !bindingHealthy {
		return NextAction{Kind: ActionRepairBinding, Summary: "repair the stale or invalid task binding"}
	}
	if state.Evidence.Eligible > 0 {
		return NextAction{Kind: ActionRunStaticEvidence, TargetID: firstEligibleProbe(state.Probes), Summary: "execute the next bounded static evidence batch", CommandHint: "sensei advance-task --active"}
	}
	for _, q := range state.Questions {
		if q.ResolutionClass == ClassActiveUnresolved {
			return NextAction{Kind: ActionProvideExternalEvidence, TargetID: q.ID, Summary: "provide the missing evidence for the primary unresolved question"}
		}
	}
	if state.PrimaryQuestion != nil {
		return NextAction{Kind: ActionAnswerArchitectQuestion, TargetID: state.PrimaryQuestion.ID, Summary: state.PrimaryQuestion.QuestionText}
	}
	if state.Summary.ActiveRootBlockers > 0 {
		return NextAction{Kind: ActionAdvanceConvergence, TargetID: state.TaskID, Summary: "advance one convergence iteration with the current evidence"}
	}
	if strings.Contains(state.Permission.Modify, "admitted") {
		return NextAction{Kind: ActionPerformAdmittedEdit, TargetID: state.TaskID, Summary: "perform only the admitted edit"}
	}
	if state.Permission.Modify == "waiting" {
		return NextAction{Kind: ActionRequestMutation, TargetID: state.TaskID, Summary: "request mutation admission for the exact scope"}
	}
	return NextAction{Kind: ActionCompleteTask, TargetID: state.TaskID, Summary: "complete the task and preserve final receipts"}
}

func validateAccounting(state TaskControlState) error {
	s := state.Summary
	accounted := s.MechanicallyResolved + s.StaticProbeAnswerable + s.ArchitectJudgementRequired + s.NonBlockingUnknown + s.Dominated + s.Duplicate + s.ActiveUnresolved + s.Uncertifiable
	if accounted != s.TotalBlockers {
		return fmt.Errorf("task.control.accounting_mismatch: blockers=%d accounted=%d", s.TotalBlockers, accounted)
	}
	for _, q := range state.Questions {
		if q.ResolutionClass == "" {
			return fmt.Errorf("task.control.accounting_mismatch: question %s has no resolution_class", q.ID)
		}
	}
	for _, p := range state.Probes {
		if p.Disposition == "" {
			return fmt.Errorf("task.control.accounting_mismatch: probe %s has no disposition", p.ID)
		}
	}
	if state.NextAction.Kind == "" {
		return errors.New(ReasonNoPrimaryAction)
	}
	return nil
}

func MarshalYAML(state TaskControlState) ([]byte, error) {
	state.ReceiptDigestSHA256 = StateDigest(state)
	return yaml.Marshal(Envelope{TaskControl: state})
}

func UnmarshalYAML(data []byte) (TaskControlState, error) {
	var env Envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return TaskControlState{}, err
	}
	if env.TaskControl.SchemaVersion != SchemaVersion {
		return TaskControlState{}, errors.New("missing or unsupported task_control schema")
	}
	if env.TaskControl.ReceiptDigestSHA256 != StateDigest(env.TaskControl) {
		return TaskControlState{}, errors.New("task control digest mismatch")
	}
	return env.TaskControl, nil
}

func StateDigest(state TaskControlState) string {
	state = normalizeDigestState(state)
	state.ReceiptDigestSHA256 = ""
	raw, _ := json.Marshal(state)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeDigestState(state TaskControlState) TaskControlState {
	state.Permission.ExactScope = nilIfEmpty(state.Permission.ExactScope)
	state.Blockers = nilBlockersIfEmpty(state.Blockers)
	for i := range state.Blockers {
		state.Blockers[i].QuestionIDs = nilIfEmpty(state.Blockers[i].QuestionIDs)
		state.Blockers[i].ProbeIDs = nilIfEmpty(state.Blockers[i].ProbeIDs)
		state.Blockers[i].ClaimIDs = nilIfEmpty(state.Blockers[i].ClaimIDs)
		state.Blockers[i].NodeIDs = nilIfEmpty(state.Blockers[i].NodeIDs)
		state.Blockers[i].Files = nilIfEmpty(state.Blockers[i].Files)
		state.Blockers[i].ReasonCodes = nilIfEmpty(state.Blockers[i].ReasonCodes)
	}
	state.Questions = nilQuestionsIfEmpty(state.Questions)
	for i := range state.Questions {
		state.Questions[i].AnswerabilityBasis = nilIfEmpty(state.Questions[i].AnswerabilityBasis)
		state.Questions[i].BlockerIDs = nilIfEmpty(state.Questions[i].BlockerIDs)
		state.Questions[i].ClaimIDs = nilIfEmpty(state.Questions[i].ClaimIDs)
		state.Questions[i].ProbeIDs = nilIfEmpty(state.Questions[i].ProbeIDs)
	}
	state.Probes = nilProbesIfEmpty(state.Probes)
	for i := range state.Probes {
		state.Probes[i].ReasonCodes = nilIfEmpty(state.Probes[i].ReasonCodes)
		state.Probes[i].TargetFiles = nilIfEmpty(state.Probes[i].TargetFiles)
	}
	state.Groups = nilGroupsIfEmpty(state.Groups)
	for i := range state.Groups {
		state.Groups[i].DependentBlockerIDs = nilIfEmpty(state.Groups[i].DependentBlockerIDs)
		state.Groups[i].QuestionIDs = nilIfEmpty(state.Groups[i].QuestionIDs)
		state.Groups[i].ProbeIDs = nilIfEmpty(state.Groups[i].ProbeIDs)
		state.Groups[i].AffectedFiles = nilIfEmpty(state.Groups[i].AffectedFiles)
		state.Groups[i].AdmissionConsequences = nilIfEmpty(state.Groups[i].AdmissionConsequences)
	}
	state.Limitations = nilIfEmpty(state.Limitations)
	state.Receipts = nilIfEmpty(state.Receipts)
	return state
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

func nilBlockersIfEmpty(values []ClassifiedBlocker) []ClassifiedBlocker {
	if len(values) == 0 {
		return nil
	}
	return values
}

func nilQuestionsIfEmpty(values []ClassifiedQuestion) []ClassifiedQuestion {
	if len(values) == 0 {
		return nil
	}
	return values
}

func nilProbesIfEmpty(values []ClassifiedProbe) []ClassifiedProbe {
	if len(values) == 0 {
		return nil
	}
	return values
}

func nilGroupsIfEmpty(values []BlockerGroup) []BlockerGroup {
	if len(values) == 0 {
		return nil
	}
	return values
}

func hasNormativeAnswerType(types []string) bool {
	for _, typ := range types {
		switch typ {
		case architecture.AnswerTypeDesiredDirection, architecture.AnswerTypeExceptionAuthorization, architecture.AnswerTypeIntentStatement, architecture.AnswerTypeGovernedDecisionCandidate:
			return true
		}
	}
	return false
}

func bindingEqual(a, b architecture.ClaimDocumentBinding) bool { return a == b }

func normalizeClassifiedProbe(p ClassifiedProbe) ClassifiedProbe {
	p.TargetFiles = clean(p.TargetFiles)
	p.ReasonCodes = clean(p.ReasonCodes)
	return p
}

func sortState(state *TaskControlState) {
	sort.SliceStable(state.Blockers, func(i, j int) bool { return state.Blockers[i].ID < state.Blockers[j].ID })
	sort.SliceStable(state.Questions, func(i, j int) bool { return state.Questions[i].ID < state.Questions[j].ID })
	sort.SliceStable(state.Probes, func(i, j int) bool { return state.Probes[i].ID < state.Probes[j].ID })
}

func blockerLess(a, b ClassifiedBlocker) bool {
	if severityRank(a.Severity) != severityRank(b.Severity) {
		return severityRank(a.Severity) < severityRank(b.Severity)
	}
	if dispositionRank(a.Disposition) != dispositionRank(b.Disposition) {
		return dispositionRank(a.Disposition) < dispositionRank(b.Disposition)
	}
	return a.ID < b.ID
}

func severityRank(v string) int {
	switch v {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium", "warning":
		return 2
	default:
		return 3
	}
}
func priorityRank(v string) int {
	switch v {
	case architecture.QuestionPriorityCritical:
		return 0
	case architecture.QuestionPriorityHigh:
		return 1
	case architecture.QuestionPriorityMedium:
		return 2
	default:
		return 3
	}
}
func dispositionRank(v string) int {
	switch v {
	case ClassUncertifiable:
		return 0
	case ClassArchitectJudgementRequired:
		return 1
	case ClassActiveUnresolved:
		return 2
	case ClassStaticProbeAnswerable:
		return 3
	default:
		return 4
	}
}

func firstEligibleProbe(probes []ClassifiedProbe) string {
	for _, p := range probes {
		if p.Disposition == ProbeEligible {
			return p.ID
		}
	}
	return ""
}

func clean(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

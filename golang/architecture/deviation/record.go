// SPDX-License-Identifier: AGPL-3.0-only

package deviation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
)

var (
	sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)
	tokenRE  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/-]*$`)
	predicateRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
)

// Record creates one immutable deviation receipt from exact caller input.
func Record(in RecordInput) (Receipt, error) {
	receipt := Receipt{
		SchemaVersion: SchemaVersion,
		GeneratedBy: GeneratedBy,
		Kind: in.Kind,
		Binding: in.Binding,
		Scope: in.Scope,
		Shape: in.Shape,
		ExpectedBehavior: in.Expected,
		ObservedBehavior: in.Observed,
		TaskID: in.TaskID,
		TaskSessionID: in.TaskSessionID,
		AgentID: in.AgentID,
		ChangeDigestSHA256: in.ChangeDigest,
		SourceArtifactDigestSHA256: in.SourceDigest,
		RelatedClaimIDs: in.RelatedClaims,
		EvidenceRefs: in.EvidenceRefs,
		RecordedAt: in.RecordedAt,
		TimestampSource: in.Timestamp,
	}
	receipt = canonicalizeReceipt(receipt)
	receipt.IndependenceKey = expectedIndependenceKey(receipt)
	receipt.ID = expectedReceiptID(receipt)
	digest, err := ReceiptDigest(receipt)
	if err != nil {
		return Receipt{}, err
	}
	receipt.SemanticDigestSHA256 = digest
	if err := ValidateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// NormalizeReceipts validates, deduplicates, and sorts immutable receipts.
func NormalizeReceipts(receipts []Receipt) ([]Receipt, error) {
	out := make([]Receipt, 0, len(receipts))
	seen := map[string]Receipt{}
	for _, receipt := range receipts {
		receipt = canonicalizeReceipt(receipt)
		if err := ValidateReceipt(receipt); err != nil {
			return nil, err
		}
		if existing, ok := seen[receipt.ID]; ok {
			if existing.SemanticDigestSHA256 != receipt.SemanticDigestSHA256 {
				return nil, fmt.Errorf("deviation receipt id collision for %s", receipt.ID)
			}
			continue
		}
		seen[receipt.ID] = receipt
		out = append(out, receipt)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ValidateReceipt proves exact event identity and immutable semantic content.
func ValidateReceipt(in Receipt) error {
	receipt := canonicalizeReceipt(in)
	var errs []string
	if receipt.SchemaVersion != SchemaVersion {
		errs = append(errs, "deviation schema version must be exact")
	}
	if receipt.GeneratedBy != GeneratedBy {
		errs = append(errs, "deviation generated_by must be exact")
	}
	if !IsValidKind(receipt.Kind) {
		errs = append(errs, "unknown deviation kind")
	}
	if err := validateExactBinding(receipt.Binding); err != nil {
		errs = append(errs, err.Error())
	}
	if receipt.Scope.Repository == "" || receipt.Scope.Repository != receipt.Binding.RepositoryDomain {
		errs = append(errs, "deviation scope repository must exactly match binding")
	}
	if receipt.Shape.Subject == "" || receipt.Shape.Predicate == "" || receipt.Shape.Object == "" {
		errs = append(errs, "deviation shape subject, predicate, and object are required")
	}
	if receipt.Shape.Predicate != "" && !predicateRE.MatchString(receipt.Shape.Predicate) {
		errs = append(errs, "deviation shape predicate must be a conservative token")
	}
	if receipt.ExpectedBehavior == "" || receipt.ObservedBehavior == "" {
		errs = append(errs, "expected and observed behavior are required")
	}
	if receipt.TaskID == "" || receipt.TaskSessionID == "" || receipt.AgentID == "" {
		errs = append(errs, "task id, task session id, and agent id are required")
	}
	for name, value := range map[string]string{
		"change digest": receipt.ChangeDigestSHA256,
		"source artifact digest": receipt.SourceArtifactDigestSHA256,
		"semantic digest": receipt.SemanticDigestSHA256,
	} {
		if !sha256RE.MatchString(value) {
			errs = append(errs, name+" must be lowercase SHA-256")
		}
	}
	if receipt.IndependenceKey == "" || receipt.IndependenceKey != expectedIndependenceKey(receipt) {
		errs = append(errs, "deviation independence key must exactly match task and change binding")
	}
	if receipt.ID == "" || receipt.ID != expectedReceiptID(receipt) {
		errs = append(errs, "deviation id must exactly match semantic event identity")
	}
	for _, claimID := range receipt.RelatedClaimIDs {
		if !strings.HasPrefix(claimID, "claim.") || !tokenRE.MatchString(claimID) {
			errs = append(errs, "related claim ids must be stable claim identifiers")
			break
		}
	}
	if len(receipt.EvidenceRefs) == 0 {
		errs = append(errs, "at least one evidence reference is required")
	}
	for _, ref := range receipt.EvidenceRefs {
		if _, _, ok := architecture.ParseClassQualifiedReference(ref); !ok {
			errs = append(errs, "evidence references must be class-qualified")
			break
		}
	}
	if receipt.RecordedAt == "" {
		errs = append(errs, "recorded_at is required")
	} else if _, err := time.Parse(time.RFC3339, receipt.RecordedAt); err != nil {
		errs = append(errs, "recorded_at must be RFC3339")
	}
	if receipt.TimestampSource == "" {
		errs = append(errs, "timestamp source is required")
	}
	if err := validateScope(receipt.Scope); err != nil {
		errs = append(errs, err.Error())
	}
	actualDigest, err := ReceiptDigest(receipt)
	if err != nil {
		errs = append(errs, err.Error())
	} else if receipt.SemanticDigestSHA256 != actualDigest {
		errs = append(errs, "deviation semantic digest mismatch")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ReceiptDigest returns the semantic digest with only its self-reference cleared.
func ReceiptDigest(in Receipt) (string, error) {
	receipt := canonicalizeReceipt(in)
	receipt.SemanticDigestSHA256 = ""
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func expectedReceiptID(in Receipt) string {
	receipt := canonicalizeReceipt(in)
	receipt.ID = ""
	receipt.SemanticDigestSHA256 = ""
	data, _ := json.Marshal(receipt)
	return "deviation." + sha256Bytes(data)[:24]
}

func expectedIndependenceKey(in Receipt) string {
	receipt := canonicalizeReceipt(in)
	parts := []string{
		receipt.Binding.RepositoryDomain,
		receipt.TaskID,
		receipt.TaskSessionID,
		receipt.ChangeDigestSHA256,
	}
	return "occurrence." + sha256String(strings.Join(parts, "\x00"))[:24]
}

func validateExactBinding(binding architecture.ClaimDocumentBinding) error {
	binding = canonicalizeBinding(binding)
	var errs []string
	if binding.RepositoryDomain == "" {
		errs = append(errs, "binding repository domain is required")
	}
	if binding.GraphDigestStatus != architecture.GraphDigestResolved || !sha256RE.MatchString(binding.GraphDigestSHA256) {
		errs = append(errs, "binding requires a resolved exact graph digest")
	}
	exactRevision := binding.RevisionStatus == architecture.RevisionResolved && binding.Revision != ""
	exactTree := sha256RE.MatchString(binding.TreeDigestSHA256)
	if !exactRevision && !exactTree {
		errs = append(errs, "binding requires an exact revision or repository tree digest")
	}
	if binding.TreeDigestSHA256 != "" && !exactTree {
		errs = append(errs, "binding tree digest must be lowercase SHA-256")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func validateScope(scope architecture.ClaimScope) error {
	for _, file := range scope.Files {
		if filepath.IsAbs(file) || strings.HasPrefix(file, "../") || strings.Contains(file, "/../") || file == ".." {
			return errors.New("deviation scope file paths must be repository-relative and non-escaping")
		}
	}
	return nil
}

func canonicalizeReceipt(in Receipt) Receipt {
	receipt := in
	receipt.SchemaVersion = strings.TrimSpace(receipt.SchemaVersion)
	receipt.GeneratedBy = strings.TrimSpace(receipt.GeneratedBy)
	receipt.ID = strings.TrimSpace(receipt.ID)
	receipt.Binding = canonicalizeBinding(receipt.Binding)
	receipt.Scope = canonicalizeScope(receipt.Scope)
	receipt.Shape.Subject = strings.TrimSpace(receipt.Shape.Subject)
	receipt.Shape.Predicate = strings.TrimSpace(receipt.Shape.Predicate)
	receipt.Shape.Object = strings.TrimSpace(receipt.Shape.Object)
	receipt.ExpectedBehavior = strings.TrimSpace(receipt.ExpectedBehavior)
	receipt.ObservedBehavior = strings.TrimSpace(receipt.ObservedBehavior)
	receipt.TaskID = strings.TrimSpace(receipt.TaskID)
	receipt.TaskSessionID = strings.TrimSpace(receipt.TaskSessionID)
	receipt.AgentID = strings.TrimSpace(receipt.AgentID)
	receipt.ChangeDigestSHA256 = strings.TrimSpace(receipt.ChangeDigestSHA256)
	receipt.SourceArtifactDigestSHA256 = strings.TrimSpace(receipt.SourceArtifactDigestSHA256)
	receipt.IndependenceKey = strings.TrimSpace(receipt.IndependenceKey)
	receipt.RelatedClaimIDs = cleanStrings(receipt.RelatedClaimIDs)
	receipt.EvidenceRefs = cleanClassRefs(receipt.EvidenceRefs)
	receipt.RecordedAt = strings.TrimSpace(receipt.RecordedAt)
	receipt.TimestampSource = strings.TrimSpace(receipt.TimestampSource)
	receipt.SemanticDigestSHA256 = strings.TrimSpace(receipt.SemanticDigestSHA256)
	return receipt
}

func canonicalizeBinding(in architecture.ClaimDocumentBinding) architecture.ClaimDocumentBinding {
	binding := in
	binding.RepositoryDomain = strings.TrimSpace(binding.RepositoryDomain)
	binding.Revision = strings.TrimSpace(binding.Revision)
	binding.RevisionStatus = strings.TrimSpace(binding.RevisionStatus)
	binding.TreeDigestSHA256 = strings.TrimSpace(binding.TreeDigestSHA256)
	binding.GraphDigestSHA256 = strings.TrimSpace(binding.GraphDigestSHA256)
	binding.GraphDigestStatus = strings.TrimSpace(binding.GraphDigestStatus)
	return binding
}

func canonicalizeScope(in architecture.ClaimScope) architecture.ClaimScope {
	scope := in
	scope.Repository = strings.TrimSpace(scope.Repository)
	scope.Repo = strings.TrimSpace(scope.Repo)
	if scope.Repository == "" {
		scope.Repository = scope.Repo
	}
	if scope.Repo == "" {
		scope.Repo = scope.Repository
	}
	scope.Domain = strings.TrimSpace(scope.Domain)
	scope.SourceSet = strings.TrimSpace(scope.SourceSet)
	files := make([]string, 0, len(scope.Files))
	for _, file := range scope.Files {
		file = strings.TrimSpace(strings.ReplaceAll(file, "\\", "/"))
		if file != "" {
			files = append(files, path.Clean(file))
		}
	}
	scope.Files = cleanStrings(files)
	scope.Symbols = cleanStrings(scope.Symbols)
	scope.Components = cleanStrings(scope.Components)
	return scope
}

func cleanClassRefs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, ref := range in {
		class, id, ok := architecture.ParseClassQualifiedReference(ref)
		if ok {
			out = append(out, class+":"+id)
			continue
		}
		if strings.TrimSpace(ref) != "" {
			out = append(out, strings.TrimSpace(ref))
		}
	}
	return cleanStrings(out)
}

func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256Bytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

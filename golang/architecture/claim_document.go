// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ClaimDocument struct {
	SchemaVersion string               `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string               `json:"generated_by" yaml:"generated_by"`
	Binding       ClaimDocumentBinding `json:"binding" yaml:"binding"`
	FactReceipts  []ClaimFactReceipt   `json:"fact_receipts,omitempty" yaml:"fact_receipts,omitempty"`
	Claims        []Claim              `json:"claims" yaml:"claims"`
	Limitations   []Limitation         `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ClaimDocumentBinding struct {
	RepositoryDomain  string `json:"repository_domain" yaml:"repository_domain"`
	Revision          string `json:"revision,omitempty" yaml:"revision,omitempty"`
	RevisionStatus    string `json:"revision_status" yaml:"revision_status"`
	GraphDigestSHA256 string `json:"graph_digest_sha256,omitempty" yaml:"graph_digest_sha256,omitempty"`
	GraphDigestStatus string `json:"graph_digest_status" yaml:"graph_digest_status"`
}

type ClaimFactReceipt struct {
	Fact       Fact       `json:"fact" yaml:"fact"`
	Provenance Provenance `json:"provenance" yaml:"provenance"`
}

type claimDocumentEnvelope struct {
	ArchitectureClaims ClaimDocument `json:"architecture_claims" yaml:"architecture_claims"`
}

func NormalizeClaimDocument(in ClaimDocument) (ClaimDocument, error) {
	doc := canonicalizeClaimDocument(in)
	receipts := make([]ClaimFactReceipt, 0, len(doc.FactReceipts))
	for _, r := range doc.FactReceipts {
		f := canonicalizeFact(r.Fact)
		p := canonicalizeProvenance(r.Provenance)
		f.Provenance = &p
		receipts = append(receipts, ClaimFactReceipt{Fact: f, Provenance: p})
	}
	sort.SliceStable(receipts, func(i, j int) bool { return receipts[i].Fact.ID < receipts[j].Fact.ID })
	doc.FactReceipts = receipts

	claims, err := NormalizeClaims(doc.Claims)
	if err != nil {
		return ClaimDocument{}, err
	}
	doc.Claims = claims
	if err := ValidateClaimDocument(doc); err != nil {
		return ClaimDocument{}, err
	}
	return doc, nil
}

func ValidateClaimDocument(doc ClaimDocument) error {
	var errs []string
	doc = canonicalizeClaimDocument(doc)
	if doc.Binding.RevisionStatus == "" {
		errs = append(errs, "binding revision_status is required")
	}
	if doc.Binding.GraphDigestStatus == "" {
		errs = append(errs, "binding graph_digest_status is required")
	}
	if doc.Binding.RevisionStatus != "" && !oneOf(doc.Binding.RevisionStatus, RevisionResolved, RevisionUnavailable, RevisionNotGit, RevisionNotRequested) {
		errs = append(errs, "binding revision_status is invalid")
	}
	if doc.Binding.GraphDigestStatus != "" && !oneOf(doc.Binding.GraphDigestStatus, GraphDigestResolved, GraphDigestUnavailable, GraphDigestNotRequested) {
		errs = append(errs, "binding graph_digest_status is invalid")
	}

	receipts := map[string]ClaimFactReceipt{}
	for _, r := range doc.FactReceipts {
		r.Fact = canonicalizeFact(r.Fact)
		r.Provenance = canonicalizeProvenance(r.Provenance)
		r.Fact.Provenance = &r.Provenance
		if err := ValidateFact(r.Fact); err != nil {
			errs = append(errs, fmt.Sprintf("fact receipt %s: %v", r.Fact.ID, err))
		}
		if r.Provenance.RevisionStatus == "" || r.Provenance.SourceDigestStatus == "" || r.Provenance.RepositoryDomainStatus == "" {
			errs = append(errs, fmt.Sprintf("fact receipt %s provenance statuses must be explicit", r.Fact.ID))
		}
		if doc.Binding.RevisionStatus == RevisionResolved && r.Provenance.RevisionStatus == RevisionResolved && r.Provenance.Revision != doc.Binding.Revision {
			errs = append(errs, fmt.Sprintf("fact receipt %s revision does not match document binding", r.Fact.ID))
		}
		if doc.Binding.RepositoryDomain != "" && r.Provenance.RepositoryDomainStatus == RepositoryDomainResolved && r.Provenance.RepositoryDomain != doc.Binding.RepositoryDomain {
			errs = append(errs, fmt.Sprintf("fact receipt %s repository domain does not match document binding", r.Fact.ID))
		}
		receipts[r.Fact.ID] = r
	}

	claims := map[string]Claim{}
	for _, c := range doc.Claims {
		claims[c.ID] = c
	}
	for _, c := range doc.Claims {
		if err := ValidateClaim(c); err != nil {
			errs = append(errs, fmt.Sprintf("claim %s: %v", c.ID, err))
			continue
		}
		if c.AssertionOrigin != OriginDerived {
			errs = append(errs, fmt.Sprintf("claim %s: generated claim document accepts only derived origin", c.ID))
		}
		if !bindingCanSupportStatus(doc.Binding, c.EpistemicStatus) {
			errs = append(errs, fmt.Sprintf("claim %s: status %s requires resolved revision and graph digest", c.ID, c.EpistemicStatus))
		}
		premiseFiles := map[string]bool{}
		premiseSymbols := map[string]bool{}
		resolvedPremises := 0
		for _, id := range c.PremiseFacts {
			r, ok := receipts[id]
			if !ok {
				errs = append(errs, fmt.Sprintf("claim %s: unknown premise fact %s", c.ID, id))
				continue
			}
			resolvedPremises++
			for _, file := range r.Fact.Scope.Files {
				premiseFiles[file] = true
			}
			for _, symbol := range r.Fact.Scope.Symbols {
				premiseSymbols[symbol] = true
			}
		}
		if resolvedPremises > 0 {
			if !claimScopeWithinFact(c.Scope.Files, stringSetKeys(premiseFiles)) {
				errs = append(errs, fmt.Sprintf("claim %s: source files invent anchors absent from premise facts", c.ID))
			}
			if !claimScopeWithinFact(c.Scope.Symbols, stringSetKeys(premiseSymbols)) {
				errs = append(errs, fmt.Sprintf("claim %s: symbols invent anchors absent from premise facts", c.ID))
			}
		}
		for _, id := range c.DependsOnClaims {
			if _, ok := claims[id]; !ok {
				errs = append(errs, fmt.Sprintf("claim %s: missing dependent claim %s", c.ID, id))
			}
		}
		for _, id := range c.ConflictsWith {
			if _, ok := claims[id]; !ok {
				errs = append(errs, fmt.Sprintf("claim %s: missing conflicting claim %s", c.ID, id))
			}
		}
		if c.SupersededBy != "" {
			if _, ok := claims[c.SupersededBy]; !ok {
				errs = append(errs, fmt.Sprintf("claim %s: missing superseding claim %s", c.ID, c.SupersededBy))
			}
		}
		if err := validateClaimStatusShape(c, len(doc.Limitations) > 0); err != nil {
			errs = append(errs, fmt.Sprintf("claim %s: %v", c.ID, err))
		}
	}
	if cycle := claimDependencyCycle(doc.Claims); cycle != "" {
		errs = append(errs, "claim dependency cycle: "+cycle)
	}
	if cycle := claimSupersessionCycle(doc.Claims); cycle != "" {
		errs = append(errs, "claim supersession cycle: "+cycle)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func stringSetKeys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for item := range in {
		out = append(out, item)
	}
	return out
}

func MarshalCanonicalClaimDocument(doc ClaimDocument) ([]byte, error) {
	doc, err := NormalizeClaimDocument(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func MarshalCanonicalClaimDocumentYAML(doc ClaimDocument) ([]byte, error) {
	doc, err := NormalizeClaimDocument(doc)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(claimDocumentEnvelope{ArchitectureClaims: doc})
}

func UnmarshalClaimDocumentYAML(data []byte) (ClaimDocument, error) {
	var env claimDocumentEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return ClaimDocument{}, err
	}
	if env.ArchitectureClaims.SchemaVersion == "" && env.ArchitectureClaims.GeneratedBy == "" && len(env.ArchitectureClaims.Claims) == 0 {
		return ClaimDocument{}, errors.New("missing architecture_claims document")
	}
	return NormalizeClaimDocument(env.ArchitectureClaims)
}

func LoadClaimDocument(path string) (ClaimDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ClaimDocument{}, err
	}
	return UnmarshalClaimDocumentYAML(data)
}

func canonicalizeClaimDocument(in ClaimDocument) ClaimDocument {
	doc := in
	doc.SchemaVersion = strings.TrimSpace(doc.SchemaVersion)
	doc.GeneratedBy = strings.TrimSpace(doc.GeneratedBy)
	doc.Binding.RepositoryDomain = strings.TrimSpace(doc.Binding.RepositoryDomain)
	doc.Binding.Revision = strings.TrimSpace(doc.Binding.Revision)
	doc.Binding.RevisionStatus = strings.TrimSpace(doc.Binding.RevisionStatus)
	doc.Binding.GraphDigestSHA256 = strings.TrimSpace(doc.Binding.GraphDigestSHA256)
	doc.Binding.GraphDigestStatus = strings.TrimSpace(doc.Binding.GraphDigestStatus)
	return doc
}

func canonicalizeProvenance(in Provenance) Provenance {
	p := in
	p.RepositoryDomain = strings.TrimSpace(p.RepositoryDomain)
	p.RepositoryDomainStatus = strings.TrimSpace(p.RepositoryDomainStatus)
	p.Revision = strings.TrimSpace(p.Revision)
	p.RevisionStatus = strings.TrimSpace(p.RevisionStatus)
	p.SourceDigest = strings.TrimSpace(p.SourceDigest)
	p.SourceDigestStatus = strings.TrimSpace(p.SourceDigestStatus)
	p.SourceKind = strings.TrimSpace(p.SourceKind)
	return p
}

func bindingCanSupportStatus(b ClaimDocumentBinding, status string) bool {
	if status == StatusUnknown || status == StatusStale {
		return true
	}
	return b.RevisionStatus == RevisionResolved && b.GraphDigestStatus == GraphDigestResolved
}

func claimScopeWithinFact(claimItems, factItems []string) bool {
	if len(claimItems) == 0 {
		return true
	}
	allowed := map[string]bool{}
	for _, item := range factItems {
		allowed[item] = true
	}
	for _, item := range claimItems {
		if !allowed[item] {
			return false
		}
	}
	return true
}

func validateClaimStatusShape(c Claim, hasLimitation bool) error {
	switch c.EpistemicStatus {
	case StatusUnknown:
		if len(c.Unknowns) == 0 && len(c.AlternativeExplanations) == 0 && !hasLimitation {
			return errors.New("unknown status requires unknowns, alternatives, or binding limitation")
		}
	case StatusSupported:
		if len(c.PremiseFacts)+len(c.SupportingEvidence)+len(c.DependsOnClaims) == 0 {
			return errors.New("supported status requires premise facts or supporting evidence")
		}
		if len(c.RefutingEvidence) > 0 || len(c.ConflictsWith) > 0 {
			return errors.New("supported status must not include refuting evidence or conflicts")
		}
		if len(c.InvalidationConditions) == 0 {
			return errors.New("supported status requires invalidation conditions")
		}
	case StatusContested:
		if len(c.PremiseFacts)+len(c.SupportingEvidence) == 0 {
			return errors.New("contested status requires support")
		}
		if len(c.RefutingEvidence)+len(c.ConflictsWith) == 0 {
			return errors.New("contested status requires refutation or conflict")
		}
	case StatusRefuted:
		if len(c.RefutingEvidence) == 0 {
			return errors.New("refuted status requires refuting evidence")
		}
	case StatusStale:
		if c.Freshness != "stale" {
			return errors.New("stale status requires freshness=stale")
		}
		if len(c.InvalidationConditions) == 0 && !hasLimitation {
			return errors.New("stale status requires invalidation condition or stale binding reason")
		}
	case StatusSuperseded:
		if c.SupersededBy == "" {
			return errors.New("superseded status requires superseded_by")
		}
	}
	return nil
}

func claimDependencyCycle(claims []Claim) string {
	edges := map[string][]string{}
	for _, c := range claims {
		edges[c.ID] = append([]string{}, c.DependsOnClaims...)
	}
	return directedCycle(edges)
}

func claimSupersessionCycle(claims []Claim) string {
	edges := map[string][]string{}
	for _, c := range claims {
		if c.SupersededBy != "" {
			edges[c.ID] = []string{c.SupersededBy}
		} else {
			edges[c.ID] = nil
		}
	}
	return directedCycle(edges)
}

func directedCycle(edges map[string][]string) string {
	const (
		unseen = 0
		active = 1
		done   = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) string
	visit = func(id string) string {
		switch state[id] {
		case active:
			for i, item := range stack {
				if item == id {
					return strings.Join(append(stack[i:], id), " -> ")
				}
			}
			return id + " -> " + id
		case done:
			return ""
		}
		state[id] = active
		stack = append(stack, id)
		next := append([]string{}, edges[id]...)
		sort.Strings(next)
		for _, dep := range next {
			if _, ok := edges[dep]; !ok {
				continue
			}
			if cycle := visit(dep); cycle != "" {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return ""
	}
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if cycle := visit(id); cycle != "" {
			return cycle
		}
	}
	return ""
}

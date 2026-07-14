// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/inference"
	"gopkg.in/yaml.v3"
)

type inferClaimsOptions struct {
	Repo              string
	RepositoryDomain  string
	Format            string
	Output            string
	Check             bool
	IncludeDocs       bool
	IncludeTests      bool
	IncludeHistory    bool
	GraphDigest       string
	GraphDigestStatus string
	ListRules         bool
	Rules             repeatStrings
}

type repeatStrings []string

func (r *repeatStrings) String() string { return strings.Join(*r, ",") }
func (r *repeatStrings) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func runInferClaims(args []string) int {
	fs := flag.NewFlagSet("sensei infer-claims", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := inferClaimsOptions{}
	fs.StringVar(&opts.Repo, "repo", ".", "repository root to inspect")
	fs.StringVar(&opts.RepositoryDomain, "repo-domain", "", "authoritative repository domain for claim and fact bindings")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write claim document to this path instead of stdout")
	fs.BoolVar(&opts.Check, "check", false, "compare --output with fresh deterministic inference")
	fs.BoolVar(&opts.IncludeDocs, "include-docs", true, "include documentation facts in extraction")
	fs.BoolVar(&opts.IncludeTests, "include-tests", true, "include test facts in extraction")
	fs.BoolVar(&opts.IncludeHistory, "include-history", false, "include optional git-history facts")
	fs.StringVar(&opts.GraphDigest, "graph-digest", "", "explicit verified graph digest for claim binding")
	fs.StringVar(&opts.GraphDigestStatus, "graph-digest-status", architecture.GraphDigestNotRequested, "graph digest status: resolved | unavailable | not_requested")
	fs.BoolVar(&opts.ListRules, "list-rules", false, "list deterministic inference rules without scanning the repository")
	fs.Var(&opts.Rules, "rule", "registered rule id to run; may be repeated")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei infer-claims --repo <checkout> [flags]

Derive non-authoritative ArchitectureClaim candidates from normalized facts.
The command is offline: it does not query or mutate the live graph.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	reg, err := inference.DefaultRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
		return 1
	}
	if opts.ListRules {
		rendered, err := renderRuleDescriptors(reg.Descriptors(), opts.Format)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
			return 2
		}
		fmt.Print(string(rendered))
		return 0
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei infer-claims: --check requires --output")
		return 2
	}
	if err := validateGraphDigestFlags(opts.GraphDigest, opts.GraphDigestStatus); err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
		return 2
	}
	if _, err := reg.Select(opts.Rules); err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: resolve repo: %v\n", err)
		return 1
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: --repo must be an existing directory: %s\n", root)
		return 2
	}
	if opts.Output != "" && !inferClaimsOutputPathAllowed(root, opts.Output) {
		fmt.Fprintln(os.Stderr, "sensei infer-claims: --output under docs/awareness or docs/intent must be inside a candidates directory")
		return 2
	}
	rendered, doc, err := buildInferClaimsOutput(root, opts, reg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
		return 1
	}
	if opts.Check {
		existing, err := os.ReadFile(opts.Output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei infer-claims: read --output: %v\n", err)
			return 1
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(rendered)) {
			fmt.Fprintf(os.Stderr, "infer-claims: STALE - %s differs from fresh inference\n", opts.Output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "infer-claims: fresh (%d claim(s))\n", len(doc.Claims))
		return 0
	}
	if strings.TrimSpace(opts.Output) != "" {
		if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "sensei infer-claims: mkdir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(opts.Output, rendered, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei infer-claims: write: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "infer-claims: wrote %d claim(s) to %s\n", len(doc.Claims), opts.Output)
		return 0
	}
	fmt.Print(string(rendered))
	return 0
}

func buildInferClaimsOutput(root string, opts inferClaimsOptions, reg *inference.Registry) ([]byte, architecture.ClaimDocument, error) {
	result, err := buildInferClaimsResult(root, opts, reg)
	return result.Rendered, result.Document, err
}

type inferClaimsBuildResult struct {
	Rendered            []byte
	Document            architecture.ClaimDocument
	FactCount           int
	GoSemanticFactCount int
}

func buildInferClaimsResult(root string, opts inferClaimsOptions, reg *inference.Registry) (inferClaimsBuildResult, error) {
	report, err := buildInvariantExtractionReport(root, invariantExtractOptions{
		Repo:              root,
		Format:            "json",
		IncludeDocs:       opts.IncludeDocs,
		IncludeTests:      opts.IncludeTests,
		IncludeHistory:    opts.IncludeHistory,
		MinimumConfidence: "low",
	})
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	result := inferClaimsBuildResult{FactCount: len(report.Facts)}
	for _, fact := range report.Facts {
		if fact.Extractor == "go_semantic_extractor" {
			result.GoSemanticFactCount++
		}
	}
	revision, revisionStatus, revisionLimitations := architecture.ResolveRevision(root, true)
	facts := rebindInferenceFactRevision(report.Facts, revision, revisionStatus)
	facts = rebindInferenceFactRepositoryDomain(facts, opts.RepositoryDomain)
	limitations := append([]architecture.Limitation{}, report.Limitations...)
	limitations = append(limitations, revisionLimitations...)
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  inferenceRepositoryDomain(facts, root, opts.RepositoryDomain),
		Revision:          revision,
		RevisionStatus:    revisionStatus,
		GraphDigestSHA256: strings.TrimSpace(opts.GraphDigest),
		GraphDigestStatus: strings.TrimSpace(opts.GraphDigestStatus),
	}
	rules, err := reg.Select(opts.Rules)
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	apps, err := inference.NewEngine(rules).Apply(inference.Context{Binding: binding, Facts: facts, Limitations: limitations})
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	doc, err := inference.BuildClaimDocument(inference.Context{Binding: binding, Facts: facts, Limitations: limitations}, apps)
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	doc.Claims, err = architecture.CompactClaims(doc.Claims)
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	doc, err = architecture.NormalizeClaimDocument(doc)
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	rendered, err := renderInferClaimsDocument(doc, opts.Format)
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	result.Rendered = rendered
	result.Document = doc
	return result, nil
}

func rebindInferenceFactRevision(facts []normalizedInvariantFact, revision, status string) []architecture.Fact {
	out := make([]architecture.Fact, 0, len(facts))
	for _, f := range facts {
		if f.Provenance != nil {
			p := *f.Provenance
			p.Revision = revision
			p.RevisionStatus = status
			f.Provenance = &p
		}
		out = append(out, f)
	}
	return out
}

func rebindInferenceFactRepositoryDomain(facts []architecture.Fact, domain string) []architecture.Fact {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return facts
	}
	out := make([]architecture.Fact, 0, len(facts))
	for _, f := range facts {
		f.Scope.Repository = domain
		if f.Provenance != nil {
			p := *f.Provenance
			p.RepositoryDomain = domain
			p.RepositoryDomainStatus = architecture.RepositoryDomainResolved
			f.Provenance = &p
		}
		out = append(out, f)
	}
	return out
}

func inferenceRepositoryDomain(facts []architecture.Fact, root, explicit string) string {
	if domain := strings.TrimSpace(explicit); domain != "" {
		return domain
	}
	for _, f := range facts {
		if f.Provenance != nil && f.Provenance.RepositoryDomain != "" {
			return f.Provenance.RepositoryDomain
		}
	}
	return filepath.Base(root)
}

func validateGraphDigestFlags(digest, status string) error {
	status = strings.TrimSpace(status)
	digest = strings.TrimSpace(digest)
	switch status {
	case architecture.GraphDigestResolved:
		if digest == "" {
			return fmt.Errorf("--graph-digest-status=resolved requires --graph-digest")
		}
	case architecture.GraphDigestUnavailable, architecture.GraphDigestNotRequested:
		if digest != "" {
			return fmt.Errorf("--graph-digest may only be set when --graph-digest-status=resolved")
		}
	default:
		return fmt.Errorf("--graph-digest-status must be resolved, unavailable, or not_requested")
	}
	return nil
}

func inferClaimsOutputPathAllowed(root, output string) bool {
	out, err := filepath.Abs(output)
	if err != nil {
		return false
	}
	for _, rel := range []string{"docs/awareness", "docs/intent"} {
		base := filepath.Join(root, rel)
		if withinPath(base, out) {
			return hasPathSegment(out, "candidates")
		}
	}
	return true
}

func withinPath(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func hasPathSegment(path, segment string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func renderInferClaimsDocument(doc architecture.ClaimDocument, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return architecture.MarshalCanonicalClaimDocumentYAML(doc)
	case "json":
		doc, err := architecture.NormalizeClaimDocument(doc)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			ArchitectureClaims architecture.ClaimDocument `json:"architecture_claims"`
		}{ArchitectureClaims: doc}); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderRuleDescriptors(desc []inference.RuleDescriptor, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return yaml.Marshal(struct {
			Rules []inference.RuleDescriptor `yaml:"rules"`
		}{Rules: desc})
	case "json":
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Rules []inference.RuleDescriptor `json:"rules"`
		}{Rules: desc}); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

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
	GraphNT           string
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
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "optional canonical graph snapshot used for governed directional claim synthesis")
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
	result, err := buildInferClaimsResult(root, opts, reg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei infer-claims: %v\n", err)
		return 1
	}
	rendered, doc := result.Rendered, result.Document
	if result.UnboundRevisionReason != "" {
		fmt.Fprintf(os.Stderr, "infer-claims: UNBOUND REVISION - %s\n", result.UnboundRevisionReason)
		fmt.Fprintln(os.Stderr, "infer-claims: the document names no revision; commit the cited files to produce a bound corpus")
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

type inferClaimsBuildResult struct {
	Rendered            []byte
	Document            architecture.ClaimDocument
	FactCount           int
	GoSemanticFactCount int
	// UnboundRevisionReason is set when the scan could not be bound to a
	// committed revision, and states why. Empty means the document names a
	// revision that really does contain the bytes the facts cite.
	UnboundRevisionReason string
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
	revision, revisionStatus, revisionLimitations, unboundReason := bindInferClaimsRevision(root, report.Facts)
	result.UnboundRevisionReason = unboundReason
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
	graphPath := strings.TrimSpace(opts.GraphNT)
	if graphPath != "" {
		graphPath = filepath.Clean(graphPath)
	}
	governedFacts, governedLimitations, err := inference.LoadGovernedDirectionFacts(inference.GovernedDirectionOptions{
		Root:      root,
		GraphPath: graphPath,
		Binding:   binding,
	})
	if err != nil {
		return inferClaimsBuildResult{}, err
	}
	facts = append(facts, governedFacts...)
	limitations = append(limitations, governedLimitations...)
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
	doc.Claims = inference.MarkGovernedDirectionConflicts(doc.Claims)
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

// bindInferClaimsRevision decides which revision, if any, the claim document may
// name. Facts are read from the working tree, so resolving HEAD only establishes
// what the last commit is, not that the scan read that commit's bytes. When the
// two disagree the document names no revision and says so in a blocking
// limitation, exactly as it already does for an unresolved graph digest: an
// unbound corpus that reports itself unbound is usable, a corpus bound to a
// revision that does not contain the files it cites is not.
func bindInferClaimsRevision(root string, facts []normalizedInvariantFact) (string, string, []architecture.Limitation, string) {
	revision, status, limitations := architecture.ResolveRevision(root, true)
	if status != architecture.RevisionResolved {
		return revision, status, limitations, ""
	}
	uncommitted, err := architecture.UncommittedSourceFiles(root, revision, inferClaimsCitedFiles(facts))
	var reason string
	switch {
	case err != nil:
		reason = fmt.Sprintf("cannot verify that the scanned working tree matches %s: %v", shortRev(revision), err)
	case len(uncommitted) > 0:
		reason = fmt.Sprintf("scanned working tree differs from %s: %d cited source file(s) are uncommitted or modified (%s)", shortRev(revision), len(uncommitted), summarizeUncommittedPaths(uncommitted))
	default:
		return revision, status, limitations, ""
	}
	limitations = append(limitations, architecture.Limitation{
		Source:   root,
		Scope:    "git_revision",
		Reason:   reason + "; facts are bound to their source digests only, and the document names no revision",
		Blocking: true,
	})
	return "", architecture.RevisionUnavailable, limitations, reason
}

// inferClaimsCitedFiles is the set of repository files the extracted facts
// actually cite. Dirtiness is judged over exactly these: an edit to a file no
// fact cites changes nothing the document asserts, while a build artifact left
// in the tree must not make every scan permanently unbindable.
func inferClaimsCitedFiles(facts []normalizedInvariantFact) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, f := range facts {
		if f.Provenance != nil && f.Provenance.SourceKind != "source_file" {
			continue
		}
		add(f.Evidence.SourceFile)
		for _, file := range f.Scope.Files {
			add(file)
		}
	}
	return out
}

func summarizeUncommittedPaths(paths []string) string {
	const shown = 3
	if len(paths) <= shown {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(paths[:shown], ", "), len(paths)-shown)
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

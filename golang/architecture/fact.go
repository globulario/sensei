// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const (
	RevisionResolved     = "resolved"
	RevisionUnavailable  = "unavailable"
	RevisionNotGit       = "not_git"
	RevisionNotRequested = "not_requested"

	SourceDigestResolved    = "resolved"
	SourceDigestUnavailable = "unavailable"

	RepositoryDomainResolved = "resolved"
	RepositoryDomainFallback = "fallback_local_label"
	RepositoryDomainUnknown  = "unavailable"
)

type Fact struct {
	ID         string            `json:"id" yaml:"id"`
	Kind       string            `json:"kind" yaml:"kind"`
	Subject    string            `json:"subject" yaml:"subject"`
	Predicate  string            `json:"predicate" yaml:"predicate"`
	Object     string            `json:"object" yaml:"object"`
	Scope      Scope             `json:"scope" yaml:"scope"`
	Evidence   Evidence          `json:"evidence" yaml:"evidence"`
	Confidence float64           `json:"confidence" yaml:"confidence"`
	Extractor  string            `json:"extractor" yaml:"extractor"`
	Meta       map[string]string `json:"meta,omitempty" yaml:"meta,omitempty"`

	// Provenance is intentionally not rendered by legacy extraction commands.
	// It is the reusable in-process carrier for closure-oriented consumers.
	Provenance *Provenance `json:"-" yaml:"-"`
}

type Scope struct {
	Repository string   `json:"repository" yaml:"repository"`
	Files      []string `json:"files" yaml:"files"`
	Symbols    []string `json:"symbols" yaml:"symbols"`
}

type Evidence struct {
	SourceFile                string `json:"source_file,omitempty" yaml:"source_file,omitempty"`
	LineStart                 int    `json:"line_start,omitempty" yaml:"line_start,omitempty"`
	LineEnd                   int    `json:"line_end,omitempty" yaml:"line_end,omitempty"`
	TestName                  string `json:"test_name,omitempty" yaml:"test_name,omitempty"`
	Commit                    string `json:"commit,omitempty" yaml:"commit,omitempty"`
	Command                   string `json:"command,omitempty" yaml:"command,omitempty"`
	EvidenceUnavailableStatus string `json:"-" yaml:"-"`
	EvidenceUnavailableReason string `json:"-" yaml:"-"`
}

type Provenance struct {
	RepositoryDomain       string `json:"repository_domain" yaml:"repository_domain"`
	RepositoryDomainStatus string `json:"repository_domain_status" yaml:"repository_domain_status"`
	Revision               string `json:"revision,omitempty" yaml:"revision,omitempty"`
	RevisionStatus         string `json:"revision_status" yaml:"revision_status"`
	SourceDigest           string `json:"source_digest,omitempty" yaml:"source_digest,omitempty"`
	SourceDigestStatus     string `json:"source_digest_status" yaml:"source_digest_status"`
	SourceKind             string `json:"source_kind" yaml:"source_kind"`
}

type Limitation struct {
	Source   string `json:"source" yaml:"source"`
	Scope    string `json:"scope" yaml:"scope"`
	Reason   string `json:"reason" yaml:"reason"`
	Blocking bool   `json:"blocking" yaml:"blocking"`
}

type Options struct {
	Root                   string
	RepositoryDomain       string
	RepositoryDomainStatus string
	Revision               string
	RevisionStatus         string
	SourceKind             string
}

func NewFact(in Fact, opts Options) (Fact, []Limitation) {
	f := canonicalizeFact(in)
	if f.ID == "" {
		f.ID = StableID(f.Kind, f.Subject, f.Predicate, f.Object, f.Evidence.SourceFile, f.Evidence.LineStart, f.Extractor)
	}
	prov := &Provenance{
		RepositoryDomain:       strings.TrimSpace(opts.RepositoryDomain),
		RepositoryDomainStatus: strings.TrimSpace(opts.RepositoryDomainStatus),
		Revision:               strings.TrimSpace(opts.Revision),
		RevisionStatus:         strings.TrimSpace(opts.RevisionStatus),
		SourceKind:             strings.TrimSpace(opts.SourceKind),
	}
	if prov.RepositoryDomainStatus == "" {
		if prov.RepositoryDomain != "" {
			prov.RepositoryDomainStatus = RepositoryDomainResolved
		} else {
			prov.RepositoryDomainStatus = RepositoryDomainUnknown
		}
	}
	if prov.RevisionStatus == "" {
		prov.RevisionStatus = RevisionNotRequested
	}
	if prov.SourceKind == "" && f.Evidence.SourceFile != "" {
		prov.SourceKind = "source_file"
	}
	var limitations []Limitation
	if f.Evidence.SourceFile != "" && strings.TrimSpace(opts.Root) != "" {
		digest, err := SourceDigestSHA256(opts.Root, f.Evidence.SourceFile)
		if err != nil {
			prov.SourceDigestStatus = SourceDigestUnavailable
			limitations = append(limitations, Limitation{
				Source:   f.Evidence.SourceFile,
				Scope:    "source_digest",
				Reason:   err.Error(),
				Blocking: false,
			})
		} else {
			prov.SourceDigest = digest
			prov.SourceDigestStatus = SourceDigestResolved
		}
	}
	if prov.SourceDigestStatus == "" {
		prov.SourceDigestStatus = SourceDigestUnavailable
	}
	f.Provenance = prov
	return f, limitations
}

func NormalizeFacts(root string, facts []Fact) ([]Fact, error) {
	out := make([]Fact, 0, len(facts))
	for _, in := range facts {
		f := canonicalizeFact(in)
		if f.ID == "" {
			f.ID = StableID(f.Kind, f.Subject, f.Predicate, f.Object, f.Evidence.SourceFile, f.Evidence.LineStart, f.Extractor)
		}
		if err := ValidateFact(f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	seen := map[string]Fact{}
	dedup := out[:0]
	for _, f := range out {
		if existing, ok := seen[f.ID]; ok {
			if !canonicalFactsEqual(existing, f) {
				return nil, fmt.Errorf("fact id collision for %s: %s != %s", f.ID, factCollisionSummary(existing), factCollisionSummary(f))
			}
			continue
		}
		seen[f.ID] = f
		dedup = append(dedup, f)
	}
	_ = root
	return dedup, nil
}

func ValidateFact(f Fact) error {
	var errs []string
	if strings.TrimSpace(f.Kind) == "" {
		errs = append(errs, "kind is required")
	}
	if strings.TrimSpace(f.Subject) == "" {
		errs = append(errs, "subject is required")
	}
	if strings.TrimSpace(f.Predicate) == "" {
		errs = append(errs, "predicate is required")
	}
	if strings.TrimSpace(f.Extractor) == "" {
		errs = append(errs, "extractor is required")
	}
	if f.Confidence < 0 || f.Confidence > 1 {
		errs = append(errs, "confidence must be between 0 and 1")
	}
	if f.Evidence.LineStart < 0 || f.Evidence.LineEnd < 0 {
		errs = append(errs, "line numbers must not be negative")
	}
	if f.Evidence.LineStart > 0 && f.Evidence.LineEnd > 0 && f.Evidence.LineEnd < f.Evidence.LineStart {
		errs = append(errs, "line_end must not precede line_start")
	}
	if f.Provenance != nil && f.Provenance.SourceKind == "source_file" && strings.TrimSpace(f.Evidence.SourceFile) == "" && strings.TrimSpace(f.Evidence.EvidenceUnavailableStatus) == "" {
		errs = append(errs, "source-backed fact requires source file or explicit evidence unavailable status")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func StableID(kind, subject, predicate, object, sourceFile string, lineStart int, extractor string) string {
	return "fact." + ShortHash(strings.Join([]string{
		strings.TrimSpace(kind),
		strings.TrimSpace(subject),
		strings.TrimSpace(predicate),
		strings.TrimSpace(object),
		filepath.ToSlash(strings.TrimSpace(sourceFile)),
		fmt.Sprint(lineStart),
		strings.TrimSpace(extractor),
	}, "|"))
}

func ShortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func SourceDigestSHA256(root, sourceFile string) (string, error) {
	path := sourceFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(sourceFile))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ResolveRevision(root string, requested bool) (string, string, []Limitation) {
	if !requested {
		return "", RevisionNotRequested, nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return "", RevisionNotGit, []Limitation{{
			Source:   root,
			Scope:    "git_history",
			Reason:   "repository is not a git checkout",
			Blocking: false,
		}}
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", RevisionUnavailable, []Limitation{{
			Source:   root,
			Scope:    "git_revision",
			Reason:   err.Error(),
			Blocking: false,
		}}
	}
	return strings.TrimSpace(string(out)), RevisionResolved, nil
}

func canonicalizeFact(in Fact) Fact {
	f := in
	f.ID = strings.TrimSpace(f.ID)
	f.Kind = strings.TrimSpace(f.Kind)
	f.Subject = strings.TrimSpace(f.Subject)
	f.Predicate = strings.TrimSpace(f.Predicate)
	f.Object = strings.TrimSpace(f.Object)
	f.Extractor = strings.TrimSpace(f.Extractor)
	f.Scope.Repository = strings.TrimSpace(f.Scope.Repository)
	f.Scope.Files = cleanStringList(f.Scope.Files, true)
	f.Scope.Symbols = cleanStringList(f.Scope.Symbols, false)
	f.Evidence.SourceFile = cleanPath(strings.TrimSpace(f.Evidence.SourceFile))
	f.Evidence.TestName = strings.TrimSpace(f.Evidence.TestName)
	f.Evidence.Commit = strings.TrimSpace(f.Evidence.Commit)
	f.Evidence.Command = strings.TrimSpace(f.Evidence.Command)
	f.Evidence.EvidenceUnavailableStatus = strings.TrimSpace(f.Evidence.EvidenceUnavailableStatus)
	f.Evidence.EvidenceUnavailableReason = strings.TrimSpace(f.Evidence.EvidenceUnavailableReason)
	if f.Meta != nil {
		m := make(map[string]string, len(f.Meta))
		for k, v := range f.Meta {
			k = strings.TrimSpace(k)
			if k != "" {
				m[k] = strings.TrimSpace(v)
			}
		}
		if len(m) == 0 {
			f.Meta = nil
		} else {
			f.Meta = m
		}
	}
	if f.Provenance != nil {
		p := *f.Provenance
		p.RepositoryDomain = strings.TrimSpace(p.RepositoryDomain)
		p.RepositoryDomainStatus = strings.TrimSpace(p.RepositoryDomainStatus)
		p.Revision = strings.TrimSpace(p.Revision)
		p.RevisionStatus = strings.TrimSpace(p.RevisionStatus)
		p.SourceDigest = strings.TrimSpace(p.SourceDigest)
		p.SourceDigestStatus = strings.TrimSpace(p.SourceDigestStatus)
		p.SourceKind = strings.TrimSpace(p.SourceKind)
		f.Provenance = &p
	}
	return f
}

func canonicalFactsEqual(a, b Fact) bool {
	return reflect.DeepEqual(canonicalComparableFact(a), canonicalComparableFact(b))
}

func canonicalComparableFact(f Fact) Fact {
	f = canonicalizeFact(f)
	return f
}

func factCollisionSummary(f Fact) string {
	return fmt.Sprintf("{kind:%q subject:%q predicate:%q object:%q file:%q line:%d extractor:%q symbols:%q meta:%v provenance:%+v}",
		f.Kind,
		f.Subject,
		f.Predicate,
		f.Object,
		f.Evidence.SourceFile,
		f.Evidence.LineStart,
		f.Extractor,
		strings.Join(f.Scope.Symbols, ","),
		f.Meta,
		f.Provenance,
	)
}

func cleanStringList(in []string, path bool) []string {
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if path {
			item = cleanPath(item)
		}
		if item != "" {
			seen[item] = true
		}
	}
	out := make([]string, 0, len(seen))
	for item := range seen {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func cleanPath(p string) string {
	return filepath.ToSlash(strings.ReplaceAll(p, `\`, `/`))
}

func MarshalLegacyFacts(facts []Fact) ([]byte, error) {
	return json.Marshal(facts)
}

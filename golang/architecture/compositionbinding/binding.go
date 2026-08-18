// SPDX-License-Identifier: AGPL-3.0-only

// Package compositionbinding resolves the exact input digests architecture
// composition requires, from one task generation, and names the owner that
// produced each one.
//
// It exists because those five digests previously arrived as five raw CLI
// strings — `--graph-digest`, `--claims-digest`, `--closure-digest`,
// `--questions-digest`, `--review-digest` — each defaulting to empty and
// validated only by a shape regex. Any 64 hex characters satisfied that,
// including the digest of nothing. The binding meant to make composition
// exactly reproducible rested on operator-typed strings whose SHAPE was
// checked and whose ORIGIN was not.
//
// Every digest here is computed from a governed document that already exists:
//
//	graph authority     source/graph.nt                       (task graph snapshot)
//	current claims      convergence/latest/maintained-claims  (architecture.ClaimDocument)
//	closure state       closure-after-dialogue                (closure.Report)
//	existing questions  dialogue.open_questions               (architecture.OpenQuestion)
//	review history      dialogue.architect_answers            (architecture.ArchitectAnswer)
//
// The last one is worth naming explicitly, because an earlier reading of this
// gap concluded review history had no owner at all. It does: an ArchitectAnswer
// records who answered, when, under what governance status, which questions it
// answered, and what superseded it. That is the review record this system keeps
// — not a new concept invented to fill a field.
//
// Resolution is atomic with respect to generation publication (one
// tasksession.ResolveGenerationDocuments call), and fails closed rather than
// emitting a well-formed value nobody can trace.
package compositionbinding

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

const SchemaVersion = "sensei.compositionbinding.v1"

// GenerationPrepareTime labels a binding resolved from a task that has not yet
// published a control generation — the state immediately after
// `sensei prepare-change` and before the first `sensei advance-task`.
//
// That is an ordinary state, not a missing one, and it is named rather than
// left as an empty string. An empty generation reads as "unknown", which would
// make a legitimately prepare-time binding indistinguishable from one whose
// generation could not be resolved — the same conflation of absence with
// failure this repository already carries a scar for.
const GenerationPrepareTime = "prepare-time"

// Dimension names, closed so a reader always has a rule for what it is given.
const (
	DimensionGraph     = "graph_authority"
	DimensionClaims    = "current_claims"
	DimensionClosure   = "closure_state"
	DimensionQuestions = "existing_questions"
	DimensionReview    = "review_history"
)

// Ordered lists the dimensions in a stable order, so the document is
// byte-identical across runs of the same generation.
var Ordered = []string{DimensionGraph, DimensionClaims, DimensionClosure, DimensionQuestions, DimensionReview}

// Resolution records one digest and, as importantly, WHERE it came from.
//
// Producer is not decoration. A digest whose origin is not recorded is
// indistinguishable from one somebody typed, which is the defect this package
// exists to remove; a later reader must be able to go back to the exact
// document without guessing which of several plausible files was meant.
type Resolution struct {
	Dimension string `json:"dimension" yaml:"dimension"`
	Producer  string `json:"producer" yaml:"producer"`
	Digest    string `json:"digest_sha256" yaml:"digest_sha256"`
	// Count is the number of records the digest covers, where the document is
	// a collection. A digest of an empty collection is a real digest, not a
	// placeholder — but it is a fixed constant, so without this a reader
	// cannot tell "no open questions" from "the wrong document".
	Count int `json:"count" yaml:"count"`
}

// Document is the owner-resolved input document. It is what a caller passes to
// composition instead of five loose strings.
type Document struct {
	SchemaVersion    string       `json:"schema_version" yaml:"schema_version"`
	TaskID           string       `json:"task_id" yaml:"task_id"`
	Generation       string       `json:"generation" yaml:"generation"`
	RepositoryDomain string       `json:"repository_domain" yaml:"repository_domain"`
	Revision         string       `json:"revision" yaml:"revision"`
	Resolutions      []Resolution `json:"resolutions" yaml:"resolutions"`
}

// Digest returns the resolved digest for a dimension.
func (d Document) Digest(dimension string) (string, bool) {
	for _, r := range d.Resolutions {
		if r.Dimension == dimension {
			return r.Digest, true
		}
	}
	return "", false
}

// Resolve derives the composition binding from the task's current generation.
//
// It refuses rather than degrades. Every failure mode the checkpoint names —
// missing, stale, mixed-generation, wrong-domain, internally inconsistent —
// ends in an error here, because a composition bound to inputs that cannot be
// vouched for produces a receipt that looks exactly like one that can.
func Resolve(repoRoot, taskDir string) (Document, error) {
	docs, err := tasksession.ResolveGenerationDocuments(repoRoot, taskDir)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: %w", err)
	}

	binding := docs.Session.Binding
	if strings.TrimSpace(binding.RepositoryDomain) == "" {
		return Document{}, fmt.Errorf("compositionbinding: the task session names no repository domain; composition cannot be scoped")
	}
	// Wrong-domain and internally-inconsistent are the same check from two
	// directions: every governed document in one generation must agree about
	// which repository it describes. A set that disagrees is not a generation,
	// whatever the pointer says.
	for _, other := range []struct {
		what    string
		binding architecture.ClaimDocumentBinding
	}{
		{"maintained claims", docs.Claims.Binding},
		{"dialogue", docs.Dialogue.Binding},
	} {
		if got := strings.TrimSpace(other.binding.RepositoryDomain); got != "" && got != binding.RepositoryDomain {
			return Document{}, fmt.Errorf("compositionbinding: %s is bound to repository %q but the task session is bound to %q; these documents are not one generation",
				other.what, got, binding.RepositoryDomain)
		}
		if got := strings.TrimSpace(other.binding.Revision); got != "" && strings.TrimSpace(binding.Revision) != "" && got != binding.Revision {
			return Document{}, fmt.Errorf("compositionbinding: %s is bound to revision %s but the task session is bound to %s; these documents are not one generation",
				other.what, short(got), short(binding.Revision))
		}
	}

	graphDigest, err := fileDigest(docs.GraphNTPath)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: graph authority: %w", err)
	}
	claimsDigest, err := closureprotocol.SemanticDigest(docs.Claims)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: current claims: %w", err)
	}
	closureDigest, err := closureprotocol.SemanticDigest(docs.Closure)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: closure state: %w", err)
	}
	questionsDigest, err := closureprotocol.SemanticDigest(docs.Dialogue.OpenQuestions)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: existing questions: %w", err)
	}
	reviewDigest, err := closureprotocol.SemanticDigest(docs.Dialogue.Answers)
	if err != nil {
		return Document{}, fmt.Errorf("compositionbinding: review history: %w", err)
	}

	generation := docs.Generation
	if strings.TrimSpace(generation) == "" {
		generation = GenerationPrepareTime
	}
	doc := Document{
		SchemaVersion:    SchemaVersion,
		TaskID:           docs.Session.TaskID,
		Generation:       generation,
		RepositoryDomain: binding.RepositoryDomain,
		Revision:         binding.Revision,
		Resolutions: []Resolution{
			{DimensionGraph, "task graph snapshot (source/graph.nt)", graphDigest, 1},
			{DimensionClaims, "maintained claims (convergence/latest/maintained-claims.yaml)", claimsDigest, len(docs.Claims.Claims)},
			{DimensionClosure, "closure report (convergence/latest/closure-after-dialogue.yaml)", closureDigest, len(docs.Closure.RelevantNodes)},
			{DimensionQuestions, "dialogue open questions (convergence/latest/dialogue.yaml)", questionsDigest, len(docs.Dialogue.OpenQuestions)},
			{DimensionReview, "dialogue architect answers (convergence/latest/dialogue.yaml)", reviewDigest, len(docs.Dialogue.Answers)},
		},
	}
	if err := Validate(doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// Validate checks the document is complete and internally consistent. It is
// exported so a caller reading a persisted binding re-checks it rather than
// trusting the file.
func Validate(d Document) error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("compositionbinding: schema_version %q is not %q", d.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(d.TaskID) == "" || strings.TrimSpace(d.RepositoryDomain) == "" {
		return fmt.Errorf("compositionbinding: task_id and repository_domain are required")
	}
	if strings.TrimSpace(d.Generation) == "" {
		return fmt.Errorf("compositionbinding: generation is required; use %q when the task has published none, so absence is stated rather than blank", GenerationPrepareTime)
	}
	seen := map[string]bool{}
	for _, r := range d.Resolutions {
		if !isDimension(r.Dimension) {
			return fmt.Errorf("compositionbinding: unknown dimension %q", r.Dimension)
		}
		if seen[r.Dimension] {
			return fmt.Errorf("compositionbinding: dimension %q resolved twice", r.Dimension)
		}
		seen[r.Dimension] = true
		if !isSHA256(r.Digest) {
			return fmt.Errorf("compositionbinding: %s digest %q is not a sha256", r.Dimension, r.Digest)
		}
		if strings.TrimSpace(r.Producer) == "" {
			return fmt.Errorf("compositionbinding: %s names no producer; a digest whose origin is unrecorded is indistinguishable from one that was typed", r.Dimension)
		}
	}
	var missing []string
	for _, dim := range Ordered {
		if !seen[dim] {
			missing = append(missing, dim)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("compositionbinding: unresolved dimension(s) %v; composition may not run on a partial binding", missing)
	}
	return nil
}

func isDimension(name string) bool {
	for _, d := range Ordered {
		if d == name {
			return true
		}
	}
	return false
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// sha256Of is the digest of exact bytes.
func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Of(data), nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

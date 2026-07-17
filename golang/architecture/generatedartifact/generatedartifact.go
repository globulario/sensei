// SPDX-License-Identifier: Apache-2.0

// Package generatedartifact regenerates a repository's governed generated
// artifacts in memory from the exact result architecture and compares them
// byte-for-byte against the materialized result tree. It proves that a generated
// file could only have been produced by this exact result architecture — not
// merely that a generated-looking file exists. It writes no files, runs no shell
// or CLI, and reads no CWD, time, environment, network, active pointer, or live
// graph state.
package generatedartifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	ProducerFamily  = "sensei.generatedartifact"
	RegistryVersion = "v1"

	// SelfProfileID is the generated-artifact profile for the Sensei repository.
	SelfProfileID = "sensei.generated-artifacts.self/v1"

	ntriplesMediaType = "application/n-triples"
	yamlMediaType     = "application/yaml"
	tsvMediaType      = "text/tab-separated-values"
)

// Verification statuses.
const (
	StatusVerified         = "verified"
	StatusMissing          = "missing"
	StatusStaleBytes       = "stale_bytes"
	StatusStaleSemantics   = "stale_semantics"
	StatusGenerationFailed = "generation_failed"
	StatusNotApplicable    = "not_applicable"
)

// Context is the pure input every producer receives.
type Context struct {
	RepositoryRoot   string
	RepositoryDomain string

	GraphInputPolicyID             string
	GraphInputSnapshotDigestSHA256 string
	SourceManifestDigestSHA256     string
	SupplementalGraphs             []graphbuild.SupplementalGraphBinding

	GraphArtifact graphbuild.Artifact
}

// Output is one producer's expected artifact, generated in memory.
type Output struct {
	ProducerID      string
	ProducerVersion string

	Path      string
	MediaType string
	Bytes     []byte

	SemanticDigestSHA256 string
	ByteDigestSHA256     string
}

// Producer deterministically regenerates one repository artifact.
type Producer interface {
	ID() string
	Version() string
	OutputPath() string
	Dependencies() []string
	Generate(ctx context.Context, in Context, prior map[string]Output) (Output, error)
}

// Profile is a closed set of required producer ids for a repository domain.
type Profile struct {
	ID          string
	ProducerIDs []string
}

// producerRegistry is the closed registry of every known producer.
var producerRegistry = func() map[string]Producer {
	m := map[string]Producer{}
	for _, p := range []Producer{proofObligationsProducer{}, embeddedGraphProducer{}, resultManifestProducer{}} {
		if _, dup := m[p.ID()]; dup {
			panic("generatedartifact: duplicate producer id " + p.ID())
		}
		m[p.ID()] = p
	}
	return m
}()

// profileRegistry maps a repository domain to its closed generated-artifact
// profile. An unregistered domain is refused under the closure-strict pipeline.
var profileRegistry = map[string]Profile{
	"github.com/globulario/sensei": {
		ID: SelfProfileID,
		ProducerIDs: []string{
			proofObligationsProducerID,
			embeddedGraphProducerID,
			resultManifestProducerID,
		},
	},
}

// ProfileForDomain returns the closed profile for a repository domain. It never
// infers applicability from file presence and never returns an empty profile for
// an unregistered domain.
func ProfileForDomain(domain string) (Profile, error) {
	p, ok := profileRegistry[strings.TrimSpace(domain)]
	if !ok {
		return Profile{}, fmt.Errorf("resultpipeline.generated_artifact_profile_unavailable: %s", domain)
	}
	return p, nil
}

// VerificationEntry is one artifact's regenerate-and-compare outcome.
type VerificationEntry struct {
	Path            string `json:"path" yaml:"path"`
	ProducerID      string `json:"producer_id" yaml:"producer_id"`
	ProducerVersion string `json:"producer_version" yaml:"producer_version"`
	Required        bool   `json:"required" yaml:"required"`
	Status          string `json:"status" yaml:"status"`

	ExpectedSemanticDigestSHA256 string `json:"expected_semantic_digest_sha256" yaml:"expected_semantic_digest_sha256"`
	ExpectedByteDigestSHA256     string `json:"expected_byte_digest_sha256" yaml:"expected_byte_digest_sha256"`
	ObservedSemanticDigestSHA256 string `json:"observed_semantic_digest_sha256,omitempty" yaml:"observed_semantic_digest_sha256,omitempty"`
	ObservedByteDigestSHA256     string `json:"observed_byte_digest_sha256,omitempty" yaml:"observed_byte_digest_sha256,omitempty"`
}

// VerificationManifest is the stage-2 canonical output.
type VerificationManifest struct {
	SchemaVersion string              `json:"schema_version" yaml:"schema_version"`
	PolicyID      string              `json:"policy_id" yaml:"policy_id"`
	ProfileID     string              `json:"profile_id" yaml:"profile_id"`
	ProducerIDs   []string            `json:"producer_ids" yaml:"producer_ids"`
	RequiredPaths []string            `json:"required_paths" yaml:"required_paths"`
	Entries       []VerificationEntry `json:"entries" yaml:"entries"`

	AllRequiredMatched bool     `json:"all_required_matched" yaml:"all_required_matched"`
	Limitations        []string `json:"limitations" yaml:"limitations"`
}

// VerificationResult carries the manifest and the verified result artifacts.
type VerificationResult struct {
	Manifest          VerificationManifest
	VerifiedArtifacts []closureprotocol.ResultArtifact
}

// Error carries the verification manifest so a failure stays inspectable.
type Error struct {
	Code     string
	Manifest VerificationManifest
}

func (e *Error) Error() string { return e.Code }

func sha256hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// observedSemanticDigest computes the observed semantic digest by media type:
// an N-Triples graph's identity is its seed-marker digest; every other artifact's
// semantic identity is its byte digest.
func observedSemanticDigest(mediaType string, data []byte, byteDigest string) string {
	if mediaType == ntriplesMediaType {
		if marker, ok := seedmeta.ParseMarker(data); ok {
			return marker.Digest
		}
		return ""
	}
	return byteDigest
}

// Generate runs every producer in the profile in deterministic topological order
// and returns their outputs, WITHOUT comparing against or writing any file. It is
// the shared production path used by the rebuild CLI and by tests to materialize
// the exact expected artifacts.
func Generate(ctx context.Context, in Context, profile Profile) (map[string]Output, error) {
	ordered, err := topoOrder(profile.ProducerIDs)
	if err != nil {
		return nil, err
	}
	outputs := map[string]Output{}
	for _, id := range ordered {
		p := producerRegistry[id]
		out, gerr := p.Generate(ctx, in, outputs)
		if gerr != nil {
			return nil, fmt.Errorf("generatedartifact: producer %q: %w", id, gerr)
		}
		if out.Path != p.OutputPath() {
			return nil, fmt.Errorf("generatedartifact: producer %q returned undeclared output %q", id, out.Path)
		}
		outputs[id] = out
	}
	return outputs, nil
}

// RegenerateAndVerify regenerates every producer in the profile in deterministic
// topological order and compares each against the exact materialized result tree.
// It writes nothing. It fails closed on any missing or stale required artifact.
func RegenerateAndVerify(ctx context.Context, in Context, profile Profile) (VerificationResult, error) {
	ordered, err := topoOrder(profile.ProducerIDs)
	if err != nil {
		return VerificationResult{}, err
	}
	manifest := VerificationManifest{
		SchemaVersion: "resultpipeline.generated-artifacts/v1",
		PolicyID:      in.GraphInputPolicyID,
		ProfileID:     profile.ID,
		ProducerIDs:   append([]string(nil), ordered...),
	}
	outputs := map[string]Output{}
	allMatched := true
	for _, id := range ordered {
		p := producerRegistry[id]
		entry := VerificationEntry{Path: p.OutputPath(), ProducerID: p.ID(), ProducerVersion: p.Version(), Required: true}
		manifest.RequiredPaths = append(manifest.RequiredPaths, p.OutputPath())

		out, gerr := p.Generate(ctx, in, outputs)
		if gerr != nil {
			entry.Status = StatusGenerationFailed
			manifest.Entries = append(manifest.Entries, entry)
			return VerificationResult{}, &Error{Code: "resultpipeline.generated_artifact_generation_failed: " + p.ID() + ": " + gerr.Error(), Manifest: finalize(manifest, false)}
		}
		if out.Path != p.OutputPath() {
			return VerificationResult{}, &Error{Code: "resultpipeline.generated_artifact_generation_failed: undeclared output " + out.Path, Manifest: finalize(manifest, false)}
		}
		outputs[id] = out
		entry.ExpectedSemanticDigestSHA256 = out.SemanticDigestSHA256
		entry.ExpectedByteDigestSHA256 = out.ByteDigestSHA256

		observed, rerr := os.ReadFile(filepath.Join(in.RepositoryRoot, filepath.FromSlash(out.Path)))
		if rerr != nil {
			entry.Status = StatusMissing
			manifest.Entries = append(manifest.Entries, entry)
			return VerificationResult{}, &Error{Code: "resultpipeline.generated_artifact_missing: " + out.Path, Manifest: finalize(manifest, false)}
		}
		obsByte := sha256hex(observed)
		obsSem := observedSemanticDigest(out.MediaType, observed, obsByte)
		entry.ObservedByteDigestSHA256 = obsByte
		entry.ObservedSemanticDigestSHA256 = obsSem
		switch {
		case obsByte == out.ByteDigestSHA256:
			entry.Status = StatusVerified
		case obsSem == out.SemanticDigestSHA256 && out.SemanticDigestSHA256 != "":
			entry.Status = StatusStaleBytes
		default:
			entry.Status = StatusStaleSemantics
		}
		manifest.Entries = append(manifest.Entries, entry)
		if entry.Status != StatusVerified {
			allMatched = false
			code := "resultpipeline.generated_artifact_stale"
			if entry.Status == StatusStaleSemantics {
				code = "resultpipeline.generated_artifact_semantic_mismatch"
			}
			return VerificationResult{}, &Error{Code: code + ": " + out.Path, Manifest: finalize(manifest, false)}
		}
	}

	final := finalize(manifest, allMatched)
	var verified []closureprotocol.ResultArtifact
	for _, e := range final.Entries {
		if e.Status == StatusVerified {
			verified = append(verified, closureprotocol.ResultArtifact{Path: e.Path, DigestSHA256: e.ExpectedByteDigestSHA256})
		}
	}
	sort.Slice(verified, func(i, j int) bool { return verified[i].Path < verified[j].Path })
	return VerificationResult{Manifest: final, VerifiedArtifacts: verified}, nil
}

func finalize(m VerificationManifest, allMatched bool) VerificationManifest {
	sort.Strings(m.RequiredPaths)
	m.RequiredPaths = dedupeSortedStrings(m.RequiredPaths)
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	m.AllRequiredMatched = allMatched
	if m.Limitations == nil {
		m.Limitations = []string{}
	}
	return m
}

func dedupeSortedStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

// topoOrder returns the producers in dependency-respecting order, refusing
// unknown producers, duplicate ids, duplicate output paths, and dependency
// cycles.
func topoOrder(ids []string) ([]string, error) {
	seen := map[string]bool{}
	paths := map[string]string{}
	for _, id := range ids {
		p, ok := producerRegistry[id]
		if !ok {
			return nil, fmt.Errorf("generatedartifact: unknown producer %q", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("generatedartifact: duplicate producer id %q", id)
		}
		seen[id] = true
		if other, dup := paths[p.OutputPath()]; dup {
			return nil, fmt.Errorf("generatedartifact: producers %q and %q share output path %q", other, id, p.OutputPath())
		}
		paths[p.OutputPath()] = id
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var order []string
	var visit func(id string) error
	visit = func(id string) error {
		p, ok := producerRegistry[id]
		if !ok {
			return fmt.Errorf("generatedartifact: unknown dependency %q", id)
		}
		switch color[id] {
		case gray:
			return fmt.Errorf("generatedartifact: dependency cycle at %q", id)
		case black:
			return nil
		}
		color[id] = gray
		deps := append([]string(nil), p.Dependencies()...)
		sort.Strings(deps)
		for _, d := range deps {
			if !seen[d] {
				return fmt.Errorf("generatedartifact: producer %q depends on %q which is not in the profile", id, d)
			}
			if err := visit(d); err != nil {
				return err
			}
		}
		color[id] = black
		order = append(order, id)
		return nil
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for _, id := range sorted {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

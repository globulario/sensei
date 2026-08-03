// SPDX-License-Identifier: AGPL-3.0-only

// Package fileinterpretation implements a providerport.Provider that
// supplies O1 interpretation from an already-authored file, not a model
// call.
//
// O8 (golang/architecture/cognitivecommand) deliberately does not advertise
// or execute providerport.OperationInterpretation: a governed source
// resolver capable of turning a Session's graph/closure digests into
// resolved, digest-bound source content and references does not exist yet
// (see docs/design/cognitive-command-providers-o8.md's "Interpretation
// boundary"). Until that resolver exists, this package is the only honest
// way to drive golang/architecture/synthesisdriver.Run's first phase: a
// human (or another already-governed process) authors an Interpretation
// document by hand, and this Provider does nothing but read, stamp, digest,
// and validate it -- it never invents interpretation content, and it never
// claims to be the resolver O8's design doc says is still missing.
package fileinterpretation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// ProviderKind is this provider's honest self-description. It must never be
// mistaken for a governed, graph-grounded interpretation resolver.
const ProviderKind = "file-supplied"

// DefaultMaxFileBytes bounds how large an interpretation file New() will
// read, absent an explicit Config.MaxFileBytes override.
const DefaultMaxFileBytes = 1 << 20 // 1 MiB

// AuthoredInterpretation is the subset of synthesis.Interpretation an
// author supplies in the --interpretation file. SchemaVersion,
// InterpretationID, SessionDigestSHA256, GeneratedBy, and
// InterpretationDigestSHA256 are always stamped by Provider from the live
// request/session, never trusted from the file, so a stale or foreign file
// cannot misrepresent which session it belongs to.
type AuthoredInterpretation struct {
	Objective                string                      `json:"objective"`
	ApplicableIntent         []string                    `json:"applicable_intent"`
	BindingInvariants        []string                    `json:"binding_invariants"`
	RelevantContracts        []string                    `json:"relevant_contracts"`
	AuthorityBoundaries      []string                    `json:"authority_boundaries"`
	KnownFailureModes        []string                    `json:"known_failure_modes"`
	ForbiddenFixes           []string                    `json:"forbidden_fixes"`
	RequiredProofObligations []string                    `json:"required_proof_obligations"`
	Assumptions              []string                    `json:"assumptions"`
	UnresolvedQuestions      []string                    `json:"unresolved_questions"`
	SourceReferences         []synthesis.SourceReference `json:"source_references"`
	Limitations              []synthesis.Limitation      `json:"limitations"`
}

// Config configures a Provider. ObservedAt is validated as RFC3339 at
// construction, matching agentcommand.Config and cognitivecommand.Config's
// own convention.
type Config struct {
	// Path is the interpretation file. It is read exactly once, at New(),
	// never again -- see Provider's doc comment for why.
	Path string
	// ProviderID identifies this provider instance in its self-description.
	ProviderID string
	// ObservedAt is the RFC3339 timestamp stamped into Describe's
	// ProviderObservation.
	ObservedAt string
	// MaxFileBytes bounds how large Path may be. Zero means
	// DefaultMaxFileBytes.
	MaxFileBytes int64
}

// Provider implements providerport.Provider by reading, stamping,
// digesting, and validating an author-supplied interpretation file.
//
// The file is read exactly once, at construction (New), not on every
// Execute call: a Provider instance's whole point is to answer "what did
// the operator actually author for this run," and re-reading on each call
// would let the same Provider identity silently answer with different
// bytes if the file changed on disk between calls (O1 only ever calls
// interpretation once per session in practice, but Execute has no way to
// know that from the interface alone, and a Provider should not depend on
// a caller's calling discipline for a correctness property it can
// guarantee itself). The consumed bytes' sha256 is computed once at read
// time and reported as evidence on every Execute call via Observer, so the
// exact content a run acted on is independently verifiable after the fact,
// not just asserted.
type Provider struct {
	config        Config
	caps          providerport.Capabilities
	authored      AuthoredInterpretation
	contentSHA256 string
	resolvedPath  string
}

var _ providerport.Provider = (*Provider)(nil)

// New validates config, reads and parses Path exactly once, and
// precomputes the (digested) capability self-description Describe returns
// on every call.
//
// Path is resolved with symlinks rejected explicitly (via Lstat, before
// any open) rather than silently followed or silently resolved -- an
// interpretation file is operator-supplied CLI input, not adversarial, but
// "deliberately regular file only" is a one-line guarantee worth stating
// plainly rather than leaving as an accident of os.ReadFile's default
// follow-symlinks behavior. A directory, device, or other special file is
// rejected the same way. The file must not exceed MaxFileBytes.
func New(config Config) (*Provider, error) {
	config.Path = strings.TrimSpace(config.Path)
	config.ProviderID = strings.TrimSpace(config.ProviderID)
	config.ObservedAt = strings.TrimSpace(config.ObservedAt)
	if config.Path == "" {
		return nil, errors.New("fileinterpretation: path is required")
	}
	if config.ProviderID == "" {
		return nil, errors.New("fileinterpretation: provider id is required")
	}
	if _, err := time.Parse(time.RFC3339, config.ObservedAt); err != nil {
		return nil, fmt.Errorf("fileinterpretation: observed_at must be RFC3339: %w", err)
	}
	if config.MaxFileBytes <= 0 {
		config.MaxFileBytes = DefaultMaxFileBytes
	}

	resolvedPath, err := filepath.Abs(config.Path)
	if err != nil {
		return nil, fmt.Errorf("fileinterpretation: resolve path %q: %w", config.Path, err)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	authored, contentSHA256, err := readAuthoredInterpretation(resolvedPath, config.MaxFileBytes)
	if err != nil {
		return nil, err
	}

	caps := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   config.ProviderID,
			ProviderKind: ProviderKind,
			ObservedAt:   config.ObservedAt,
		},
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
	}
	digest, err := providerport.CapabilitiesDigest(caps)
	if err != nil {
		return nil, err
	}
	caps.CapabilitiesDigestSHA256 = digest

	return &Provider{
		config:        config,
		caps:          caps,
		authored:      authored,
		contentSHA256: contentSHA256,
		resolvedPath:  resolvedPath,
	}, nil
}

// readAuthoredInterpretation performs the one-time, deliberate read: Lstat
// first (so a symlink or special file is rejected before any open ever
// follows it), then a size-bounded, no-follow open, then parse. A hard
// error here is a CLI-level configuration problem (bad path, wrong file
// type, too large, malformed JSON) -- exactly like a missing task session
// or non-absolute --agent-command -- not a governed provider disposition,
// so New() returning an error rather than Execute() returning
// OutcomeInvalidOutput is deliberate: fail fast, before session/identity
// resolution has even started, consistent with every other CLI
// precondition check.
func readAuthoredInterpretation(resolvedPath string, maxBytes int64) (AuthoredInterpretation, string, error) {
	info, err := os.Lstat(resolvedPath)
	if err != nil {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: stat %q: %w", resolvedPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: %q is a symlink, not a regular file -- pass the real path", resolvedPath)
	}
	if !info.Mode().IsRegular() {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: %q is not a regular file (mode %s)", resolvedPath, info.Mode())
	}
	if info.Size() > maxBytes {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: %q is %d bytes, exceeds the %d byte limit", resolvedPath, info.Size(), maxBytes)
	}

	f, err := os.OpenFile(resolvedPath, os.O_RDONLY, 0)
	if err != nil {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: open %q: %w", resolvedPath, err)
	}
	defer f.Close()

	hasher := sha256.New()
	raw, err := io.ReadAll(io.TeeReader(io.LimitReader(f, maxBytes+1), hasher))
	if err != nil {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: read %q: %w", resolvedPath, err)
	}
	if int64(len(raw)) > maxBytes {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: %q exceeds the %d byte limit while reading", resolvedPath, maxBytes)
	}

	var authored AuthoredInterpretation
	if err := json.Unmarshal(raw, &authored); err != nil {
		return AuthoredInterpretation{}, "", fmt.Errorf("fileinterpretation: parse %q: %w", resolvedPath, err)
	}

	// Recompute the hash over exactly the bytes that were unmarshaled
	// (hasher only saw what TeeReader passed through io.ReadAll, i.e. raw
	// itself), not a separate re-read -- content and hash can never
	// diverge.
	contentSHA256 := hex.EncodeToString(hasher.Sum(nil))
	return authored, contentSHA256, nil
}

// Describe returns this provider's untrusted self-description. It never
// claims OperationPlanning, OperationGeneration, or
// OperationEvaluationObservation -- this provider supplies interpretation
// only.
func (p *Provider) Describe(context.Context) (providerport.Capabilities, error) {
	return p.caps, nil
}

// Execute stamps and validates the interpretation cached at construction.
// It never re-reads Path -- see Provider's doc comment. It reports the
// cached content's sha256 as evidence via obs (when non-nil) before
// returning, so the exact bytes this run acted on are independently
// verifiable from the resulting ObservationBatch/Receipt, not just
// asserted.
func (p *Provider) Execute(_ context.Context, request providerport.Request, obs providerport.Observer) (providerport.Result, error) {
	if request.Operation != providerport.OperationInterpretation {
		return unsupportedCapability(request)
	}

	if obs != nil {
		_ = obs.Observe(fmt.Sprintf("interpretation file %s content_sha256=%s", p.resolvedPath, p.contentSHA256))
	}

	interpretation := synthesis.NormalizeInterpretation(synthesis.Interpretation{
		SchemaVersion:            synthesis.InterpretationSchemaVersion,
		InterpretationID:         "interpretation." + request.RequestID,
		SessionDigestSHA256:      request.SessionDigestSHA256,
		GeneratedBy:              synthesis.GeneratedBy,
		Objective:                p.authored.Objective,
		ApplicableIntent:         p.authored.ApplicableIntent,
		BindingInvariants:        p.authored.BindingInvariants,
		RelevantContracts:        p.authored.RelevantContracts,
		AuthorityBoundaries:      p.authored.AuthorityBoundaries,
		KnownFailureModes:        p.authored.KnownFailureModes,
		ForbiddenFixes:           p.authored.ForbiddenFixes,
		RequiredProofObligations: p.authored.RequiredProofObligations,
		Assumptions:              p.authored.Assumptions,
		UnresolvedQuestions:      p.authored.UnresolvedQuestions,
		SourceReferences:         p.authored.SourceReferences,
		Limitations:              p.authored.Limitations,
	})
	digest, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		return invalidOutput(request, fmt.Sprintf("digest interpretation: %v", err))
	}
	interpretation.InterpretationDigestSHA256 = digest

	data, err := json.Marshal(interpretation)
	if err != nil {
		return invalidOutput(request, fmt.Sprintf("marshal interpretation for schema validation: %v", err))
	}
	if err := synthesis.ValidateInterpretationSchema(data); err != nil {
		return invalidOutput(request, fmt.Sprintf("interpretation file %q failed schema validation: %v", p.resolvedPath, err))
	}

	return finishCompleted(request, digest, &interpretation)
}

func finishCompleted(request providerport.Request, payloadDigest string, interpretation *synthesis.Interpretation) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:         providerport.ResultSchemaVersion,
		RequestDigestSHA256:   request.RequestDigestSHA256,
		Operation:             request.Operation,
		TerminalOutcome:       providerport.OutcomeCompleted,
		PayloadDigestSHA256:   &payloadDigest,
		InterpretationPayload: interpretation,
	})
	return finishResult(result)
}

func invalidOutput(request providerport.Request, detail string) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     providerport.OutcomeInvalidOutput,
		Detail:              detail,
	})
	return finishResult(result)
}

func unsupportedCapability(request providerport.Request) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     providerport.OutcomeUnsupportedCapability,
		Detail:              fmt.Sprintf("fileinterpretation.Provider only supports %q, got %q", providerport.OperationInterpretation, request.Operation),
	})
	return finishResult(result)
}

func finishResult(result providerport.Result) (providerport.Result, error) {
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}

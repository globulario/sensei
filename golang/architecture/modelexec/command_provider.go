// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// CommandProvider runs a model through an explicitly named executable that
// speaks one JSON request on stdin and one JSON response on stdout.
//
// It is transport and nothing else. It does not own Sensei status semantics and
// cannot declare resolved: Execute still constructs the terminal binding from
// what it observed, exactly as the fake-provider proof established.
//
// A command bridge rather than a vendor SDK, deliberately. It keeps Sensei free
// of provider SDKs and their auth models, fronts any vendor or a local model
// through a small script, stays hermetically testable with a fixture
// executable, and — most importantly — keeps provider selection EXPLICIT. A
// provider is used because someone named it, never because a credential
// happened to be present in the environment.
type CommandProvider struct {
	// ProviderID and ProviderVersion are semantic assertions supplied by the
	// caller. They are never inferred from the executable's filename: a path
	// is not a trustworthy statement about which behaviour answers.
	ProviderID      string
	ProviderVersion string

	// Path is the executable, resolved without a shell. Argv are its
	// arguments, passed through verbatim.
	//
	// There is no shell anywhere in this adapter. A shell would make provider
	// arguments a place where repository text could be expanded, and the whole
	// point of the bounded request is that nothing reaches the model except
	// what the request carries.
	Path string
	Argv []string
}

func (c *CommandProvider) Identity() ProviderIdentity {
	return ProviderIdentity{ID: c.ProviderID, Version: c.ProviderVersion}
}

// commandRequestEnvelope is the adapter's PRIVATE wire type.
//
// It exists so the wire can be shaped for a bridge without touching
// investigation.ModelBinding or any other persisted type. #257 proved that
// serialized compatibility on those types is load-bearing: a cosmetic field
// changed every deterministic document digest, and omitempty on a nested
// struct did not omit anything. A private envelope keeps that risk out of the
// persisted schema entirely.
type commandRequestEnvelope struct {
	Schema string `json:"schema"`

	RepositoryDomain   string `json:"repository_domain"`
	RepositoryRevision string `json:"repository_revision,omitempty"`
	RepositoryTree     string `json:"repository_tree,omitempty"`
	GraphDigestSHA256  string `json:"graph_digest_sha256,omitempty"`
	GraphStatus        string `json:"graph_status,omitempty"`
	PolicyProfileID    string `json:"policy_profile_id,omitempty"`

	HowDocumentDigestSHA256      string `json:"how_document_digest_sha256,omitempty"`
	EvidenceSnapshotDigestSHA256 string `json:"evidence_snapshot_digest_sha256,omitempty"`

	TargetObservationIDs []string `json:"target_observation_ids,omitempty"`
	TargetEvidenceIDs    []string `json:"target_evidence_ids,omitempty"`
	QueryDigestSHA256    string   `json:"query_digest_sha256,omitempty"`

	SuppliedEvidence []commandEvidence `json:"supplied_evidence"`

	ProviderID           string `json:"provider_id"`
	ProviderVersion      string `json:"provider_version"`
	ModelName            string `json:"model_name"`
	ModelDigestSHA256    string `json:"model_digest_sha256,omitempty"`
	ModelDigestAbsent    bool   `json:"model_digest_absent,omitempty"`
	PromptContractDigest string `json:"prompt_contract_digest,omitempty"`
	OutputSchemaVersion  string `json:"output_schema_version,omitempty"`
	ToolPolicy           string `json:"tool_policy,omitempty"`

	// RequestDigestSHA256 is the identity the executor already computed. The
	// bridge receives it so a model harness can record what it answered, but
	// the adapter never recomputes or re-derives it.
	RequestDigestSHA256 string `json:"request_digest_sha256"`
}

type commandEvidence struct {
	ID           string `json:"id"`
	DigestSHA256 string `json:"digest_sha256"`
	FilePath     string `json:"file_path,omitempty"`
	Excerpt      string `json:"excerpt"`
}

// commandResponseEnvelope is the closed shape a bridge must answer in. Exactly
// one of Artifact or Refusal is expected.
type commandResponseEnvelope struct {
	Schema   string           `json:"schema"`
	Artifact *commandArtifact `json:"artifact,omitempty"`
	// Refusal is a STRUCTURED field rather than an exit code or a message that
	// has to be pattern-matched. A refusal is an answer and an error is a
	// failure; deciding which one happened by reading prose is exactly the
	// fragility the typed vocabulary exists to remove.
	Refusal *commandRefusal `json:"refusal,omitempty"`
}

type commandRefusal struct {
	Reason string `json:"reason"`
}

type commandArtifact struct {
	SchemaVersion             string                `json:"schema_version"`
	NondeterminismDeclaration string                `json:"nondeterminism_declaration"`
	Items                     []commandArtifactItem `json:"items"`
}

type commandArtifactItem struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text"`
	CitedEvidenceIDs []string `json:"cited_evidence_ids,omitempty"`
	RepositoryDomain string   `json:"repository_domain,omitempty"`
	FilePaths        []string `json:"file_paths,omitempty"`
	ClaimsCanonical  bool     `json:"claims_canonical,omitempty"`
	ClaimsPromotion  bool     `json:"claims_promotion,omitempty"`
	ClaimsAdmission  bool     `json:"claims_admission,omitempty"`
}

// CommandRequestSchema and CommandResponseSchema are the closed wire contract.
const (
	CommandRequestSchema  = "sensei.modelexec.command_request.v1"
	CommandResponseSchema = "sensei.modelexec.command_response.v1"
)

// Execute sends exactly one bounded request and reads exactly one response.
//
// The envelope is built ONLY from the Request. The adapter prepends no system
// prompt, no repository text, no file contents, and no clock reading. Any such
// material would be shown to the model while being absent from the request
// identity — reintroducing, one layer later, precisely the bug the #257 review
// found in the excerpt digest.
func (c *CommandProvider) Execute(ctx context.Context, req Request) (Artifact, error) {
	if strings.TrimSpace(c.ProviderID) == "" || strings.TrimSpace(c.ProviderVersion) == "" {
		return Artifact{}, fmt.Errorf("command provider requires an explicit provider id and version")
	}
	if strings.TrimSpace(c.Path) == "" {
		return Artifact{}, fmt.Errorf("command provider requires an explicit executable path")
	}

	digest, err := RequestDigest(req)
	if err != nil {
		return Artifact{}, err
	}
	envelope := commandRequestEnvelope{
		Schema:                       CommandRequestSchema,
		RepositoryDomain:             req.RepositoryDomain,
		RepositoryRevision:           req.RepositoryRevision,
		RepositoryTree:               req.RepositoryTree,
		GraphDigestSHA256:            req.GraphDigestSHA256,
		GraphStatus:                  req.GraphStatus,
		PolicyProfileID:              req.PolicyProfileID,
		HowDocumentDigestSHA256:      req.HowDocumentDigestSHA256,
		EvidenceSnapshotDigestSHA256: req.EvidenceSnapshotDigestSHA256,
		TargetObservationIDs:         req.TargetObservationIDs,
		TargetEvidenceIDs:            req.TargetEvidenceIDs,
		QueryDigestSHA256:            req.QueryDigestSHA256,
		ProviderID:                   req.Provider.ID,
		ProviderVersion:              req.Provider.Version,
		ModelName:                    req.Model.Name,
		ModelDigestSHA256:            req.Model.DigestSHA256,
		ModelDigestAbsent:            req.Model.DigestAbsent,
		PromptContractDigest:         req.PromptContractDigest,
		OutputSchemaVersion:          req.OutputSchemaVersion,
		ToolPolicy:                   req.ToolPolicy,
		RequestDigestSHA256:          digest,
	}
	for _, e := range req.SuppliedEvidence {
		envelope.SuppliedEvidence = append(envelope.SuppliedEvidence, commandEvidence{
			ID: e.ID, DigestSHA256: e.DigestSHA256, FilePath: e.FilePath, Excerpt: e.Excerpt,
		})
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Artifact{}, err
	}

	// exec.CommandContext takes the executable and argv directly. No shell is
	// involved, so nothing in Argv is expanded, globbed, or substituted.
	cmd := exec.CommandContext(ctx, c.Path, c.Argv...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Cancellation is a transport failure, not a refusal: nobody declined.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Artifact{}, fmt.Errorf("model command cancelled: %w", ctxErr)
		}
		// stderr is diagnostic only. It is quoted for the operator and never
		// reaches artifact identity.
		return Artifact{}, fmt.Errorf("model command failed: %w (stderr: %s)", err, truncate(stderr.String()))
	}

	var response commandResponseEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return Artifact{}, fmt.Errorf("model command returned unparsable output: %w", err)
	}
	if response.Schema != CommandResponseSchema {
		return Artifact{}, fmt.Errorf("model command answered in an unknown schema %q", response.Schema)
	}
	if response.Refusal != nil && response.Artifact != nil {
		return Artifact{}, errors.New("model command returned both a refusal and an artifact")
	}
	if response.Refusal != nil {
		return Artifact{}, &Refusal{Reason: response.Refusal.Reason}
	}
	if response.Artifact == nil {
		return Artifact{}, errors.New("model command returned neither a refusal nor an artifact")
	}

	out := Artifact{
		SchemaVersion:             response.Artifact.SchemaVersion,
		NondeterminismDeclaration: response.Artifact.NondeterminismDeclaration,
	}
	for _, item := range response.Artifact.Items {
		out.Items = append(out.Items, ArtifactItem{
			Kind:             item.Kind,
			Text:             item.Text,
			CitedEvidenceIDs: item.CitedEvidenceIDs,
			RepositoryDomain: item.RepositoryDomain,
			FilePaths:        item.FilePaths,
			ClaimsCanonical:  item.ClaimsCanonical,
			ClaimsPromotion:  item.ClaimsPromotion,
			ClaimsAdmission:  item.ClaimsAdmission,
		})
	}
	// The artifact is returned UNVALIDATED. Validation and digesting belong to
	// Execute, so a transport cannot become an authority by accepting its own
	// payload.
	return out, nil
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + "…"
	}
	return s
}

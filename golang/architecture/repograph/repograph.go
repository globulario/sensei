// SPDX-License-Identifier: AGPL-3.0-only

package repograph

import "fmt"

// Canonical repository-scoped projection paths (repo-relative).
const (
	GraphRelPath  = ".sensei/project/graph.nt"
	MarkerRelPath = ".sensei/graph-authority.json"
	// CombinedSeedRelPath is the combined cross-repository deployment seed. This
	// owner MUST NOT write it; it remains a separate convergence obligation.
	CombinedSeedRelPath = "golang/server/embeddata/awareness.nt"

	ProducerID      = "sensei.repograph/v1"
	ProducerVersion = "v1"
)

// Disposition distinguishes a freshly built projection from an exact replay of an
// already-current one.
type Disposition string

const (
	DispositionBuilt    Disposition = "built"
	DispositionReplayed Disposition = "replayed"
)

// BuildRequest freezes the inputs for a repository-graph projection. The expected
// manifest is a compare-and-swap token: the owner independently recomputes the
// current governed-source manifest immediately before compile and requires
// equality, and again before verification.
type BuildRequest struct {
	RepositoryRoot               string
	RepositoryDomain             string
	ExpectedManifestDigestSHA256 string
}

// VerifiedProjection is the typed result of a verified repository-graph
// projection. Every digest is recomputed by the owner from disk; none is
// caller-supplied.
type VerifiedProjection struct {
	RepositoryRoot string
	Disposition    Disposition

	GraphPath  string // repo-relative
	MarkerPath string // repo-relative

	// Governed-source identities (input).
	SourceManifestDigestSHA256  string // whole-set CAS token (governedmutation)
	GraphBuildInputDigestSHA256 string // graphbuild source-manifest digest of exactly what compiled

	// Persisted graph identities (output), recomputed from disk on reload.
	CompiledGraphByteDigestSHA256 string // sha256 of the persisted stamped file
	GraphSemanticDigestSHA256     string // marker-free semantic digest (== marker digest)
	MarkerDigestSHA256            string // from the reloaded marker file
	MarkerIRI                     string
	MarkerSchemaVersion           string
	GraphTripleCount              int

	ProducerID      string
	ProducerVersion string

	Verified bool

	// CombinedSeedObligation records that the cross-repository combined embedded
	// seed remains a separate, unfulfilled convergence obligation.
	CombinedSeedObligation string
}

// ── typed errors (distinguishable for later 8.1b-4 recovery) ────────────────

// StaleManifestError: the governed-source manifest changed out from under the
// projection. Phase is "before_compile" or "after_compile".
type StaleManifestError struct {
	Phase    string
	Expected string
	Actual   string
}

func (e *StaleManifestError) Error() string {
	return fmt.Sprintf("governed-source manifest stale (%s): expected %s, actual %s",
		e.Phase, short(e.Expected), short(e.Actual))
}

// CompileError: compile failed before any persistence. No graph or marker was
// written.
type CompileError struct{ Detail string }

func (e *CompileError) Error() string { return "graph compile failed before persistence: " + e.Detail }

// PersistError: a persistence step failed. Target is "graph" or "marker".
// GraphDurable reports whether the graph file was already durable when the marker
// write failed (graph persisted but marker persistence failed → incomplete, never
// verified).
type PersistError struct {
	Target       string
	GraphDurable bool
	Detail       string
}

func (e *PersistError) Error() string {
	return fmt.Sprintf("persist %s failed (graph_durable=%v): %s", e.Target, e.GraphDurable, e.Detail)
}

// ReloadVerifyError: the persisted pair exists but independent reload verification
// failed. Aspect names what failed (graph_byte, nt_parse, semantic_digest,
// marker_file, marker_graph_correspondence, freshness).
type ReloadVerifyError struct {
	Aspect string
	Detail string
}

func (e *ReloadVerifyError) Error() string {
	return fmt.Sprintf("persisted graph failed reload verification (%s): %s", e.Aspect, e.Detail)
}

// InvalidRequestError: the request was malformed.
type InvalidRequestError struct{ Detail string }

func (e *InvalidRequestError) Error() string { return "invalid repograph request: " + e.Detail }

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

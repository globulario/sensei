// SPDX-License-Identifier: AGPL-3.0-only

// Package runtimeboundary is the SOLE transport-neutral owner of Phase 9.7 runtime-boundary
// assessment. Given a canonical runtime-assessable boundary identity, an explicit boundary policy,
// typed runtime observations, and a validated non-self-authorizing runtime→architecture binding, it
// produces one deterministic assessment: satisfied | violated | degraded | unknown | not_applicable
// | unavailable | invalid.
//
// It imports no golang/server, generated protobuf, editor, CLI, store, or mutation package, and
// emits no RDF. The server, protobuf adapters, VS Code, and CLI are consumers below it (CP2/CP3) and
// never reclassify a runtime-boundary assessment.
//
// The load-bearing law is enforced structurally, not by prose:
//   - Missing telemetry cannot establish compliance — an absent/unobserved evidence source yields
//     unavailable/unknown, never satisfied.
//   - Observed traffic cannot create architecture — an observation that does not validly bind, in
//     scope, to an already-declared boundary is refused; it never mints, authorizes, or redefines a
//     boundary.
//
// Determinism: assessment is a pure function of typed inputs. No ambient cwd, environment, clock,
// network, or random input affects the verdict or digest. A missing source is never synthesized as
// zero/authorized; unknown remains unknown.
package runtimeboundary

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ProducerName / ProducerVersion identify every runtimeboundary projection producer.
const (
	ProducerName    = "sensei.runtimeboundary"
	ProducerVersion = "v1"

	// SchemaAssessment is the canonical schema id of a runtime-boundary assessment.
	SchemaAssessment = "runtime.boundary_assessment/v1"
)

// Availability is the CLOSED projection availability vocabulary. Zero value fails closed.
type Availability string

const (
	// AvailabilityAvailable: all required sources for the assessment were observed + validated.
	AvailabilityAvailable Availability = "available"
	// AvailabilityPartial: usable, but a required/relevant secondary source is unavailable/degraded.
	AvailabilityPartial Availability = "partial"
	// AvailabilityUnavailable: the primary source needed to construct the assessment cannot be observed.
	AvailabilityUnavailable Availability = "unavailable"
	// AvailabilityInvalid: identity/policy/observation/binding is contradictory or malformed.
	AvailabilityInvalid Availability = "invalid"
)

func validAvailability(a Availability) bool {
	switch a {
	case AvailabilityAvailable, AvailabilityPartial, AvailabilityUnavailable, AvailabilityInvalid:
		return true
	}
	return false
}

// SourceAvailability is the CLOSED per-source status vocabulary. Zero value fails closed.
type SourceAvailability string

const (
	SourceAvailable   SourceAvailability = "available"
	SourceDegraded    SourceAvailability = "degraded"
	SourceUnavailable SourceAvailability = "unavailable"
	SourceInvalid     SourceAvailability = "invalid"
)

func validSourceAvailability(a SourceAvailability) bool {
	switch a {
	case SourceAvailable, SourceDegraded, SourceUnavailable, SourceInvalid:
		return true
	}
	return false
}

// SourceImpact is the CLOSED role a source plays in an assessment's availability.
type SourceImpact string

const (
	// ImpactPrimary: the source without which the assessment cannot be constructed.
	ImpactPrimary SourceImpact = "primary"
	// ImpactRequired: needed for a complete assessment; its loss degrades to partial.
	ImpactRequired SourceImpact = "required"
	// ImpactRelevant: contributes; its loss degrades to partial.
	ImpactRelevant SourceImpact = "relevant"
	// ImpactOptional: truly optional; its absence does not degrade.
	ImpactOptional SourceImpact = "optional"
)

func validSourceImpact(i SourceImpact) bool {
	switch i {
	case ImpactPrimary, ImpactRequired, ImpactRelevant, ImpactOptional:
		return true
	}
	return false
}

// SourceStatus is one input source's observation status with its assessment impact. A missing
// source is unavailable, never a silent zero.
type SourceStatus struct {
	Owner        string             `json:"owner" yaml:"owner"`
	Schema       string             `json:"schema" yaml:"schema"`
	Availability SourceAvailability `json:"availability" yaml:"availability"`
	Impact       SourceImpact       `json:"impact" yaml:"impact"`
	ReasonCode   string             `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	Identity     string             `json:"identity,omitempty" yaml:"identity,omitempty"`
	Digest       string             `json:"digest,omitempty" yaml:"digest,omitempty"`
}

// ProjectionMeta is embedded in every runtimeboundary projection: producer identity, availability,
// per-source statuses, the non-authoritative marker, limitations, and a self-excluding digest.
type ProjectionMeta struct {
	SchemaVersion              string         `json:"schema_version" yaml:"schema_version"`
	ProducerName               string         `json:"producer_name" yaml:"producer_name"`
	ProducerVersion            string         `json:"producer_version" yaml:"producer_version"`
	RepositoryIdentity         string         `json:"repository_identity,omitempty" yaml:"repository_identity,omitempty"`
	RequestedDomain            string         `json:"requested_domain,omitempty" yaml:"requested_domain,omitempty"`
	Availability               Availability   `json:"availability" yaml:"availability"`
	Sources                    []SourceStatus `json:"sources,omitempty" yaml:"sources,omitempty"`
	NonAuthoritativeProjection bool           `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
	Limitations                []string       `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	DigestSHA256               string         `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// newMeta stamps the fixed producer identity + non-authoritative marker for a schema.
func newMeta(schema, repo, domain string, avail Availability, sources []SourceStatus, limits []string) ProjectionMeta {
	sortSources(sources)
	return ProjectionMeta{
		SchemaVersion: schema, ProducerName: ProducerName, ProducerVersion: ProducerVersion,
		RepositoryIdentity: repo, RequestedDomain: domain, Availability: avail,
		Sources: sources, NonAuthoritativeProjection: true, Limitations: limits,
	}
}

// aggregateAvailability derives assessment availability from source impacts: primary
// unavailable/invalid → unavailable; any required/relevant degraded/unavailable/invalid → partial;
// optional loss does not degrade; else available.
func aggregateAvailability(sources []SourceStatus) Availability {
	primaryBad, anyReqRelBad := false, false
	for _, s := range sources {
		fatal := s.Availability == SourceUnavailable || s.Availability == SourceInvalid
		switch s.Impact {
		case ImpactPrimary:
			if fatal {
				primaryBad = true
			} else if s.Availability == SourceDegraded {
				anyReqRelBad = true
			}
		case ImpactRequired, ImpactRelevant:
			if fatal || s.Availability == SourceDegraded {
				anyReqRelBad = true
			}
		}
	}
	switch {
	case primaryBad:
		return AvailabilityUnavailable
	case anyReqRelBad:
		return AvailabilityPartial
	default:
		return AvailabilityAvailable
	}
}

// validateMeta enforces canonical producer identity, valid availability + source vocab,
// availability↔source coherence, and the non-authoritative marker.
func validateMeta(m ProjectionMeta, schema string) error {
	if m.SchemaVersion != schema {
		return fmt.Errorf("assessment schema %q is not %q", m.SchemaVersion, schema)
	}
	if m.ProducerName != ProducerName || m.ProducerVersion != ProducerVersion {
		return fmt.Errorf("assessment producer identity is not canonical")
	}
	if !validAvailability(m.Availability) {
		return fmt.Errorf("assessment availability %q is off-vocabulary", m.Availability)
	}
	if !m.NonAuthoritativeProjection {
		return fmt.Errorf("assessment must be marked non-authoritative")
	}
	seen := map[string]bool{}
	primaryCount := 0
	for _, s := range m.Sources {
		if err := validateSourceStatus(s); err != nil {
			return err
		}
		if s.Impact == ImpactPrimary {
			primaryCount++
		}
		key := s.Owner + "\x00" + s.Identity + "\x00" + s.Schema
		if seen[key] {
			return fmt.Errorf("duplicate source status %q", s.Owner)
		}
		seen[key] = true
	}
	if m.Availability != AvailabilityInvalid && len(m.Sources) > 0 && primaryCount != 1 {
		return fmt.Errorf("assessment must declare exactly one primary source, got %d", primaryCount)
	}
	if !availabilityConsistent(m.Availability, m.Sources) {
		return fmt.Errorf("assessment availability %q is inconsistent with its source impacts", m.Availability)
	}
	return nil
}

// availabilityConsistent is the necessary coherence relation between aggregate availability and
// source impacts. Only a bad PRIMARY source justifies unavailable. Invalid is a fail-closed carrier.
func availabilityConsistent(avail Availability, sources []SourceStatus) bool {
	if avail == AvailabilityInvalid {
		return true
	}
	primaryBad, anyReqRelBad := false, false
	for _, s := range sources {
		fatal := s.Availability == SourceUnavailable || s.Availability == SourceInvalid
		switch s.Impact {
		case ImpactPrimary:
			if fatal {
				primaryBad = true
			} else if s.Availability == SourceDegraded {
				anyReqRelBad = true
			}
		case ImpactRequired, ImpactRelevant:
			if fatal || s.Availability == SourceDegraded {
				anyReqRelBad = true
			}
		}
	}
	switch avail {
	case AvailabilityAvailable:
		return !primaryBad && !anyReqRelBad
	case AvailabilityPartial:
		return anyReqRelBad && !primaryBad
	case AvailabilityUnavailable:
		return primaryBad
	}
	return false
}

// validateSourceStatus is the CENTRAL source-status validator.
func validateSourceStatus(s SourceStatus) error {
	if s.Owner == "" || s.Owner != strings.TrimSpace(s.Owner) {
		return fmt.Errorf("source status owner is empty or padded")
	}
	if s.Schema == "" || s.Schema != strings.TrimSpace(s.Schema) {
		return fmt.Errorf("source %q schema is empty or padded", s.Owner)
	}
	if !validSourceAvailability(s.Availability) {
		return fmt.Errorf("source %q availability %q is off-vocabulary", s.Owner, s.Availability)
	}
	if !validSourceImpact(s.Impact) {
		return fmt.Errorf("source %q impact %q is off-vocabulary", s.Owner, s.Impact)
	}
	if s.Identity != "" && (s.Identity != strings.TrimSpace(s.Identity) || isAbsolutePath(s.Identity)) {
		return fmt.Errorf("source %q identity is padded or an absolute path", s.Owner)
	}
	switch s.Availability {
	case SourceAvailable:
		if s.Identity == "" {
			return fmt.Errorf("available source %q missing identity", s.Owner)
		}
		if s.ReasonCode != "" {
			return fmt.Errorf("available source %q must carry no failure reason", s.Owner)
		}
	case SourceDegraded, SourceInvalid:
		if s.Identity == "" {
			return fmt.Errorf("observed source %q missing identity", s.Owner)
		}
		if s.ReasonCode == "" {
			return fmt.Errorf("%s source %q must carry a typed reason", s.Availability, s.Owner)
		}
	case SourceUnavailable:
		if s.ReasonCode == "" {
			return fmt.Errorf("unavailable source %q must carry a typed reason", s.Owner)
		}
	}
	return nil
}

// srcStatus normalizes a source: an available source carries no failure reason; a
// degraded/unavailable/invalid source always carries a typed reason (defaulted if empty).
func srcStatus(owner, schema, identity, digest string, avail SourceAvailability, impact SourceImpact, reason string) SourceStatus {
	if avail == SourceAvailable {
		reason = ""
	} else if reason == "" {
		reason = string(avail)
	}
	return SourceStatus{Owner: owner, Schema: schema, Identity: identity, Digest: digest, Availability: avail, Impact: impact, ReasonCode: reason}
}

// sortSources canonically orders sources before digesting (impact rank, then owner/schema/identity).
func sortSources(s []SourceStatus) {
	rank := func(i SourceImpact) int {
		switch i {
		case ImpactPrimary:
			return 0
		case ImpactRequired:
			return 1
		case ImpactRelevant:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(s, func(i, j int) bool {
		if rank(s[i].Impact) != rank(s[j].Impact) {
			return rank(s[i].Impact) < rank(s[j].Impact)
		}
		if s[i].Owner != s[j].Owner {
			return s[i].Owner < s[j].Owner
		}
		if s[i].Schema != s[j].Schema {
			return s[i].Schema < s[j].Schema
		}
		return s[i].Identity < s[j].Identity
	})
}

// digestOf computes the deterministic self-excluding SHA-256 of a value whose digest field has
// already been cleared. Platform-stable (canonical key-sorted JSON via the closure protocol).
func digestOf(v any) (string, error) {
	return closureprotocol.SemanticDigest(v)
}

// ---- shared string helpers (self-contained; no controlstate import) --------

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isAbsolutePath rejects filesystem-shaped identities (identities are logical, never paths).
func isAbsolutePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") ||
		(len(s) >= 2 && s[1] == ':') // windows drive
}

func trimmedNonEmpty(s string) bool {
	return s != "" && s == strings.TrimSpace(s)
}

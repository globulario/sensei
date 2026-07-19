// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Phase 9.4b, Checkpoint 1: the completion-policy configuration surface.
//
// A domain opts into completion enforcement through an EXPLICIT `completion:`
// section of <repo>/.sensei/gate-policy.yaml. This surface is deliberately kept
// INDEPENDENT of the EditCheck `Rules` map (gate_policy.go): it is a distinct type
// with its own strict loader, it is parsed separately, and it can NEVER be reached
// through an EditCheck rule, an inherited rule, a wildcard rule, or a `rules:` entry
// — the completion gate is a separate governance system, not an EditCheck level.
//
// This checkpoint provides ONLY the schema, loader, validation, and its typed
// result. It changes no advisory or enforce behavior; nothing here yet influences a
// pass/block/advisory/publication outcome. The enforce decision is Checkpoint 2.
//
// Absence of a completion policy means completion enforcement is not enabled for a
// domain — never a default to enforce, and never a silent collapse of "invalid"
// into "absent".

// completionPolicyMode is a domain's declared completion enforcement mode.
type completionPolicyMode string

const (
	completionModeAdvisory completionPolicyMode = "advisory"
	completionModeEnforce  completionPolicyMode = "enforce"
)

var validCompletionModes = map[completionPolicyMode]bool{
	completionModeAdvisory: true,
	completionModeEnforce:  true,
}

// completionPolicyState is the typed, four-valued result of resolving the completion
// policy for one domain. It is an explicit enum, never a pair of loose booleans: the
// ZERO value is completionPolicyInvalid, so an uninitialized or failed resolution can
// never be silently read as "absent" (which would disable enforcement). "invalid" and
// "absent" are DISTINCT values, and a valid-but-not-required policy is DISTINCT from
// both — a load error is always distinguishable from a domain that simply does not
// require completion.
type completionPolicyState int

const (
	// completionPolicyInvalid is the zero value on purpose: a policy that failed to
	// load, parse, or validate. It is ALWAYS returned with a non-nil error, so it can
	// never be confused with a valid "absent" result (which carries a nil error).
	completionPolicyInvalid completionPolicyState = iota
	// completionPolicyAbsent: no policy file, no completion section, or this domain
	// did not opt in. Completion enforcement is not enabled for the domain.
	completionPolicyAbsent
	// completionPolicyPresentNotRequired: the domain opted into the completion policy
	// but does not require completion.
	completionPolicyPresentNotRequired
	// completionPolicyPresentRequired: the domain opted in and requires completion.
	completionPolicyPresentRequired
)

func (s completionPolicyState) String() string {
	switch s {
	case completionPolicyInvalid:
		return "invalid"
	case completionPolicyAbsent:
		return "absent"
	case completionPolicyPresentNotRequired:
		return "present_not_required"
	case completionPolicyPresentRequired:
		return "present_required"
	default:
		return fmt.Sprintf("completionPolicyState(%d)", int(s))
	}
}

// completionDomainConfig is one domain's on-disk completion configuration. Mode is
// optional (inherits the section default); RequireCompletion is a pointer so an
// explicit `require_completion: false` is distinguishable from an omitted field.
type completionDomainConfig struct {
	Mode              string `yaml:"mode"`
	RequireCompletion *bool  `yaml:"require_completion"`
}

// completionSectionFile is the raw on-disk shape of the `completion:` section.
type completionSectionFile struct {
	Default string                            `yaml:"default"`
	Domains map[string]completionDomainConfig `yaml:"domains"`
}

// completionDomainPolicy is the resolved, validated per-domain policy — exposed as a
// typed internal API for the next checkpoint (the enforce decision). It carries the
// effective mode AND whether completion is required, so a later checkpoint never has
// to re-parse or re-derive them.
type completionDomainPolicy struct {
	Mode              completionPolicyMode
	RequireCompletion bool
}

// completionGatePolicy is the parsed, validated `completion:` section. present is
// false when the section was absent (no file or no `completion:` key). It is a
// SEPARATE type from gatePolicy and is never merged into its Rules map.
type completionGatePolicy struct {
	present     bool
	defaultMode completionPolicyMode
	domains     map[string]completionDomainPolicy
	loadedFrom  string
}

// resolveCompletionPolicy loads the completion policy and resolves the typed state
// for one canonical domain. It is the single entry point a caller uses.
//
//   - any malformed/unreadable/contradictory policy → (completionPolicyInvalid, err)
//     — fail loudly, never silently advisory or enforce;
//   - no policy file (default path), no completion section, or a domain that did not
//     opt in → (completionPolicyAbsent, nil);
//   - an opted-in domain → PresentNotRequired or PresentRequired.
//
// Resolution is deterministic and idempotent: the same inputs always yield the same
// result. A missing DEFAULT policy path is not an error (absent); a missing EXPLICIT
// path IS an error, matching resolveGatePolicy.
func resolveCompletionPolicy(explicitPath, repoRoot, domain string) (completionPolicyState, error) {
	p, err := loadCompletionGatePolicy(explicitPath, repoRoot)
	if err != nil {
		return completionPolicyInvalid, err
	}
	return p.stateFor(domain), nil
}

// loadCompletionGatePolicy reads and validates the `completion:` section of the gate
// policy file. It returns a policy with present=false (and a nil error) when there is
// no policy to read, and an error when a policy exists but is invalid.
func loadCompletionGatePolicy(explicitPath, repoRoot string) (completionGatePolicy, error) {
	path := explicitPath
	if path == "" {
		path = defaultGatePolicyPath(repoRoot)
		if _, err := os.Stat(path); err != nil {
			return completionGatePolicy{present: false}, nil // no policy configured
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return completionGatePolicy{}, fmt.Errorf("read completion policy %s: %w", path, err)
	}
	return parseCompletionGatePolicy(data, path)
}

// parseCompletionGatePolicy parses the completion section from a gate-policy document.
// It reads ONLY the `completion:` key (the EditCheck `default:`/`rules:` keys are left
// to gate_policy.go — this loader neither requires nor validates them), then decodes
// that subtree STRICTLY so unknown fields, wrong types, and duplicate domains fail
// loudly. Splitting the parse out of file I/O keeps it unit-testable on raw bytes.
func parseCompletionGatePolicy(data []byte, path string) (completionGatePolicy, error) {
	// Extract the completion subtree without imposing strictness on the rest of the
	// file (the EditCheck keys are not this loader's concern).
	var top struct {
		Completion yaml.Node `yaml:"completion"`
	}
	if err := yaml.Unmarshal(data, &top); err != nil {
		return completionGatePolicy{}, fmt.Errorf("completion policy %s: %w", path, err)
	}
	if top.Completion.IsZero() {
		return completionGatePolicy{present: false, loadedFrom: path}, nil // no completion section
	}

	// Strictly decode the completion subtree: re-encode the node, then decode with
	// KnownFields so an unknown key anywhere in the section is rejected. Decoding the
	// `domains` map also rejects duplicate domain keys.
	raw, err := yaml.Marshal(&top.Completion)
	if err != nil {
		return completionGatePolicy{}, fmt.Errorf("completion policy %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var sec completionSectionFile
	if err := dec.Decode(&sec); err != nil {
		return completionGatePolicy{}, fmt.Errorf("completion policy %s: %w", path, err)
	}

	return validateCompletionSection(sec, path)
}

// validateCompletionSection normalizes and validates the parsed section into the typed
// policy, failing loudly on any invalid or contradictory declaration.
func validateCompletionSection(sec completionSectionFile, path string) (completionGatePolicy, error) {
	out := completionGatePolicy{present: true, loadedFrom: path, domains: map[string]completionDomainPolicy{}}

	// Section default mode (empty → advisory; nothing enforces without explicit
	// per-domain opt-in regardless).
	def := completionPolicyMode(strings.ToLower(strings.TrimSpace(sec.Default)))
	if def == "" {
		def = completionModeAdvisory
	}
	if !validCompletionModes[def] {
		return completionGatePolicy{}, fmt.Errorf("completion policy %s: default %q is not one of advisory|enforce", path, sec.Default)
	}
	out.defaultMode = def

	// Deterministic iteration for stable error messages.
	names := make([]string, 0, len(sec.Domains))
	for name := range sec.Domains {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cfg := sec.Domains[name]
		key := strings.TrimSpace(name)
		if key == "" {
			return completionGatePolicy{}, fmt.Errorf("completion policy %s: a domain key is empty", path)
		}
		mode := def
		if m := strings.ToLower(strings.TrimSpace(cfg.Mode)); m != "" {
			mode = completionPolicyMode(m)
			if !validCompletionModes[mode] {
				return completionGatePolicy{}, fmt.Errorf("completion policy %s: domain %q mode %q is not one of advisory|enforce", path, key, cfg.Mode)
			}
		}
		require := cfg.RequireCompletion != nil && *cfg.RequireCompletion
		// A contradictory declaration fails loudly: requiring completion is only
		// meaningful under enforce, so require_completion:true with advisory mode is
		// rejected rather than silently ignored.
		if require && mode != completionModeEnforce {
			return completionGatePolicy{}, fmt.Errorf("completion policy %s: domain %q sets require_completion: true but mode is %q (require_completion is only valid under enforce)", path, key, mode)
		}
		out.domains[key] = completionDomainPolicy{Mode: mode, RequireCompletion: require}
	}
	return out, nil
}

// stateFor resolves the typed four-valued state for one canonical domain. Domain
// identity is matched by exact equality (after trimming) — the same exact-identity
// semantics the store's scope filter uses (InScope: nodeDomain == scope). It invents
// no second domain namespace, no case folding, and no fallback lookup (basename,
// prefix stripping, "the latest domain", etc.). A domain not opted in is Absent, never
// Invalid — only load/parse/validation failures are Invalid.
func (p completionGatePolicy) stateFor(domain string) completionPolicyState {
	if !p.present {
		return completionPolicyAbsent
	}
	dp, ok := p.domains[strings.TrimSpace(domain)]
	if !ok {
		return completionPolicyAbsent
	}
	if dp.RequireCompletion {
		return completionPolicyPresentRequired
	}
	return completionPolicyPresentNotRequired
}

// domainPolicy returns the resolved per-domain policy (effective mode + require) for a
// domain that opted in. The bool is false when the domain is absent. Exposed as the
// typed internal API the enforce-decision checkpoint will consume; it does not itself
// enforce anything.
func (p completionGatePolicy) domainPolicy(domain string) (completionDomainPolicy, bool) {
	if !p.present {
		return completionDomainPolicy{}, false
	}
	dp, ok := p.domains[strings.TrimSpace(domain)]
	return dp, ok
}

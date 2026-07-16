// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FreshnessMode classifies how a profile's freshness prose is enforced.
type FreshnessMode string

const (
	// FreshnessDuration binds validity to a fixed window after the observation.
	FreshnessDuration FreshnessMode = "duration"
	// FreshnessPerResult binds validity to the result tree; the receipt stays
	// fresh for as long as it observes the authoritative result binding.
	FreshnessPerResult FreshnessMode = "per_result"
	// FreshnessSelfDeclared means the producer declares its own validity. The
	// validator refuses to trust it without a bounded, observed expiry.
	FreshnessSelfDeclared FreshnessMode = "self_declared"
)

// FreshnessWindow is the canonical, enforceable form of a profile's freshness
// prose.
type FreshnessWindow struct {
	Mode     FreshnessMode `json:"mode" yaml:"mode"`
	Duration time.Duration `json:"duration,omitempty" yaml:"duration,omitempty"`
	Raw      string        `json:"raw" yaml:"raw"`
}

var durationSuffixRE = regexp.MustCompile(`^(\d+)(d|day|days|w|week|weeks)$`)

// ParseFreshness converts freshness prose into a canonical, enforceable window.
// It accepts sentinel forms ("per-result", "self-declared") and quantified
// durations ("24h", "30m", "7 days", "2 weeks"). Empty or unparseable prose is
// an error so an active profile cannot silently degrade to "no expiry".
func ParseFreshness(raw string) (FreshnessWindow, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return FreshnessWindow{}, fmt.Errorf("freshness window is empty")
	}
	switch s {
	case "per-result", "per_result", "per result", "result-bound", "result_bound":
		return FreshnessWindow{Mode: FreshnessPerResult, Raw: raw}, nil
	case "self-declared", "self_declared", "self declared", "declared":
		return FreshnessWindow{Mode: FreshnessSelfDeclared, Raw: raw}, nil
	}
	d, err := parseDurationProse(s)
	if err != nil {
		return FreshnessWindow{}, fmt.Errorf("unparseable freshness window %q", raw)
	}
	if d <= 0 {
		return FreshnessWindow{}, fmt.Errorf("freshness window must be positive: %q", raw)
	}
	return FreshnessWindow{Mode: FreshnessDuration, Duration: d, Raw: raw}, nil
}

func parseDurationProse(s string) (time.Duration, error) {
	compact := strings.ReplaceAll(s, " ", "")
	if d, err := time.ParseDuration(compact); err == nil {
		return d, nil
	}
	m := durationSuffixRE.FindStringSubmatch(compact)
	if m == nil {
		return 0, fmt.Errorf("bad duration %q", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, err
	}
	switch m[2] {
	case "d", "day", "days":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w", "week", "weeks":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("bad duration %q", s)
}

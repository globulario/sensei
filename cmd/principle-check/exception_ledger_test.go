// SPDX-License-Identifier: Apache-2.0

package main

// exception_ledger_test.go — the gate for meta.exception_must_have_reason_owner_and_expiry.
//
// It validates ONLY the structured exception ledger
// (docs/awareness-control/exceptions.yaml). It never scans arbitrary code, so it
// cannot false-positive on ordinary source comments. Contract: every governed
// exception must declare reason, owner, created, expires, scope, and
// removal_condition; a provisional entry must also carry review_after earlier than
// expires; an expired entry or a malformed date FAILS. An exception that outlives
// its review rings the bell instead of silencing it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type exceptionScope struct {
	Repo    string   `yaml:"repo"`
	Paths   []string `yaml:"paths"`
	Command string   `yaml:"command"`
}

type exceptionEntry struct {
	ID               string         `yaml:"id"`
	Kind             string         `yaml:"kind"`
	Scope            exceptionScope `yaml:"scope"`
	Reason           string         `yaml:"reason"`
	Owner            string         `yaml:"owner"`
	Created          string         `yaml:"created"`
	Expires          string         `yaml:"expires"`
	ReviewAfter      string         `yaml:"review_after"`
	Provisional      bool           `yaml:"provisional"`
	RemovalCondition string         `yaml:"removal_condition"`
	Evidence         []string       `yaml:"evidence"`
}

type exceptionLedger struct {
	Exceptions []exceptionEntry `yaml:"exceptions"`
}

var allowedExceptionKinds = map[string]bool{
	"baseline":         true,
	"suppression":      true,
	"allowlist":        true,
	"xfail":            true,
	"temporary_bypass": true,
	"tolerated_drift":  true,
}

func exceptionLedgerPath() string {
	return filepath.Join(agRepoRoot(), "docs", "awareness-control", "exceptions.yaml")
}

func parseDate(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

// validateException returns the list of contract violations for one entry,
// evaluated as of `now`. Empty slice == valid.
func validateException(e exceptionEntry, now time.Time) []string {
	var v []string
	req := func(name, val string) {
		if strings.TrimSpace(val) == "" {
			v = append(v, "missing "+name)
		}
	}
	req("id", e.ID)
	req("kind", e.Kind)
	req("reason", e.Reason)
	req("owner", e.Owner)
	req("removal_condition", e.RemovalCondition)
	if strings.TrimSpace(e.Scope.Repo) == "" {
		v = append(v, "missing scope.repo")
	}
	if e.Kind != "" && !allowedExceptionKinds[e.Kind] {
		v = append(v, "invalid kind "+e.Kind)
	}

	if e.Created == "" {
		v = append(v, "missing created")
	} else if _, err := parseDate(e.Created); err != nil {
		v = append(v, "invalid created date "+e.Created)
	}

	expires, expErr := parseDate(e.Expires)
	switch {
	case e.Expires == "":
		v = append(v, "missing expires")
	case expErr != nil:
		v = append(v, "invalid expires date "+e.Expires)
	case !expires.After(now):
		v = append(v, "expired (expires "+e.Expires+")")
	}

	if e.Provisional {
		switch {
		case e.ReviewAfter == "":
			v = append(v, "provisional entry missing review_after")
		default:
			ra, raErr := parseDate(e.ReviewAfter)
			if raErr != nil {
				v = append(v, "invalid review_after date "+e.ReviewAfter)
			} else if expErr == nil && !ra.Before(expires) {
				v = append(v, "review_after must be earlier than expires")
			}
		}
	}
	return v
}

func loadLedger(t *testing.T) exceptionLedger {
	t.Helper()
	b, err := os.ReadFile(exceptionLedgerPath())
	if err != nil {
		t.Fatalf("read exception ledger: %v", err)
	}
	var l exceptionLedger
	if err := yaml.Unmarshal(b, &l); err != nil {
		t.Fatalf("parse exception ledger: %v", err)
	}
	return l
}

// validEntry is a known-good provisional entry the negative tests mutate.
func validEntry() exceptionEntry {
	return exceptionEntry{
		ID:               "exception.example",
		Kind:             "baseline",
		Scope:            exceptionScope{Repo: "services", Paths: []string{"docs/x"}},
		Reason:           "why",
		Owner:            "davecourtois",
		Created:          "2026-06-26",
		Expires:          "2026-09-26",
		ReviewAfter:      "2026-08-26",
		Provisional:      true,
		RemovalCondition: "when X is true",
	}
}

// ref is a fixed evaluation time so the negative/positive unit cases are
// deterministic regardless of wall-clock.
var ref = time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)

// THE gate: every entry in the real ledger is valid as of NOW. Uses time.Now()
// (not ref) on purpose — an entry that passes its expires date starts failing
// here, which is exactly the bell.
func TestExceptionLedger_AllEntriesValid(t *testing.T) {
	l := loadLedger(t)
	if len(l.Exceptions) == 0 {
		t.Fatal("exception ledger is empty — expected the seeded governed exceptions")
	}
	seen := map[string]bool{}
	now := time.Now()
	for _, e := range l.Exceptions {
		if seen[e.ID] {
			t.Errorf("duplicate exception id %q", e.ID)
		}
		seen[e.ID] = true
		if vs := validateException(e, now); len(vs) != 0 {
			t.Errorf("%s: %s", e.ID, strings.Join(vs, "; "))
		}
	}
}

func TestExceptionLedger_MissingOwnerFails(t *testing.T) {
	e := validEntry()
	e.Owner = ""
	if !hasViolation(validateException(e, ref), "missing owner") {
		t.Fatal("missing owner must fail")
	}
}

func TestExceptionLedger_MissingExpiryFails(t *testing.T) {
	e := validEntry()
	e.Expires = ""
	if !hasViolation(validateException(e, ref), "missing expires") {
		t.Fatal("missing expires must fail")
	}
}

func TestExceptionLedger_MissingRemovalConditionFails(t *testing.T) {
	e := validEntry()
	e.RemovalCondition = ""
	if !hasViolation(validateException(e, ref), "missing removal_condition") {
		t.Fatal("missing removal_condition must fail")
	}
}

func TestExceptionLedger_ExpiredEntryFails(t *testing.T) {
	e := validEntry()
	e.Expires = "2026-06-01" // before ref
	e.ReviewAfter = "2026-05-15"
	if !hasViolation(validateException(e, ref), "expired") {
		t.Fatal("expired entry must fail")
	}
}

func TestExceptionLedger_InvalidDateFails(t *testing.T) {
	e := validEntry()
	e.Expires = "next-tuesday"
	if !hasViolation(validateException(e, ref), "invalid expires date") {
		t.Fatal("malformed date must fail")
	}
}

func TestExceptionLedger_ProvisionalRequiresReviewAfter(t *testing.T) {
	e := validEntry()
	e.Provisional = true
	e.ReviewAfter = ""
	if !hasViolation(validateException(e, ref), "provisional entry missing review_after") {
		t.Fatal("provisional entry without review_after must fail")
	}
}

func TestExceptionLedger_ProvisionalReviewAfterMustPrecedeExpiry(t *testing.T) {
	e := validEntry()
	e.Provisional = true
	e.ReviewAfter = "2026-09-26" // == expires, not before
	if !hasViolation(validateException(e, ref), "review_after must be earlier than expires") {
		t.Fatal("review_after not before expires must fail")
	}
}

func TestExceptionLedger_ValidProvisionalPasses(t *testing.T) {
	if vs := validateException(validEntry(), ref); len(vs) != 0 {
		t.Fatalf("a well-formed provisional entry must pass; got %v", vs)
	}
}

// Frozen-count "improve-only" ratchets (theme-tokens, interactive-element) are
// healthy gates, NOT exceptions — they tighten, they do not bypass. They must
// never be ledgered as exceptions (verify-first 2026-06-26).
func TestExceptionLedger_NoFrozenCountRatchets(t *testing.T) {
	l := loadLedger(t)
	for _, e := range l.Exceptions {
		if e.Kind == "frozen_count" {
			t.Errorf("%s: frozen-count ratchets are healthy gates, not exceptions — do not ledger them", e.ID)
		}
		low := strings.ToLower(e.ID)
		if strings.Contains(low, "frozen") || strings.Contains(low, "theme_token") || strings.Contains(low, "interactive_element") {
			t.Errorf("%s: looks like an improve-only ratchet, not a governed exception", e.ID)
		}
	}
}

func hasViolation(vs []string, substr string) bool {
	for _, s := range vs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

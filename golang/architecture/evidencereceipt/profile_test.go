// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestParseFreshness(t *testing.T) {
	cases := []struct {
		raw     string
		mode    FreshnessMode
		dur     time.Duration
		wantErr bool
	}{
		{raw: "per-result", mode: FreshnessPerResult},
		{raw: "per_result", mode: FreshnessPerResult},
		{raw: "self-declared", mode: FreshnessSelfDeclared},
		{raw: "24h", mode: FreshnessDuration, dur: 24 * time.Hour},
		{raw: "30m", mode: FreshnessDuration, dur: 30 * time.Minute},
		{raw: "7 days", mode: FreshnessDuration, dur: 7 * 24 * time.Hour},
		{raw: "2 weeks", mode: FreshnessDuration, dur: 14 * 24 * time.Hour},
		{raw: "1d", mode: FreshnessDuration, dur: 24 * time.Hour},
		{raw: "", wantErr: true},
		{raw: "whenever", wantErr: true},
		{raw: "0h", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			win, err := ParseFreshness(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if win.Mode != tc.mode {
				t.Fatalf("mode = %q, want %q", win.Mode, tc.mode)
			}
			if win.Mode == FreshnessDuration && win.Duration != tc.dur {
				t.Fatalf("duration = %v, want %v", win.Duration, tc.dur)
			}
		})
	}
}

func TestValidateProfile(t *testing.T) {
	valid := testProfile()
	if err := ValidateProfile(valid); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}

	cases := []struct {
		name  string
		mut   func(Profile) Profile
		valid bool
	}{
		{name: "active missing owner", mut: func(p Profile) Profile { p.Owner = ""; return p }},
		{name: "active missing observation path", mut: func(p Profile) Profile { p.LegalObservationPath = ""; return p }},
		{name: "active missing trust", mut: func(p Profile) Profile { p.Trust = ""; return p }},
		{name: "active missing governed target", mut: func(p Profile) Profile { p.GovernedTarget = ""; return p }},
		{name: "active missing freshness", mut: func(p Profile) Profile { p.Freshness = ""; return p }},
		{name: "active unparseable freshness", mut: func(p Profile) Profile { p.Freshness = "soon"; return p }},
		{name: "active runtime without target kind", mut: func(p Profile) Profile {
			p.EvidenceKind = closureprotocol.EvidenceRuntime
			return p
		}},
		{
			name: "inactive profile is lenient",
			mut: func(p Profile) Profile {
				p.Status = closureprotocol.ReceiptSuperseded
				p.Trust = ""
				p.Freshness = ""
				p.GovernedTarget = ""
				return p
			},
			valid: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProfile(tc.mut(testProfile()))
			if tc.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"testing"
)

// Fixtures unmarshal into prState exactly as the live `gh` JSON would (the
// classifier is pure, so these prove the verdict logic deterministically). Head
// SHA is "head-sha-abc" throughout; "old-sha-xyz" models an obsolete head.

func mustPR(t *testing.T, fixture string) prState {
	t.Helper()
	var pr prState
	if err := json.Unmarshal([]byte(fixture), &pr); err != nil {
		t.Fatalf("fixture unmarshal: %v", err)
	}
	return pr
}

func TestClassifyMergeAuthority(t *testing.T) {
	req := mergeCheckConfig{RequiredChecks: []string{"build-and-test"}, RequiredKnown: true}

	cases := []struct {
		name    string
		fixture string
		cfg     mergeCheckConfig
		want    string
	}{
		{
			// 1. all required checks green + clean merge state.
			name: "authorized",
			cfg:  req,
			fixture: `{"number":1,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictAuthorized,
		},
		{
			// 2. all checks green BUT the PR is conflicting — green must NOT override.
			name: "green-but-conflicting",
			cfg:  req,
			fixture: `{"number":2,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"CONFLICTING","mergeStateStatus":"DIRTY",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictConflict,
		},
		{
			// 3. a required check failed.
			name: "failed-required-check",
			cfg:  req,
			fixture: `{"number":3,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"failure","head_sha":"head-sha-abc"}]}`,
			want: verdictCheckFailure,
		},
		{
			// 4. a required check is still running.
			name: "pending-required-check",
			cfg:  req,
			fixture: `{"number":4,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"UNSTABLE",
				"checks":[{"name":"build-and-test","status":"in_progress","conclusion":"","head_sha":"head-sha-abc"}]}`,
			want: verdictPending,
		},
		{
			// 5. a required check has no run on the head at all.
			name: "missing-required-check",
			cfg:  mergeCheckConfig{RequiredChecks: []string{"build-and-test", "lint"}, RequiredKnown: true},
			fixture: `{"number":5,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictMissing,
		},
		{
			// 6. the required check's latest run is for an OBSOLETE head (stale green).
			name: "stale-check-sha",
			cfg:  req,
			fixture: `{"number":6,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"old-sha-xyz"}]}`,
			want: verdictStale,
		},
		{
			// 7. GitHub has not computed mergeability yet.
			name: "unknown-merge-state",
			cfg:  req,
			fixture: `{"number":7,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"UNKNOWN","mergeStateStatus":"UNKNOWN",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictUnknownState,
		},
		{
			// extra: wrong base — even a green, clean PR is blocked if it targets
			// the wrong branch (the base-retarget no-op class).
			name: "wrong-base",
			cfg:  mergeCheckConfig{ExpectedBase: "master", RequiredChecks: []string{"build-and-test"}, RequiredKnown: true},
			fixture: `{"number":8,"baseRefName":"develop","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictWrongBase,
		},
		{
			// extra: head BEHIND base — checks didn't run against the merge result.
			name: "behind-base-is-stale",
			cfg:  req,
			fixture: `{"number":9,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"BEHIND",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictStale,
		},
		{
			// extra: BLOCKED with all visible checks passing → a required gate we
			// can't see (a check or a review) is unsatisfied; never authorize.
			name: "blocked-with-no-visible-cause",
			cfg:  req,
			fixture: `{"number":10,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictMissing,
		},
		{
			// extra: draft PR is never an authorization candidate.
			name: "draft",
			cfg:  req,
			fixture: `{"number":11,"baseRefName":"master","headRefName":"feature","headRefOid":"head-sha-abc",
				"isDraft":true,"mergeable":"MERGEABLE","mergeStateStatus":"DRAFT",
				"checks":[{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"head-sha-abc"}]}`,
			want: verdictUnknownState,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyMergeAuthority(mustPR(t, tc.fixture), tc.cfg)
			if got.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q (reason: %s)", got.Verdict, tc.want, got.Reason)
			}
			// Exit contract: only MERGE_AUTHORIZED is exit 0.
			wantExit := 0
			if tc.want != verdictAuthorized {
				wantExit = 1
			}
			if ec := exitCodeForVerdict(got.Verdict); ec != wantExit {
				t.Fatalf("exit code = %d, want %d for verdict %s", ec, wantExit, got.Verdict)
			}
		})
	}
}

// When the required set is UNKNOWN (e.g. branch-protection API unavailable on a
// private free-tier repo), the verifier must NOT emit MISSING from absence — but
// it MUST still block a failing present check (conservative gate over all
// present checks).
func TestClassifyMergeAuthority_RequiredSetUnknown(t *testing.T) {
	cfg := mergeCheckConfig{RequiredKnown: false}

	failing := mustPR(t, `{"number":20,"baseRefName":"master","headRefName":"f","headRefOid":"h",
		"mergeable":"MERGEABLE","mergeStateStatus":"UNSTABLE",
		"checks":[{"name":"build","status":"completed","conclusion":"failure","head_sha":"h"}]}`)
	if got := classifyMergeAuthority(failing, cfg); got.Verdict != verdictCheckFailure {
		t.Fatalf("unknown-required + failing present check: verdict = %q, want %q", got.Verdict, verdictCheckFailure)
	}

	clean := mustPR(t, `{"number":21,"baseRefName":"master","headRefName":"f","headRefOid":"h",
		"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN",
		"checks":[{"name":"build","status":"completed","conclusion":"success","head_sha":"h"}]}`)
	got := classifyMergeAuthority(clean, cfg)
	if got.Verdict != verdictAuthorized {
		t.Fatalf("unknown-required + all present green + clean: verdict = %q, want %q", got.Verdict, verdictAuthorized)
	}
	if got.RequiredKnown {
		t.Fatalf("RequiredKnown should be false in the report when the required set is unknown")
	}
}

// A non-required check failing (UNSTABLE) must NOT block when the required set is
// known and all required checks pass — mergeability + required-pass is the law.
func TestClassifyMergeAuthority_NonRequiredFailureDoesNotBlock(t *testing.T) {
	cfg := mergeCheckConfig{RequiredChecks: []string{"build-and-test"}, RequiredKnown: true}
	pr := mustPR(t, `{"number":30,"baseRefName":"master","headRefName":"f","headRefOid":"h",
		"mergeable":"MERGEABLE","mergeStateStatus":"UNSTABLE",
		"checks":[
			{"name":"build-and-test","status":"completed","conclusion":"success","head_sha":"h"},
			{"name":"optional-lint","status":"completed","conclusion":"failure","head_sha":"h"}
		]}`)
	if got := classifyMergeAuthority(pr, cfg); got.Verdict != verdictAuthorized {
		t.Fatalf("non-required failure should not block: verdict = %q, want %q", got.Verdict, verdictAuthorized)
	}
}

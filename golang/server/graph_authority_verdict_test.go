// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/protobuf/encoding/protojson"
)

// The composed verdict must survive JSON serialization in ALL THREE states.
// proto3 omits default scalars, so `authoritative:false` vanishes on the wire
// and becomes indistinguishable from "never observed" -- the erasure that made
// the R1 benchmark environment unreproducible. UNSPECIFIED is still omitted (it
// is the enum's zero), but it is omitted as an ABSENCE, which is what it means;
// NOT_AUTHORITATIVE is a real negative and must always appear.
func TestAuthorityVerdictDistinguishesNegativeFromUnobserved(t *testing.T) {
	negative := &awarenesspb.GraphAuthority{
		Verdict:       awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE,
		Authoritative: false,
	}
	b, err := protojson.Marshal(negative)
	if err != nil {
		t.Fatalf("marshal negative: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "AUTHORITY_VERDICT_NOT_AUTHORITATIVE") {
		t.Fatalf("a real negative verdict must serialize explicitly; got %s", got)
	}
	if strings.Contains(got, `"authoritative"`) {
		t.Fatalf("precondition changed: authoritative=false is expected to be omitted by proto3; got %s", got)
	}

	unobserved := &awarenesspb.GraphAuthority{}
	b2, err := protojson.Marshal(unobserved)
	if err != nil {
		t.Fatalf("marshal unobserved: %v", err)
	}
	if strings.Contains(string(b2), "AUTHORITY_VERDICT") {
		t.Fatalf("an unobserved verdict must NOT claim a state; got %s", string(b2))
	}

	positive := &awarenesspb.GraphAuthority{
		Verdict:       awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE,
		Authoritative: true,
	}
	b3, err := protojson.Marshal(positive)
	if err != nil {
		t.Fatalf("marshal positive: %v", err)
	}
	if !strings.Contains(string(b3), "AUTHORITY_VERDICT_AUTHORITATIVE") {
		t.Fatalf("positive verdict must serialize; got %s", string(b3))
	}
}

// The compatibility bool is derived from the verdict, so the two surfaces
// cannot disagree. A drift here would let a consumer read one field and get a
// different answer than a consumer reading the other.
func TestAuthorityVerdictAndBoolNeverDisagree(t *testing.T) {
	for _, tc := range []struct {
		verdict awarenesspb.AuthorityVerdict
		bool_   bool
	}{
		{awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE, true},
		{awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_NOT_AUTHORITATIVE, false},
	} {
		a := &awarenesspb.GraphAuthority{Verdict: tc.verdict, Authoritative: tc.bool_}
		agrees := (a.GetVerdict() == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE) == a.GetAuthoritative()
		if !agrees {
			t.Fatalf("verdict %v and authoritative %v disagree", tc.verdict, tc.bool_)
		}
	}
}

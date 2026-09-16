// SPDX-License-Identifier: AGPL-3.0-only

package main

// P2-2, the MCP side. Blind review of #361 raised that GraphGeneration's docstring promised
// ("", nil) for an unreachable service while the code returned ("", err).
//
// The mismatch was real; the promise was the wrong half. golang/architecture/diffaudit's
// graph_generation_outage_test.go proves the evaluator degrades to cannot_verify either way and
// keeps the error text as the only thing distinguishing an outage from a checker that reports
// nothing. So the code is right and the docstring was corrected to say so.
//
// These pin the boundary itself, because a corrected comment is not enforcement: a future edit
// "fixing" this to ("", nil) to match the old promise would silently delete the only signal an
// operator can act on, and nothing in this package would have noticed.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func generationChecker(t *testing.T, metadata func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error)) *mcpSingleFileChecker {
	t.Helper()
	return &mcpSingleFileChecker{
		bridge: &bridge{client: fakeClient{metadata: metadata}},
		domain: "example.com/acme/thing",
	}
}

// P2-2-MCP-A. AN UNREACHABLE SERVICE PROPAGATES ITS ERROR. This is the assertion the reviewer's
// suggested repair would break.
func TestGraphGenerationPropagatesAnUnreachableServiceError(t *testing.T) {
	c := generationChecker(t, func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		return nil, status.Error(codes.Unavailable, "connection refused")
	})

	gen, err := c.GraphGeneration(context.Background())
	if err == nil {
		t.Fatal("an unreachable service reported no error, so the audit can no longer tell an " +
			"outage from a graph that states no generation")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the error does not carry what went wrong: %v", err)
	}
	if gen != "" {
		t.Errorf("a failed call returned a generation %q", gen)
	}
}

// P2-2-MCP-B. A REACHABLE SERVICE THAT STATES NOTHING is the absence case, and it is NOT an
// error: nothing failed, there is simply no identity to bind to. This is the half the docstring
// always described correctly.
func TestGraphGenerationReportsAbsenceWithoutAnError(t *testing.T) {
	c := generationChecker(t, func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{}, nil
	})

	gen, err := c.GraphGeneration(context.Background())
	if err != nil {
		t.Fatalf("a reachable service that stated no generation was reported as a failure: %v", err)
	}
	if gen != "" {
		t.Errorf("gen = %q, want empty", gen)
	}
}

// P2-2-MCP-C. The healthy path, and the CANONICAL FIELD it reads. GraphGeneration must report the
// live store digest -- not graph_build_commit, which identifies the rule snapshot and on this
// installation belongs to another repository, so two generations built from it are
// indistinguishable by it. Right position, wrong operand is the failure this forecloses.
func TestGraphGenerationReportsTheLiveStoreDigestNotTheRuleSnapshotCommit(t *testing.T) {
	const served = "c0b660fc42a50c4be4741592de178dfff3216c9b873d455c37cc864e27f01705"
	const snapshot = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	c := generationChecker(t, func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			LiveStoreGraphDigestSha256:    served,
			CertifiedAwarenessGraphCommit: snapshot,
		}, nil
	})

	gen, err := c.GraphGeneration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gen != served {
		t.Errorf("gen = %q, want the live store digest %q", gen, served)
	}
	if gen == snapshot {
		t.Error("GraphGeneration reported the rule-snapshot commit, which cannot distinguish two " +
			"generations built from one snapshot")
	}
}

// P2-2-MCP-D. And the audit asks about the DOMAIN IT IS AUDITING, or it compares two graphs.
func TestGraphGenerationAsksAboutTheAuditsOwnDomain(t *testing.T) {
	var asked string
	c := generationChecker(t, func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
		asked = in.GetDomain()
		return &awarenesspb.MetadataResponse{}, nil
	})
	if _, err := c.GraphGeneration(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != "example.com/acme/thing" {
		t.Errorf("metadata was asked about %q, want the audit's own domain", asked)
	}
}

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRunQuery_DistinguishesBackendUnreachableFromNoResults(t *testing.T) {
	prev := queryRPC
	queryRPC = func(context.Context, string, *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
		return nil, status.Error(codes.Unavailable, "connection refused")
	}
	defer func() { queryRPC = prev }()

	code, _, errOut := captureStdoutStderr(t, func() int {
		return runQuery([]string{"--mode", "by_class", "--class", "invariant"})
	})
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	for _, want := range []string{
		"query unavailable",
		"backend is unreachable",
		"not an empty/no-results response",
		"connection refused",
	} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr=%q missing %q", errOut, want)
		}
	}
}

func TestRunResolve_DistinguishesBackendUnreachableFromNoResults(t *testing.T) {
	prev := resolveRPC
	resolveRPC = func(context.Context, string, string, string, string) (*awarenesspb.ResolveResponse, error) {
		return nil, status.Error(codes.Unavailable, "connection refused")
	}
	defer func() { resolveRPC = prev }()

	code, _, errOut := captureStdoutStderr(t, func() int {
		return runResolve([]string{"Invariant", "demo.rule"})
	})
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	for _, want := range []string{
		"resolve unavailable",
		"backend is unreachable",
		"not an empty/no-results response",
		"connection refused",
	} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr=%q missing %q", errOut, want)
		}
	}
}

func TestRunMetadata_DistinguishesBackendUnreachableFromNoResults(t *testing.T) {
	prev := metadataRPC
	metadataRPC = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return nil, status.Error(codes.Unavailable, "connection refused")
	}
	defer func() { metadataRPC = prev }()

	code, _, errOut := captureStdoutStderr(t, func() int {
		return runMetadata(nil)
	})
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	for _, want := range []string{
		"metadata unavailable",
		"backend is unreachable",
		"not an empty/no-results response",
		"connection refused",
	} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr=%q missing %q", errOut, want)
		}
	}
}

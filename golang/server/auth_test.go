// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestParseBearer(t *testing.T) {
	cases := map[string]string{
		"Bearer secret":  "secret",
		"bearer secret":  "secret", // case-insensitive scheme
		"BEARER  spaced": "spaced",
		"rawtoken":       "rawtoken", // bare token accepted
		"  Bearer x  ":   "x",
	}
	for in, want := range cases {
		if got := parseBearer(in); got != want {
			t.Errorf("parseBearer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAuthExemptMethod(t *testing.T) {
	exempt := []string{"/grpc.health.v1.Health/Check", "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}
	for _, m := range exempt {
		if !isAuthExemptMethod(m) {
			t.Errorf("%s should be auth-exempt", m)
		}
	}
	if isAuthExemptMethod("/globular.awareness_graph.AwarenessGraph/Impact") {
		t.Error("AwarenessGraph methods must NOT be exempt")
	}
}

func ctxWithAuth(v string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", v))
}

func TestCheckBearer(t *testing.T) {
	if err := checkBearer(ctxWithAuth("Bearer secret"), "secret"); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	if err := checkBearer(ctxWithAuth("Bearer wrong"), "secret"); status.Code(err) != codes.Unauthenticated {
		t.Errorf("wrong token: want Unauthenticated, got %v", err)
	}
	if err := checkBearer(context.Background(), "secret"); status.Code(err) != codes.Unauthenticated {
		t.Errorf("no metadata: want Unauthenticated, got %v", err)
	}
	if err := checkBearer(ctxWithAuth(""), "secret"); status.Code(err) != codes.Unauthenticated {
		t.Errorf("empty header: want Unauthenticated, got %v", err)
	}
}

func TestBearerUnaryInterceptor(t *testing.T) {
	handler := func(context.Context, any) (any, error) { return "ok", nil }
	inter := bearerUnaryInterceptor("secret")
	protected := &grpc.UnaryServerInfo{FullMethod: "/globular.awareness_graph.AwarenessGraph/Impact"}

	// missing token on a protected method -> Unauthenticated, handler not run
	if _, err := inter(context.Background(), nil, protected, handler); status.Code(err) != codes.Unauthenticated {
		t.Errorf("missing token: want Unauthenticated, got %v", err)
	}
	// valid token -> handler runs
	if res, err := inter(ctxWithAuth("Bearer secret"), nil, protected, handler); err != nil || res != "ok" {
		t.Errorf("valid token: got (%v, %v), want (ok, nil)", res, err)
	}
	// exempt method with NO token -> allowed (probes must not need the secret)
	exempt := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}
	if res, err := inter(context.Background(), nil, exempt, handler); err != nil || res != "ok" {
		t.Errorf("exempt method: got (%v, %v), want (ok, nil)", res, err)
	}
}

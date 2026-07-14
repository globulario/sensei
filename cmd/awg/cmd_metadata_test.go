// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestRunDomains_PrintsSelectableDomains(t *testing.T) {
	prev := metadataRPC
	metadataRPC = func(context.Context, string, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			AvailableDomains: []string{"github.com/caddyserver/caddy", "github.com/gin-gonic/gin"},
		}, nil
	}
	defer func() { metadataRPC = prev }()

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runDomains(nil)
	})
	if code != 0 {
		t.Fatalf("runDomains code=%d stderr=%q", code, stderr)
	}
	if got, want := stdout, "github.com/caddyserver/caddy\ngithub.com/gin-gonic/gin\n"; got != want {
		t.Fatalf("stdout=%q, want %q", got, want)
	}
}

func TestRunDomains_JSON(t *testing.T) {
	prev := metadataRPC
	metadataRPC = func(context.Context, string, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{AvailableDomains: []string{"globular"}}, nil
	}
	defer func() { metadataRPC = prev }()

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runDomains([]string{"--json"})
	})
	if code != 0 {
		t.Fatalf("runDomains --json code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, `"available_domains"`) || !strings.Contains(stdout, `"globular"`) {
		t.Fatalf("stdout=%q missing JSON domain list", stdout)
	}
}

func TestRunMetadata_PrintsSelectableDomains(t *testing.T) {
	prev := metadataRPC
	metadataRPC = func(context.Context, string, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			ServerVersion:    "test",
			AvailableDomains: []string{"github.com/globulario/sensei", "globular"},
		}, nil
	}
	defer func() { metadataRPC = prev }()

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runMetadata(nil)
	})
	if code != 0 {
		t.Fatalf("runMetadata code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Selectable domains:",
		"github.com/globulario/sensei",
		"globular",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout=%q missing %q", stdout, want)
		}
	}
}

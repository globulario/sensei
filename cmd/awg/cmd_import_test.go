// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestDeriveDomain(t *testing.T) {
	cases := map[string]string{
		"https://github.com/gin-gonic/gin":     "github.com/gin-gonic/gin",
		"https://github.com/gin-gonic/gin.git": "github.com/gin-gonic/gin",
		"http://gitlab.com/a/b/c":              "gitlab.com/a/b/c",
		"git@github.com:gin-gonic/gin.git":     "github.com/gin-gonic/gin",
		"github.com/owner/repo":                "github.com/owner/repo",
		"not-a-url":                            "", // no slash → cannot derive
	}
	for in, want := range cases {
		if got := deriveDomain(in); got != want {
			t.Errorf("deriveDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveSlug(t *testing.T) {
	cases := map[string]string{
		"github.com/gin-gonic/gin": "gin-gonic/gin",
		"gitlab.com/a/b/c":         "b/c",
		"owner/repo":               "owner/repo",
		"single":                   "",
	}
	for in, want := range cases {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveRepoBaseAndSanitize(t *testing.T) {
	if got := deriveRepoBase("https://github.com/gin-gonic/gin.git"); got != "gin" {
		t.Errorf("deriveRepoBase = %q, want gin", got)
	}
	if got := sanitizeName("gin-gonic/gin@x"); got != "gin-gonicginx" {
		t.Errorf("sanitizeName = %q", got)
	}
	if got := sanitizeName("////"); got != "repo" {
		t.Errorf("sanitizeName empty fallback = %q, want repo", got)
	}
}

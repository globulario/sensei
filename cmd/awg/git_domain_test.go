// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestDomainFromRemoteURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/globulario/sensei.git":   "github.com/globulario/sensei",
		"https://github.com/globulario/sensei":       "github.com/globulario/sensei",
		"git@github.com:globulario/services.git":     "github.com/globulario/services",
		"ssh://git@github.com/globulario/sensei.git": "github.com/globulario/sensei",
		"ssh://git@gitlab.example.com:2222/t/p.git":  "gitlab.example.com/t/p",
		"  https://github.com/A/B.git\n":             "github.com/A/B", // host lowercased, path preserved
		"":                                           "",
		"not a url":                                  "",
	}
	for in, want := range cases {
		if got := domainFromRemoteURL(in); got != want {
			t.Errorf("domainFromRemoteURL(%q) = %q, want %q", in, got, want)
		}
	}
}

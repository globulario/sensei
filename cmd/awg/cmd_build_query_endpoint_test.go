// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestQueryEndpointPath(t *testing.T) {
	cases := map[string]string{
		"/store":        "/query",
		"/store/":       "/query",
		"/query":        "/query", // must NOT become /query/query
		"/query/":       "/query",
		"/":             "/query",
		"":              "/query",
		"/db":           "/db/query",
		"/prefix/store": "/prefix/query",
	}
	for in, want := range cases {
		if got := queryEndpointPath(in); got != want {
			t.Errorf("queryEndpointPath(%q) = %q, want %q", in, got, want)
		}
	}
}

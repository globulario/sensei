// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

func TestCDPDispatch_PropagatesProtocolError(t *testing.T) {
	c := newCDP()
	ch := make(chan cdpResult, 1)
	c.pending[7] = ch

	c.dispatch(cdpMsg{
		ID:    7,
		Error: &cdpErr{Code: -32000, Message: "navigation failed"},
	})

	res := <-ch
	if res.err == nil {
		t.Fatal("dispatch should propagate CDP error")
	}
	if !strings.Contains(res.err.Error(), "navigation failed") {
		t.Fatalf("err = %v", res.err)
	}
}

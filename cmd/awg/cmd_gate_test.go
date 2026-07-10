// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestGateVerdict(t *testing.T) {
	cases := []struct {
		name                         string
		wouldBlock, warns, evaluated int
		unavailable                  bool
		wantCode                     int
	}{
		{"clean pass", 0, 0, 5, false, 0},
		{"advisory only passes", 0, 3, 5, false, 0},
		{"one block fails", 1, 0, 5, false, 1},
		{"blocks win over warns", 2, 4, 5, false, 1},
		{"unavailable, nothing evaluated fails closed", 0, 0, 0, true, 2},
		{"unavailable but some evaluated + block still blocks", 1, 0, 3, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, summary := gateVerdict(tc.wouldBlock, tc.warns, tc.evaluated, tc.unavailable)
			if code != tc.wantCode {
				t.Errorf("gateVerdict = %d (%q), want %d", code, summary, tc.wantCode)
			}
			if summary == "" {
				t.Error("verdict summary must not be empty")
			}
		})
	}
}

// runGateCapture runs runGate with stdout captured, returning the exit code and
// the captured output.
func runGateCapture(args []string) (int, string) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	code := runGate(args)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return code, buf.String()
}

// Report-only mode must FAIL OPEN: a git/diff error (here, a non-repo path) must
// still exit 0 and emit a degraded report with the canonical summary line.
func TestGate_ReportOnly_FailsOpenOnError(t *testing.T) {
	code, out := runGateCapture([]string{
		"--report-only", "--repo-root", "/no/such/repo/awg-xyz", "--domain", "globular",
	})
	if code != 0 {
		t.Fatalf("report-only must exit 0 on a git error, got %d", code)
	}
	if !strings.Contains(out, "DEGRADED") {
		t.Errorf("expected a DEGRADED report, got:\n%s", out)
	}
	if !strings.Contains(out, "AWG gate report-only: 0 hard failures,") {
		t.Errorf("expected the canonical summary line, got:\n%s", out)
	}
}

// Without --report-only, the same git error is a hard error (exit 1). This
// guards that report-only is what relaxes the exit behaviour, not the default.
func TestGate_Default_ErrorsOnGitFailure(t *testing.T) {
	if code := runGate([]string{"--repo-root", "/no/such/repo/awg-xyz"}); code != 1 {
		t.Fatalf("default (non-report-only) gate must exit 1 on a git error, got %d", code)
	}
}

// parseAddedLinesFromDiff must collect only ADDED lines, key them by the new
// (b/) path, skip pure deletions (/dev/null target), and ignore removed lines —
// so the gate only ever judges the change's blast radius, never pre-existing
// code.
func TestParseAddedLinesFromDiff(t *testing.T) {
	diff := `diff --git a/modules/caddyhttp/x/caddyfile.go b/modules/caddyhttp/x/caddyfile.go
index 111..222 100644
--- a/modules/caddyhttp/x/caddyfile.go
+++ b/modules/caddyhttp/x/caddyfile.go
@@ -10,0 +11 @@ func f() {
+	return fmt.Errorf("bad")
@@ -20 +21 @@ func g() {
-	old := 1
+	new := 2
diff --git a/oldfile.go b/dev/null
deleted file mode 100644
--- a/oldfile.go
+++ /dev/null
@@ -1 +0,0 @@
-	gone := true
diff --git a/newpkg/new.go b/newpkg/new.go
new file mode 100644
--- /dev/null
+++ b/newpkg/new.go
@@ -0,0 +1 @@
+package newpkg
`
	got := parseAddedLinesFromDiff(diff)

	caddy := got["modules/caddyhttp/x/caddyfile.go"]
	if !strings.Contains(caddy, `fmt.Errorf("bad")`) || !strings.Contains(caddy, "new := 2") {
		t.Errorf("caddyfile added lines wrong: %q", caddy)
	}
	if strings.Contains(caddy, "old := 1") {
		t.Errorf("removed line leaked into added content: %q", caddy)
	}
	if _, ok := got["oldfile.go"]; ok {
		t.Errorf("pure deletion must not appear: %v", got)
	}
	if got["newpkg/new.go"] != "package newpkg" {
		t.Errorf("new file added content wrong: %q", got["newpkg/new.go"])
	}
}

func TestParseAddedLinesFromDiff_Empty(t *testing.T) {
	if got := parseAddedLinesFromDiff(""); len(got) != 0 {
		t.Errorf("empty diff must yield no files, got %v", got)
	}
}

// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"strings"
	"testing"
)

const sampleValidDiff = `diff --git a/cmd/main.go b/cmd/main.go
index 1234567..89abcdef 100644
--- a/cmd/main.go
+++ b/cmd/main.go
@@ -10,3 +10,4 @@ func main() {
 	fmt.Println("hello")
+	fmt.Println("world")
 }
diff --git a/docs/readme.md b/docs/readme.md
new file mode 100644
--- /dev/null
+++ b/docs/readme.md
@@ -0,0 +1,2 @@
+# Documentation
+Initial setup
diff --git a/old_file.go b/new_file.go
similarity index 100%
rename from old_file.go
rename to new_file.go
`

const sampleBinaryDiff = `diff --git a/assets/logo.png b/assets/logo.png
index 123456..789abc 100644
Binary files a/assets/logo.png and b/assets/logo.png differ
`

func TestParseDiff_ValidMultiFile(t *testing.T) {
	opts := DefaultParseOptions()
	parsed, err := ParseDiff(sampleValidDiff, opts)
	if err != nil {
		t.Fatalf("unexpected error parsing valid diff: %v", err)
	}
	if parsed == nil {
		t.Fatal("parsed result is nil")
	}
	if len(parsed.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(parsed.Files))
	}

	// Verify main.go
	f0 := parsed.Files[0]
	if f0.Path != "cmd/main.go" || f0.Kind != ChangeModify || f0.TotalAdded != 1 {
		t.Errorf("unexpected patch 0: %+v", f0)
	}

	// Verify readme.md (add)
	f1 := parsed.Files[1]
	if f1.Path != "docs/readme.md" || f1.Kind != ChangeAdd || f1.TotalAdded != 2 {
		t.Errorf("unexpected patch 1: %+v", f1)
	}

	// Verify rename
	f2 := parsed.Files[2]
	if f2.Path != "new_file.go" || f2.Kind != ChangeRename || f2.OldPath != "old_file.go" {
		t.Errorf("unexpected patch 2: %+v", f2)
	}
}

func TestParseDiff_BinaryFile(t *testing.T) {
	opts := DefaultParseOptions()
	parsed, err := ParseDiff(sampleBinaryDiff, opts)
	if err != nil {
		t.Fatalf("unexpected error parsing binary diff: %v", err)
	}
	if len(parsed.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(parsed.Files))
	}
	f := parsed.Files[0]
	if f.Path != "assets/logo.png" || f.Kind != ChangeBinary || !f.IsBinary {
		t.Errorf("expected ChangeBinary, got %+v", f)
	}
	if len(parsed.ReasonCodes) == 0 || parsed.ReasonCodes[0] != ReasonUnsupportedDiffFeature {
		t.Errorf("expected ReasonUnsupportedDiffFeature in reason codes, got %v", parsed.ReasonCodes)
	}
}

func TestParseDiff_HostileInputs(t *testing.T) {
	tests := []struct {
		name string
		diff string
	}{
		{
			name: "path traversal dotdot",
			diff: "diff --git a/../etc/passwd b/../etc/passwd\n--- a/../etc/passwd\n+++ b/../etc/passwd\n@@ -1,1 +1,1 @@\n",
		},
		{
			name: "absolute path leading slash",
			diff: "diff --git a//etc/shadow b//etc/shadow\n--- a//etc/shadow\n+++ b//etc/shadow\n@@ -1,1 +1,1 @@\n",
		},
		{
			name: "windows drive path",
			diff: "diff --git a/C:/Windows/system32 b/C:/Windows/system32\n",
		},
		{
			name: "null byte in payload",
			diff: "diff --git a/file.txt b/file.txt\x00\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDiff(tt.diff, DefaultParseOptions())
			if err == nil {
				t.Fatalf("expected error for hostile diff %q, but parse succeeded", tt.name)
			}
		})
	}
}

func TestParseDiff_DuplicateFilePaths(t *testing.T) {
	dupDiff := `diff --git a/src/app.go b/src/app.go
--- a/src/app.go
+++ b/src/app.go
@@ -1,1 +1,1 @@
+a
diff --git a/src/app.go b/src/app.go
--- a/src/app.go
+++ b/src/app.go
@@ -1,1 +1,1 @@
+b
`
	_, err := ParseDiff(dupDiff, DefaultParseOptions())
	if err == nil || !strings.Contains(err.Error(), "duplicate or ambiguous") {
		t.Fatalf("expected duplicate path error, got: %v", err)
	}
}

func TestParseDiff_BoundsExceeded(t *testing.T) {
	opts := ParseOptions{
		MaxBytes: 100,
		MaxFiles: 10,
		MaxHunks: 10,
	}
	largeDiff := strings.Repeat("diff --git a/a.go b/a.go\n", 10)
	_, err := ParseDiff(largeDiff, opts)
	if err == nil || !strings.Contains(err.Error(), "maximum size limit") {
		t.Fatalf("expected max size error, got: %v", err)
	}
}

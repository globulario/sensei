// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestEstablishBriefingRepositoryContext(t *testing.T) {
	dir := t.TempDir()

	t.Run("neither configured disables feedback", func(t *testing.T) {
		ctx, err := establishBriefingRepositoryContext("", "")
		if err != nil || ctx != nil {
			t.Fatalf("neither configured must be (nil,nil), got %+v %v", ctx, err)
		}
	})
	t.Run("root without domain fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext(dir, ""); err == nil {
			t.Fatal("root without domain must fail")
		}
	})
	t.Run("domain without root fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext("", "github.com/x/y"); err == nil {
			t.Fatal("domain without root must fail")
		}
	})
	t.Run("padded root fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext(" "+dir, "d"); err == nil {
			t.Fatal("padded root must fail")
		}
	})
	t.Run("padded domain fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext(dir, "d "); err == nil {
			t.Fatal("padded domain must fail")
		}
	})
	t.Run("whitespace domain fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext(dir, "a b"); err == nil {
			t.Fatal("whitespace domain must fail")
		}
	})
	t.Run("relative root fails startup (no filepath.Abs)", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext("relative/checkout", "d"); err == nil {
			t.Fatal("relative root must fail")
		}
	})
	t.Run("nonexistent root fails", func(t *testing.T) {
		if _, err := establishBriefingRepositoryContext(filepath.Join(dir, "nope"), "d"); err == nil {
			t.Fatal("nonexistent root must fail")
		}
	})
	t.Run("non-directory root fails", func(t *testing.T) {
		f := filepath.Join(dir, "file")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := establishBriefingRepositoryContext(f, "d"); err == nil {
			t.Fatal("non-directory root must fail")
		}
	})
	t.Run("valid absolute root resolves", func(t *testing.T) {
		ctx, err := establishBriefingRepositoryContext(dir, "github.com/x/y")
		if err != nil || ctx == nil {
			t.Fatalf("valid context: %+v %v", ctx, err)
		}
		if !filepath.IsAbs(ctx.Root) || ctx.Domain != "github.com/x/y" {
			t.Fatalf("context not canonical: %+v", ctx)
		}
	})
	t.Run("symlink root resolves once", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink semantics differ on Windows")
		}
		real := filepath.Join(dir, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		ctx, err := establishBriefingRepositoryContext(link, "d")
		if err != nil {
			t.Fatal(err)
		}
		resolvedReal, _ := filepath.EvalSymlinks(real)
		if ctx.Root != resolvedReal {
			t.Fatalf("symlink not resolved once: %q vs %q", ctx.Root, resolvedReal)
		}
	})
}

// BriefingRequest carries no filesystem-root field — a caller can never select which
// repository the server verifies.
func TestBriefingRequest_HasNoFilesystemRootField(t *testing.T) {
	md := (&awarenesspb.BriefingRequest{}).ProtoReflect().Descriptor()
	for i := 0; i < md.Fields().Len(); i++ {
		name := string(md.Fields().Get(i).Name())
		switch name {
		case "repo_root", "repository_root", "root", "path", "checkout", "filesystem_root":
			t.Fatalf("BriefingRequest exposes a filesystem-root field %q", name)
		}
	}
}

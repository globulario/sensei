// SPDX-License-Identifier: AGPL-3.0-only

package importgraph

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// The Rust language parser. Rust's dependency model is crate-level: the useful
// signal at component-rollup granularity is cross-crate `use other_crate::…`.
// Intra-crate paths (crate::/self::/super::, mod) never cross components, so this
// parser resolves at the crate boundary (defined by Cargo.toml) rather than the
// full module tree. It emits language-neutral ImportFacts; it decides no
// components, edges, or meaning. Best-effort, never fatal.

func init() { register("rust", parseRustImports) }

// rustStdlibCrates are the crate roots that are part of the language/runtime.
var rustStdlibCrates = map[string]bool{
	"std": true, "core": true, "alloc": true, "proc_macro": true, "test": true,
}

func parseRustImports(root string) (ParseResult, error) {
	crates := loadRustCrates(root)
	var res ParseResult

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			if p != root && isNestedRepo(p) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".rs" {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isRustTestPath(rel) {
			return nil
		}
		res.Files = append(res.Files, SourceFile{Path: rel, IsEntrypoint: d.Name() == "main.rs"})

		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, seg := range extractRustUseRoots(src) {
			res.Imports = append(res.Imports, resolveRustImport(rel, seg, crates))
		}
		return nil
	})
	if walkErr != nil {
		return ParseResult{}, walkErr
	}
	return res, nil
}

// isRustTestPath reports whether a repo-relative .rs path is in an integration
// test or benchmark tree (Cargo convention), which is not architecture.
func isRustTestPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "tests" || seg == "benches" {
			return true
		}
	}
	return false
}

// ── import extraction (tree-sitter) ──────────────────────────────────────────

// extractRustUseRoots returns the first path segment (the crate/root identifier)
// of every `use` and `extern crate` declaration.
func extractRustUseRoots(src []byte) []string {
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tsrust.Language())); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	var out []string
	var visit func(n *ts.Node)
	visit = func(n *ts.Node) {
		switch n.Kind() {
		case "use_declaration":
			if seg := rustFirstSegment(nodeText(n, src), "use "); seg != "" {
				out = append(out, seg)
			}
		case "extern_crate_declaration":
			if seg := rustFirstSegment(nodeText(n, src), "crate "); seg != "" {
				out = append(out, seg)
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(tree.RootNode())
	return out
}

// rustFirstSegment slices the first path identifier out of a declaration's text,
// after the keyword. e.g. "pub use serde::{Deserialize};" + "use " -> "serde".
func rustFirstSegment(text, afterKeyword string) string {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	i := strings.Index(t, afterKeyword)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(t[i+len(afterKeyword):])
	// First segment = up to "::", "{", whitespace, or "as".
	cut := len(rest)
	for _, sep := range []string{"::", "{", " ", "\t", "\n", "*"} {
		if j := strings.Index(rest, sep); j >= 0 && j < cut {
			cut = j
		}
	}
	seg := strings.Trim(rest[:cut], " \t\n")
	// `use ::foo` (global path) — the leading "::" yields an empty segment; the
	// caller's separator handling already trims it, but guard anyway.
	return strings.TrimSpace(seg)
}

// ── resolution ───────────────────────────────────────────────────────────────

func resolveRustImport(sourceFile, seg string, crates map[string]string) ImportFact {
	f := ImportFact{Language: "rust", SourceFile: sourceFile, Raw: seg, Kind: "static"}
	switch seg {
	case "crate", "self", "super", "Self":
		f.Resolved = "internal" // intra-crate; TargetPath empty → never a cross-component edge
		return f
	}
	if rustStdlibCrates[seg] {
		f.Resolved = "stdlib"
		return f
	}
	if dir, ok := crates[rustNormalizeCrate(seg)]; ok {
		f.Resolved, f.TargetPath = "internal", dir
		return f
	}
	f.Resolved = "external"
	return f
}

func rustNormalizeCrate(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// ── crate discovery (Cargo.toml [package] name -> dir) ───────────────────────

func loadRustCrates(root string) map[string]string {
	crates := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (excludedDir(d.Name()) || isNestedRepo(p)) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "Cargo.toml" {
			return nil
		}
		name := cargoPackageName(p)
		if name == "" {
			return nil // virtual workspace root (no [package]) or unparsable
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return nil
		}
		crates[rustNormalizeCrate(name)] = filepath.ToSlash(rel)
		return nil
	})
	return crates
}

// cargoPackageName extracts [package] name from a Cargo.toml (minimal TOML scan).
func cargoPackageName(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	inPackage := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "name"); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "="); ok {
				return strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	return ""
}

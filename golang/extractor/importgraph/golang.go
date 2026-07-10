// SPDX-License-Identifier: AGPL-3.0-only

package importgraph

import (
	"bufio"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The Go language parser: the first implementation of the import-graph pattern.
// It knows Go syntax and module resolution and emits language-neutral
// ImportFacts; it decides no components, edges, or meaning.

func init() { register("go", parseGoImports) }

// parseGoImports walks the repo's non-test, non-generated .go files and returns
// their source files + import facts. Internal vs stdlib vs external is resolved
// against the module path(s) in go.mod — discovered repo-wide, so a module that
// lives in a subdirectory (e.g. golang/go.mod) is honored, not just one at the
// repo root.
func parseGoImports(root string) (ParseResult, error) {
	modules := readGoModules(root)
	fset := token.NewFileSet()
	var res ParseResult

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			// A subdirectory containing a .git entry is a repository boundary
			// (submodule / vendored or CI-placed nested checkout) — not part of
			// this repo's source. Skip it; keep the root repo itself scannable.
			if p != root && isNestedRepo(p) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, ".pb.go") || strings.HasSuffix(name, "_generated.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		res.Files = append(res.Files, SourceFile{Path: rel, IsEntrypoint: name == "main.go"})

		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // tolerate unparseable files; still counted as a source file
		}
		for _, spec := range f.Imports {
			imp, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil {
				continue
			}
			res.Imports = append(res.Imports, goFact(rel, imp, modules))
		}
		return nil
	})
	if walkErr != nil {
		return ParseResult{}, walkErr
	}
	return res, nil
}

// goModule is one discovered module: its declared path and the repo-root-relative
// directory of its go.mod (forward slashes; "" for a module at the repo root).
type goModule struct {
	path string
	dir  string
}

// goFact resolves one Go import path into a normalized ImportFact, matched
// against every discovered module (longest module path first → longest-prefix
// wins for nested modules). For an internal import, TargetPath is the package's
// directory RELATIVE TO THE REPO ROOT — the module's own directory prefixed onto
// the import's module-relative tail — so it lands on the same component ids the
// rollup emits even when the module lives in a subdirectory.
func goFact(sourceFile, imp string, modules []goModule) ImportFact {
	f := ImportFact{Language: "go", SourceFile: sourceFile, Raw: imp, Kind: "static"}
	for _, m := range modules {
		if m.path == "" || (imp != m.path && !strings.HasPrefix(imp, m.path+"/")) {
			continue
		}
		f.Resolved = "internal"
		sub := strings.TrimPrefix(strings.TrimPrefix(imp, m.path), "/")
		switch {
		case m.dir == "":
			f.TargetPath = sub
		case sub == "":
			f.TargetPath = m.dir
		default:
			f.TargetPath = m.dir + "/" + sub
		}
		return f
	}
	if isGoStdlib(imp) {
		f.Resolved = "stdlib"
		return f
	}
	f.Resolved = "external"
	return f
}

// isGoStdlib reports whether a Go import path is a standard-library package: its
// first path segment carries no dot (e.g. "fmt", "net/http"). Third-party and
// module-internal paths have a dotted domain in the first segment.
func isGoStdlib(imp string) bool {
	first := imp
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		first = imp[:i]
	}
	return !strings.Contains(first, ".")
}

// readGoModules discovers every go.mod under root (not just root/go.mod) and
// returns the modules sorted longest-module-path first, so goFact's prefix match
// resolves a nested module before its parent. Excluded and nested-repo dirs are
// skipped with the same rules as the source walk, so vendored/submodule go.mod
// files never count as this repo's modules.
func readGoModules(root string) []goModule {
	var mods []goModule
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
		if d.Name() != "go.mod" {
			return nil
		}
		mp := parseModulePath(p)
		if mp == "" {
			return nil
		}
		dir, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return nil
		}
		if dir = filepath.ToSlash(dir); dir == "." {
			dir = ""
		}
		mods = append(mods, goModule{path: mp, dir: dir})
		return nil
	})
	sort.Slice(mods, func(i, j int) bool { return len(mods[i].path) > len(mods[j].path) })
	return mods
}

// parseModulePath returns the module path declared in a go.mod file, or "".
func parseModulePath(goModPath string) string {
	f, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

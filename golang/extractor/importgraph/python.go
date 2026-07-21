// SPDX-License-Identifier: AGPL-3.0-only

package importgraph

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// The Python language parser. It knows Python import syntax and resolution
// (relative dot-imports + absolute imports probed against candidate source
// roots) and emits language-neutral ImportFacts; it decides no components,
// edges, or meaning. Resolution is best-effort and never fatal.

func init() { register("python", parsePythonImports) }

// pyRootCandidates are the directories Python packages commonly live under
// (in addition to the repo root). Used to resolve absolute imports.
var pyRootCandidates = []string{"src", "lib", "app", "packages", "apps", "modules"}

func parsePythonImports(root string) (ParseResult, error) {
	roots := pyCandidateRoots(root)
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
		name := d.Name()
		if !isPySource(name) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		res.Files = append(res.Files, SourceFile{Path: rel, IsEntrypoint: isPyEntrypoint(name)})

		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, imp := range extractPythonImports(src) {
			res.Imports = append(res.Imports, resolvePyImport(root, rel, imp, roots))
		}
		return nil
	})
	if walkErr != nil {
		return ParseResult{}, walkErr
	}
	return res, nil
}

func pyCandidateRoots(root string) []string {
	roots := []string{"."}
	for _, name := range pyRootCandidates {
		if fi, err := os.Stat(filepath.Join(root, name)); err == nil && fi.IsDir() {
			roots = append(roots, name)
		}
	}
	return roots
}

// ── file classification ──────────────────────────────────────────────────────

func isPySource(name string) bool {
	l := strings.ToLower(name)
	if filepath.Ext(l) != ".py" {
		return false // also excludes .pyi stubs
	}
	if strings.HasSuffix(l, "_pb2.py") || strings.HasSuffix(l, "_pb2_grpc.py") {
		return false // generated protobuf
	}
	if strings.HasPrefix(l, "test_") || strings.HasSuffix(l, "_test.py") {
		return false // tests
	}
	return true
}

func isPyEntrypoint(name string) bool {
	switch strings.ToLower(name) {
	case "__main__.py", "main.py":
		return true
	}
	return false
}

// ── import extraction (tree-sitter) ──────────────────────────────────────────

// pyImport is a raw Python import: an absolute dotted module (level 0) or a
// relative import (level = leading-dot count, module = the dotted tail).
type pyImport struct {
	module string
	level  int
}

func extractPythonImports(src []byte) []pyImport {
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tspython.Language())); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	var out []pyImport
	var visit func(n *ts.Node)
	visit = func(n *ts.Node) {
		switch n.Kind() {
		case "import_statement":
			// import a, a.b as c — each name is a dotted_name or aliased_import.
			for i := uint(0); i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				switch c.Kind() {
				case "dotted_name":
					out = append(out, pyImport{module: nodeText(c, src)})
				case "aliased_import":
					if dn := tsNamedChildOfKind(c, "dotted_name"); dn != nil {
						out = append(out, pyImport{module: nodeText(dn, src)})
					}
				}
			}
		case "import_from_statement":
			// The module being imported FROM is the first named child; the
			// imported names follow and are not dependency targets.
			if n.NamedChildCount() > 0 {
				m := n.NamedChild(0)
				switch m.Kind() {
				case "dotted_name":
					out = append(out, pyImport{module: nodeText(m, src)})
				case "relative_import":
					level, tail := pyRelative(m, src)
					out = append(out, pyImport{module: tail, level: level})
				}
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(tree.RootNode())
	return out
}

// pyRelative parses a relative_import node ("." / ".." / "..pkg.mod") into its
// leading-dot level and dotted tail.
func pyRelative(n *ts.Node, src []byte) (int, string) {
	full := nodeText(n, src)
	level := 0
	for level < len(full) && full[level] == '.' {
		level++
	}
	if level == 0 {
		level = 1 // a relative_import always has at least one dot
	}
	return level, strings.TrimLeft(full, ".")
}

// ── resolution ───────────────────────────────────────────────────────────────

func resolvePyImport(root, sourceFile string, imp pyImport, roots []string) ImportFact {
	raw := imp.module
	if imp.level > 0 {
		raw = strings.Repeat(".", imp.level) + imp.module
	}
	f := ImportFact{Language: "python", SourceFile: sourceFile, Raw: raw, Kind: "static"}

	if imp.level > 0 {
		base := path.Dir(sourceFile)
		for i := 1; i < imp.level; i++ {
			base = path.Dir(base)
		}
		target := base
		if imp.module != "" {
			target = path.Join(base, strings.ReplaceAll(imp.module, ".", "/"))
		}
		if td, ok := probePyModule(root, target); ok {
			f.Resolved, f.TargetPath = "internal", td
		} else {
			f.Resolved = "unresolved"
		}
		return f
	}

	// absolute: probe candidate roots for the module on disk
	rel := strings.ReplaceAll(imp.module, ".", "/")
	for _, r := range roots {
		cand := rel
		if r != "." {
			cand = path.Join(r, rel)
		}
		if td, ok := probePyModule(root, cand); ok {
			f.Resolved, f.TargetPath = "internal", td
			return f
		}
	}
	first := imp.module
	if i := strings.IndexByte(first, '.'); i >= 0 {
		first = first[:i]
	}
	if pyStdlib[first] {
		f.Resolved = "stdlib"
	} else {
		f.Resolved = "external"
	}
	return f
}

// probePyModule resolves a repo-relative dotted-path (no extension) to its
// component target DIRECTORY: a package dir as-is, or a module file's dir.
func probePyModule(root, rel string) (string, bool) {
	rel = path.Clean(rel)
	if rel == "." || rel == "" {
		return "", false
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		return rel, true // package directory (with or without __init__.py)
	}
	if _, err := os.Stat(abs + ".py"); err == nil {
		return path.Dir(rel), true // module file; its dir is the component target
	}
	return "", false
}

// ── Python 3 standard-library top-level module names ─────────────────────────

var pyStdlibList = []string{
	"__future__", "abc", "argparse", "array", "ast", "asyncio", "atexit", "base64",
	"bdb", "binascii", "bisect", "builtins", "bz2", "calendar", "cmath", "cmd",
	"codecs", "collections", "colorsys", "compileall", "concurrent", "configparser",
	"contextlib", "contextvars", "copy", "copyreg", "csv", "ctypes", "curses",
	"dataclasses", "datetime", "decimal", "difflib", "dis", "doctest", "email",
	"encodings", "enum", "errno", "faulthandler", "fcntl", "filecmp", "fileinput",
	"fnmatch", "fractions", "ftplib", "functools", "gc", "getopt", "getpass",
	"gettext", "glob", "graphlib", "gzip", "hashlib", "heapq", "hmac", "html",
	"http", "imaplib", "importlib", "inspect", "io", "ipaddress", "itertools",
	"json", "keyword", "linecache", "locale", "logging", "lzma", "mailbox",
	"marshal", "math", "mimetypes", "mmap", "multiprocessing", "numbers", "operator",
	"os", "pathlib", "pdb", "pickle", "pickletools", "pkgutil", "platform", "plistlib",
	"poplib", "posixpath", "pprint", "profile", "pstats", "pty", "pwd", "py_compile",
	"pyclbr", "pydoc", "queue", "quopri", "random", "re", "reprlib", "resource",
	"runpy", "sched", "secrets", "select", "selectors", "shelve", "shlex", "shutil",
	"signal", "site", "smtplib", "socket", "socketserver", "sqlite3", "ssl", "stat",
	"statistics", "string", "stringprep", "struct", "subprocess", "symtable", "sys",
	"sysconfig", "syslog", "tarfile", "tempfile", "termios", "textwrap", "threading",
	"time", "timeit", "tkinter", "token", "tokenize", "tomllib", "trace", "traceback",
	"tracemalloc", "tty", "turtle", "types", "typing", "unicodedata", "unittest",
	"urllib", "uuid", "venv", "warnings", "wave", "weakref", "webbrowser", "xml",
	"xmlrpc", "zipapp", "zipfile", "zipimport", "zlib", "zoneinfo",
}

var pyStdlib = func() map[string]bool {
	m := make(map[string]bool, len(pyStdlibList))
	for _, n := range pyStdlibList {
		m[n] = true
	}
	return m
}()

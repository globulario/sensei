// SPDX-License-Identifier: AGPL-3.0-only

package importgraph

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	yaml "gopkg.in/yaml.v3"
)

// The TypeScript/JavaScript language parser. It knows TS/JS import syntax and
// resolution (relative paths + tsconfig/jsconfig path aliases) and emits
// language-neutral ImportFacts; it decides no components, edges, or meaning.
//
// The tree-sitter-typescript grammar is a superset of JavaScript for import
// syntax, so it parses .ts/.tsx/.js/.jsx/.mjs/.cjs — no JS-specific grammar
// dependency. Resolution is best-effort and never fatal: anything not found on
// disk is recorded as Resolved=="unresolved" and dropped by the shared core.

func init() { register("typescript", parseTypeScriptImports) }

var tsSourceExts = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
}

// tsResolveExts is the candidate extension order when probing a module path on
// disk (mirrors TS/Node resolution closely enough for component rollup).
var tsResolveExts = []string{".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs", ".json"}

func parseTypeScriptImports(root string) (ParseResult, error) {
	cfgs := loadTSConfigs(root)
	ws := loadWorkspacePackages(root)
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
		if !isTSSource(name) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		res.Files = append(res.Files, SourceFile{Path: rel, IsEntrypoint: isTSEntrypoint(name)})

		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		jsx := strings.HasSuffix(strings.ToLower(name), ".tsx") || strings.HasSuffix(strings.ToLower(name), ".jsx")
		for _, sp := range extractTSImports(src, jsx) {
			res.Imports = append(res.Imports, resolveTSImport(root, rel, sp.spec, sp.kind, cfgs, ws))
		}
		return nil
	})
	if walkErr != nil {
		return ParseResult{}, walkErr
	}
	return res, nil
}

// ── file classification ──────────────────────────────────────────────────────

func isTSSource(name string) bool {
	l := strings.ToLower(name)
	if strings.HasSuffix(l, ".d.ts") {
		return false
	}
	if strings.Contains(l, "_pb.") || strings.HasSuffix(l, "_grpc_web_pb.ts") || strings.HasSuffix(l, "_grpc_web_pb.js") {
		return false
	}
	if isTSTest(l) {
		return false
	}
	return tsSourceExts[filepath.Ext(l)]
}

func isTSTest(lower string) bool {
	for _, suf := range []string{
		".test.ts", ".test.tsx", ".test.js", ".test.jsx",
		".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx",
	} {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

func isTSEntrypoint(name string) bool {
	switch strings.ToLower(name) {
	case "index.ts", "index.tsx", "index.js", "main.ts", "main.js":
		return true
	}
	return false
}

// ── import extraction (tree-sitter) ──────────────────────────────────────────

type tsSpec struct {
	spec string
	kind string // static | side_effect | require | dynamic
}

// extractTSImports parses one TS/JS file and returns its string-literal import
// specifiers. Non-literal dynamic imports (templates) are skipped.
func extractTSImports(src []byte, jsx bool) []tsSpec {
	parser := ts.NewParser()
	defer parser.Close()
	lang := ts.NewLanguage(tstypescript.LanguageTypescript())
	if jsx {
		lang = ts.NewLanguage(tstypescript.LanguageTSX())
	}
	if err := parser.SetLanguage(lang); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	var out []tsSpec
	var visit func(n *ts.Node)
	visit = func(n *ts.Node) {
		switch n.Kind() {
		case "import_statement":
			if s, ok := tsDirectSourceString(n, src); ok {
				kind := "static"
				if tsNamedChildOfKind(n, "import_clause") == nil {
					kind = "side_effect"
				}
				out = append(out, tsSpec{s, kind})
			}
		case "export_statement":
			// only re-exports carry a source string ("export … from '…'")
			if s, ok := tsDirectSourceString(n, src); ok {
				out = append(out, tsSpec{s, "static"})
			}
		case "call_expression":
			if s, kind, ok := tsCallImport(n, src); ok {
				out = append(out, tsSpec{s, kind})
			}
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(tree.RootNode())
	return out
}

// tsDirectSourceString returns the value of a direct `string` child (the module
// source of an import/export statement), if present.
func tsDirectSourceString(n *ts.Node, src []byte) (string, bool) {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c.Kind() == "string" {
			return tsStringValue(c, src), true
		}
	}
	return "", false
}

func tsNamedChildOfKind(n *ts.Node, kind string) *ts.Node {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == kind {
			return c
		}
	}
	return nil
}

// tsCallImport handles require("x") and dynamic import("x") with a literal arg.
func tsCallImport(n *ts.Node, src []byte) (string, string, bool) {
	if n.NamedChildCount() == 0 {
		return "", "", false
	}
	fn := n.NamedChild(0)
	kind := ""
	switch fn.Kind() {
	case "import":
		kind = "dynamic"
	case "identifier":
		if nodeText(fn, src) == "require" {
			kind = "require"
		}
	}
	if kind == "" {
		return "", "", false
	}
	args := tsNamedChildOfKind(n, "arguments")
	if args == nil {
		return "", "", false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if a := args.NamedChild(i); a.Kind() == "string" {
			return tsStringValue(a, src), kind, true
		}
	}
	return "", "", false
}

func tsStringValue(n *ts.Node, src []byte) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == "string_fragment" {
			return nodeText(c, src)
		}
	}
	t := nodeText(n, src) // empty string literal: "" / '' / ``
	if len(t) >= 2 {
		t = t[1 : len(t)-1]
	}
	return t
}

func nodeText(n *ts.Node, src []byte) string { return string(src[n.StartByte():n.EndByte()]) }

// ── resolution ───────────────────────────────────────────────────────────────

func resolveTSImport(root, sourceFile, spec, kind string, cfgs *tsConfigIndex, ws *workspaceIndex) ImportFact {
	f := ImportFact{Language: "typescript", SourceFile: sourceFile, Raw: spec, Kind: kind}
	switch {
	case spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
		base := path.Clean(path.Join(path.Dir(sourceFile), spec))
		if td, ok := probeTSModule(root, base); ok {
			f.Resolved, f.TargetPath = "internal", td
		} else {
			f.Resolved = "unresolved"
		}
	default:
		if cands := cfgs.aliasCandidates(sourceFile, spec); cands != nil {
			f.Resolved = "unresolved" // matched an alias pattern but not found on disk
			for _, c := range cands {
				if td, ok := probeTSModule(root, c); ok {
					f.Resolved, f.TargetPath = "internal", td
					break
				}
			}
		} else if dir, ok := ws.lookup(spec); ok {
			// A bare import of an in-repo workspace package (npm/pnpm/yarn) — the
			// package name resolves to its directory, so it is internal.
			f.Resolved, f.TargetPath = "internal", dir
		} else if isNodeBuiltin(spec) {
			f.Resolved = "stdlib"
		} else {
			f.Resolved = "external"
		}
	}
	return f
}

// probeTSModule resolves a repo-relative module path to its component target
// DIRECTORY, mirroring TS/Node file resolution. Returns ok=false if nothing is
// found on disk.
func probeTSModule(root, rel string) (string, bool) {
	rel = path.Clean(rel)
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		return rel, true // a package directory (resolves via its index.*)
	}
	for _, ext := range tsResolveExts {
		if _, err := os.Stat(abs + ext); err == nil {
			return path.Dir(rel), true // the module file's directory is the component target
		}
	}
	for _, ext := range tsResolveExts {
		if _, err := os.Stat(filepath.Join(abs, "index"+ext)); err == nil {
			return rel, true
		}
	}
	return "", false
}

// ── workspace package index (npm / pnpm / yarn) ──────────────────────────────

// workspaceIndex maps an in-repo workspace package name to its repo-relative
// directory, so a bare import of that name (e.g. "@acme/ui") resolves as
// internal rather than external. Names are kept longest-first for prefix lookup.
type workspaceIndex struct {
	names []string          // package names, sorted longest-first
	dir   map[string]string // package name → repo-relative dir
}

// lookup resolves a bare specifier to a workspace package's dir: an exact
// package-name match or a subpath import (name + "/..."). Returns the package
// dir (the rollup collapses subpaths to the package's component).
func (w *workspaceIndex) lookup(spec string) (string, bool) {
	for _, name := range w.names {
		if spec == name || strings.HasPrefix(spec, name+"/") {
			return w.dir[name], true
		}
	}
	return "", false
}

// loadWorkspacePackages discovers workspace member packages from a root
// pnpm-workspace.yaml or package.json "workspaces", and maps each member's
// declared package.json name to its directory.
func loadWorkspacePackages(root string) *workspaceIndex {
	idx := &workspaceIndex{dir: map[string]string{}}
	for _, glob := range workspaceGlobs(root) {
		for _, dir := range expandWorkspaceGlob(root, glob) {
			if name := packageJSONName(filepath.Join(root, filepath.FromSlash(dir))); name != "" {
				if _, dup := idx.dir[name]; !dup {
					idx.dir[name] = dir
					idx.names = append(idx.names, name)
				}
			}
		}
	}
	sort.Slice(idx.names, func(i, j int) bool { return len(idx.names[i]) > len(idx.names[j]) })
	return idx
}

// workspaceGlobs returns the member globs from pnpm-workspace.yaml or the root
// package.json "workspaces" (array, or {packages: [...]}).
func workspaceGlobs(root string) []string {
	if data, err := os.ReadFile(filepath.Join(root, "pnpm-workspace.yaml")); err == nil {
		var doc struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal(data, &doc) == nil && len(doc.Packages) > 0 {
			return doc.Packages
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var doc struct {
			Workspaces json.RawMessage `json:"workspaces"`
		}
		if json.Unmarshal(data, &doc) == nil && len(doc.Workspaces) > 0 {
			var arr []string
			if json.Unmarshal(doc.Workspaces, &arr) == nil && len(arr) > 0 {
				return arr
			}
			var obj struct {
				Packages []string `json:"packages"`
			}
			if json.Unmarshal(doc.Workspaces, &obj) == nil {
				return obj.Packages
			}
		}
	}
	return nil
}

// expandWorkspaceGlob expands a member glob to repo-relative dirs: "dir/*" (one
// level) or an exact "dir". Deep "**" globs are not expanded (noted limitation).
func expandWorkspaceGlob(root, glob string) []string {
	glob = filepath.ToSlash(strings.TrimSuffix(glob, "/"))
	if strings.HasSuffix(glob, "/*") {
		base := strings.TrimSuffix(glob, "/*")
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(base)))
		if err != nil {
			return nil
		}
		var out []string
		for _, e := range entries {
			if e.IsDir() && e.Name() != "node_modules" && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, path.Join(base, e.Name()))
			}
		}
		return out
	}
	if strings.Contains(glob, "*") {
		return nil // deep/other globs unsupported in v1
	}
	if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(glob))); err == nil && fi.IsDir() {
		return []string{glob}
	}
	return nil
}

// packageJSONName reads the "name" field of <dir>/package.json, or "".
func packageJSONName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(doc.Name)
}

// ── tsconfig / jsconfig path-alias index ─────────────────────────────────────

type tsConfig struct {
	dir     string              // repo-relative dir of the config (for nearest-ancestor lookup)
	baseUrl string              // repo-relative resolved base for path mapping
	paths   map[string][]string // compilerOptions.paths
}

type tsConfigIndex struct{ configs []tsConfig }

// loadTSConfigs discovers tsconfig.json / jsconfig.json across the repo.
func loadTSConfigs(root string) *tsConfigIndex {
	idx := &tsConfigIndex{}
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
		if d.Name() != "tsconfig.json" && d.Name() != "jsconfig.json" {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		if cfg, ok := parseTSConfig(root, filepath.ToSlash(rel), 0); ok {
			idx.configs = append(idx.configs, cfg)
		}
		return nil
	})
	return idx
}

func parseTSConfig(root, rel string, depth int) (tsConfig, bool) {
	if depth > 3 {
		return tsConfig{}, false
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return tsConfig{}, false
	}
	var doc struct {
		Extends         string `json:"extends"`
		CompilerOptions struct {
			BaseUrl string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &doc); err != nil {
		return tsConfig{}, false
	}
	dir := path.Dir(rel)
	baseUrl := doc.CompilerOptions.BaseUrl
	paths := doc.CompilerOptions.Paths

	// Best-effort single-level extends: a config with no path mapping of its own
	// inherits the base's baseUrl + paths (resolved against the base's dir).
	if len(paths) == 0 && strings.TrimSpace(doc.Extends) != "" {
		if base, ok := parseTSConfig(root, path.Clean(path.Join(dir, doc.Extends)), depth+1); ok {
			return tsConfig{dir: dir, baseUrl: base.baseUrl, paths: base.paths}, true
		}
	}

	resolvedBase := dir
	if baseUrl != "" {
		resolvedBase = path.Clean(path.Join(dir, baseUrl))
	}
	return tsConfig{dir: dir, baseUrl: resolvedBase, paths: paths}, true
}

// aliasCandidates returns the ordered repo-relative target paths a path-alias
// maps spec to (using the file's nearest-ancestor config), or nil if spec
// matches no alias key.
func (idx *tsConfigIndex) aliasCandidates(sourceFile, spec string) []string {
	cfg := idx.nearest(sourceFile)
	if cfg == nil || len(cfg.paths) == 0 {
		return nil
	}
	for _, key := range sortedAliasKeys(cfg.paths) {
		captured, ok := matchAliasKey(key, spec)
		if !ok {
			continue
		}
		out := make([]string, 0, len(cfg.paths[key]))
		for _, tgt := range cfg.paths[key] {
			out = append(out, path.Clean(path.Join(cfg.baseUrl, substituteStar(tgt, captured))))
		}
		return out
	}
	return nil
}

func (idx *tsConfigIndex) nearest(sourceFile string) *tsConfig {
	sd := path.Dir(sourceFile)
	var best *tsConfig
	bestLen := -1
	for i := range idx.configs {
		cd := idx.configs[i].dir
		if isAncestorDir(cd, sd) && len(cd) > bestLen {
			best = &idx.configs[i]
			bestLen = len(cd)
		}
	}
	return best
}

func isAncestorDir(anc, d string) bool {
	if anc == "." || anc == "" {
		return true
	}
	return d == anc || strings.HasPrefix(d, anc+"/")
}

// sortedAliasKeys orders keys most-specific-first: exact keys before wildcard
// keys, wildcards by descending prefix length (TS path-mapping precedence).
func sortedAliasKeys(paths map[string][]string) []string {
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		wi, wj := strings.Contains(keys[i], "*"), strings.Contains(keys[j], "*")
		if wi != wj {
			return !wi // exact (no '*') first
		}
		pi := strings.Index(keys[i], "*")
		pj := strings.Index(keys[j], "*")
		if wi && pi != pj {
			return pi > pj // longer prefix before '*' wins
		}
		return keys[i] < keys[j]
	})
	return keys
}

// matchAliasKey matches spec against a path-mapping key. For a wildcard key the
// captured middle is returned; for an exact key the captured value is "".
func matchAliasKey(key, spec string) (string, bool) {
	if i := strings.IndexByte(key, '*'); i >= 0 {
		prefix, suffix := key[:i], key[i+1:]
		if len(spec) >= len(prefix)+len(suffix) && strings.HasPrefix(spec, prefix) && strings.HasSuffix(spec, suffix) {
			return spec[len(prefix) : len(spec)-len(suffix)], true
		}
		return "", false
	}
	return "", spec == key
}

func substituteStar(target, captured string) string {
	if i := strings.IndexByte(target, '*'); i >= 0 {
		return target[:i] + captured + target[i+1:]
	}
	return target
}

// stripJSONC removes // and /* */ comments and trailing commas so a JSONC
// tsconfig parses with encoding/json. String contents are preserved.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inStr := false
	var strCh byte
	esc := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == strCh {
				inStr = false
			}
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, data[i]) // keep the newline
			}
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // skip past the closing '*'; loop i++ skips the '/'
			continue
		}
		if c == '"' || c == '\'' {
			inStr, strCh = true, c
		}
		out = append(out, c)
	}
	return removeTrailingCommas(out)
}

func removeTrailingCommas(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == ',' {
			j := i + 1
			for j < len(b) && (b[j] == ' ' || b[j] == '\t' || b[j] == '\n' || b[j] == '\r') {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out = append(out, b[i])
	}
	return out
}

// ── Node built-in modules (stdlib) ───────────────────────────────────────────

var nodeBuiltins = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"sys": true, "timers": true, "tls": true, "trace_events": true, "tty": true,
	"url": true, "util": true, "v8": true, "vm": true, "wasi": true,
	"worker_threads": true, "zlib": true,
}

func isNodeBuiltin(spec string) bool {
	if rest, ok := strings.CutPrefix(spec, "node:"); ok {
		_ = rest
		return true // any node: specifier is a built-in
	}
	name := spec
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		name = spec[:i]
	}
	return nodeBuiltins[name]
}

// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	ts "github.com/tree-sitter/go-tree-sitter"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonSourceFiles selects Python sources without executing repository code.
// In a Git checkout it includes tracked and unignored new files, so ignored
// runtime snapshots cannot prevent claims from binding to a committed revision.
// Non-Git directories retain the deterministic filesystem selection.
func PythonSourceFiles(ctx context.Context, root string, includeTests bool) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	listed, gitErr := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*.py").Output()
	var candidates []string
	if gitErr == nil {
		for _, rel := range strings.Split(string(listed), "\x00") {
			if rel != "" {
				candidates = append(candidates, filepath.Join(root, filepath.FromSlash(rel)))
			}
		}
	} else {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			return nil, fmt.Errorf("select Python repository sources: %w", gitErr)
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != root && (bootstrapExcludedDir(d.Name()) || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(d.Name()) == ".py" {
				candidates = append(candidates, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	var files []string
	for _, path := range candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("Python source escapes root: %s", path)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		excluded := false
		for _, dir := range parts[:len(parts)-1] {
			if bootstrapExcludedDir(dir) || strings.HasPrefix(dir, ".") {
				excluded = true
				break
			}
		}
		name := filepath.Base(path)
		if excluded || (!includeTests && isTestFile(name)) || strings.HasSuffix(name, "_pb2.py") || strings.HasSuffix(name, "_pb2_grpc.py") {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink in Python source set refused: %s", rel)
		}
		// Explicit containment, not only the traversal behaviour of git and
		// WalkDir: a symlinked PARENT directory must not carry a source out of
		// the repository.
		if err := containedSource(root, path); err != nil {
			return nil, err
		}
		if !seen[path] {
			files = append(files, path)
			seen[path] = true
		}
	}
	sort.Strings(files)
	return files, nil
}

// containedSource refuses a path whose fully resolved location is not inside
// the fully resolved repository root, whatever component (file or any parent
// directory) is a symlink.
func containedSource(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve Python source %s: %w", path, err)
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("Python source escapes root through a symlinked directory, refused: %s", path)
	}
	return nil
}

// extractPythonArchitecture observes syntax without importing or executing the
// target repository. Dynamic dispatch, ownership and enforcement remain unknown.
func extractPythonArchitecture(ctx context.Context, identity invariantRepositoryIdentity, opts invariantExtractOptions) ([]normalizedInvariantFact, int, []architecture.Limitation, error) {
	var facts []normalizedInvariantFact
	var limitations []architecture.Limitation
	files, err := PythonSourceFiles(ctx, identity.Root, opts.IncludeTests)
	if err != nil {
		if stop := extractionStopped(ctx, "during Python syntax extraction"); stop != nil {
			return nil, 0, []architecture.Limitation{*stop}, nil
		}
		return nil, 0, nil, err
	}
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tspython.Language())); err != nil {
		return nil, 0, nil, err
	}
	for _, path := range files {
		if ctx.Err() != nil {
			break
		}
		rel := invariantRel(identity.Root, path)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, len(files), nil, err
		}
		tree := parser.ParseWithOptions(func(offset int, _ ts.Point) []byte { return src[offset:] }, nil, &ts.ParseOptions{ProgressCallback: func(ts.ParseState) bool { return ctx.Err() != nil }})
		if tree == nil {
			if ctx.Err() != nil {
				break
			}
			return nil, len(files), nil, fmt.Errorf("Python parser returned no tree for %s", rel)
		}
		if tree.RootNode().HasError() {
			limitations = append(limitations, architecture.Limitation{Source: rel, Scope: rel, Reason: "Python syntax could not be parsed completely; no facts were extracted from this file", Blocking: true})
		} else {
			facts = append(facts, pythonSyntaxFacts(ctx, identity, rel, src, tree.RootNode())...)
		}
		tree.Close()
	}
	if stop := extractionStopped(ctx, "during Python syntax extraction"); stop != nil {
		limitations = append(limitations, *stop)
	}
	if len(files) > 0 {
		limitations = append(limitations, architecture.Limitation{Source: "python_ast_extractor", Scope: "repository", Reason: "Python observations are syntax-only: dynamic dispatch, aliases, descriptors, runtime mutation, call resolution, test enforcement, and architectural ownership are not established"})
	}
	return facts, len(files), limitations, nil
}

func pythonSyntaxFacts(ctx context.Context, identity invariantRepositoryIdentity, rel string, src []byte, root *ts.Node) []normalizedInvariantFact {
	var facts []normalizedInvariantFact
	text := func(n *ts.Node) string {
		if n == nil {
			return ""
		}
		return string(src[n.StartByte():n.EndByte()])
	}
	add := func(n *ts.Node, symbol, kind, predicate, object string) {
		facts = append(facts, invariantFact(identity, kind, symbol, predicate, object, rel, symbol,
			int(n.StartPosition().Row)+1, int(n.EndPosition().Row)+1, "", "python_ast_extractor", 0.55,
			map[string]string{"language": "python", "analysis": "syntax_only"}))
	}
	var visit func(*ts.Node, string, string)
	visit = func(n *ts.Node, symbol, class string) {
		if n == nil || ctx.Err() != nil {
			return
		}
		switch n.Kind() {
		case "class_definition", "function_definition":
			name := text(n.ChildByFieldName("name"))
			if symbol == rel {
				symbol += "::" + name
			} else {
				symbol += "." + name
			}
			if n.Kind() == "class_definition" {
				class = symbol
			}
			// Defaults, annotations and decorators execute in a different scope.
			visit(n.ChildByFieldName("body"), symbol, class)
			return
		case "if_statement":
			condition, body := n.ChildByFieldName("condition"), n.ChildByFieldName("consequence")
			if condition != nil && body != nil {
				for i := uint(0); i < body.NamedChildCount(); i++ {
					statement := body.NamedChild(i)
					if statement.Kind() == "comment" {
						continue
					}
					// A nested or conditional raise does not prove refusal of
					// the outer branch. Only its first direct statement qualifies.
					if statement.Kind() == "raise_statement" {
						add(n, symbol, "guard", "refuses_when", text(condition))
					}
					break
				}
			}
		case "assignment", "augmented_assignment":
			var target func(*ts.Node)
			target = func(left *ts.Node) {
				if left == nil {
					return
				}
				switch left.Kind() {
				case "attribute":
					owner := symbol
					object := text(left)
					if class != "" && (strings.HasPrefix(object, "self.") || strings.HasPrefix(object, "cls.")) {
						owner = class
					}
					add(left, symbol, "write", "writes", owner+"::"+object)
				case "pattern_list", "tuple_pattern", "list_pattern":
					for i := uint(0); i < left.NamedChildCount(); i++ {
						target(left.NamedChild(i))
					}
				}
			}
			target(n.ChildByFieldName("left"))
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), symbol, class)
		}
	}
	visit(root, rel, "")
	return facts
}

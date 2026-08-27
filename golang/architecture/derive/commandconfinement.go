// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The second registered derivation: where is authority to invoke an external
// process concentrated.
//
// Deliberately a different SPECIES of fact from the first. Lock discipline asks
// whether state access satisfies a synchronization relation; this asks who owns
// a boundary to the outside world. If both travel the same machinery —
// proposition, derivation, subjects, recipe, revalidation, anchor — then the
// architecture is general rather than tailored to mutex analysis. That is the
// whole reason for choosing it, and it is why "reach" was not the criterion:
// the layering family would have reached every file in the repository while
// establishing nothing anybody was uncertain about.
//
// # What it answers, and what it does not
//
// Answered: within the scope searched, every invocation site this derivation
// can see for the named executable lives in the named owner package.
//
// Not answered: that the owner SHOULD be the owner, that the confinement must
// be preserved, or that no invocation happens by a route this cannot see. The
// first is a judgment, the second is normative, and the third is in Limits.
//
// # Scope is part of the proposition
//
// "All invocations of git" means nothing without saying where you looked. Scope
// is therefore a term of the proposition rather than a search convenience, and
// a narrower scope is a WEAKER claim rather than a cheaper one — which keeps a
// proposer from buying an easy DERIVED by looking in one directory.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

type commandConfinement struct{}

func (commandConfinement) ID() string      { return "derive.command_invocation_confined_to" }
func (commandConfinement) Version() string { return "v1" }

func (commandConfinement) Limits() []string {
	return []string{
		"an invocation whose executable is a variable, field or parameter rather than a literal",
		"an invocation through a wrapper this derivation does not follow",
		"an invocation from a file outside the scope searched",
		"an invocation via a shell string, a script, or a build step rather than os/exec",
		"an invocation from a dependency outside the repository",
		"an invocation from a testdata/ directory, which the Go toolchain excludes from the program",
	}
}

func (commandConfinement) Applies(p Proposition) bool {
	return p.Kind == KindCommandInvocationConfinedTo &&
		strings.TrimSpace(p.Command) != "" && strings.TrimSpace(p.Owner) != "" && len(p.SearchPaths) != 0
}

func (c commandConfinement) Derive(src PinnedSource, p Proposition) Attempt {
	files, read, fset, failure := c.parseScope(src, p)
	if failure != nil {
		return *failure
	}
	if len(files) == 0 {
		return Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("no non-test Go files under %s at the pinned commit", strings.Join(p.SearchPaths, ", "))}
	}

	owner := strings.Trim(strings.TrimSpace(p.Owner), "/")
	var subjects []Subject
	var outside []string
	sites := 0

	for i, f := range files {
		filePath := read[i]
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isExecCommand(call) || len(call.Args) == 0 {
				return true
			}
			name, literal := literalExecutable(call)
			if !literal || name != p.Command {
				// A non-literal executable is invisible to this derivation, not
				// evidence of confinement. Limits() says so, and the outcome is
				// scoped to what was observable rather than asserted over what
				// was not.
				return true
			}
			sites++
			pos := fset.Position(call.Pos())
			subjects = append(subjects, Subject{
				File: filePath, Line: pos.Line,
				Entity: "exec(" + p.Command + ")", Role: "invocation-site",
			})
			if pkg := path.Dir(filePath); pkg != owner {
				outside = append(outside, fmt.Sprintf("%s:%d in %s", filePath, pos.Line, pkg))
			}
			return true
		})
	}

	if sites == 0 {
		return Attempt{Outcome: Unknown, Inputs: read, Detail: fmt.Sprintf(
			"no literal invocation of %q found under %s; nothing to establish",
			p.Command, strings.Join(p.SearchPaths, ", "))}
	}
	if len(outside) != 0 {
		sort.Strings(outside)
		return Attempt{Outcome: Refuted, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"%d of %d observable invocation(s) of %q originate outside %s: %s",
			len(outside), sites, p.Command, owner, strings.Join(outside, "; "))}
	}
	return Attempt{Outcome: Derived, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
		"all %d observable invocation(s) of %q under %s originate from %s, across %d subject file(s) "+
			"(%d file(s) were read to compute it)",
		sites, p.Command, strings.Join(p.SearchPaths, ", "), owner, len(subjectFiles(subjects)), len(read))}
}

// parseScope reads every non-test Go file under the proposition's scope.
func (commandConfinement) parseScope(src PinnedSource, p Proposition) ([]*ast.File, []string, *token.FileSet, *Attempt) {
	fset := token.NewFileSet()
	var files []*ast.File
	var read []string
	for _, dir := range p.SearchPaths {
		paths, err := src.ListRecursive(strings.Trim(strings.TrimSpace(dir), "/"))
		if err != nil {
			return nil, read, fset, &Attempt{Outcome: Unknown, Inputs: read,
				Detail: fmt.Sprintf("cannot list %s at the pinned commit: %v", dir, err)}
		}
		for _, path := range paths {
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || inTestdata(path) {
				continue
			}
			b, err := src.Read(path)
			if err != nil {
				return nil, read, fset, &Attempt{Outcome: Unknown, Inputs: read,
					Detail: fmt.Sprintf("cannot read %s: %v", path, err)}
			}
			f, err := parser.ParseFile(fset, path, b, 0)
			if err != nil {
				return nil, read, fset, &Attempt{Outcome: Unknown, Inputs: read,
					Detail: fmt.Sprintf("cannot parse %s: %v", path, err)}
			}
			files = append(files, f)
			read = append(read, path)
		}
	}
	return files, read, fset, nil
}

// inTestdata reports whether a path lies under a testdata/ directory.
//
// Found by running this derivation against a real repository, where it parsed
// cmd/principle-check/rules/testdata/positive/*/bad.go -- files that exist
// precisely to demonstrate violations. The Go toolchain ignores testdata/, so
// those files are not part of the program, and a fixture built to be wrong
// would have refuted a confinement that actually holds.
//
// Skipping them narrows what is read, so it is declared in Limits() rather than
// applied silently: a reader must be able to see that this claim was not made
// over test fixtures.
func inTestdata(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "testdata" {
			return true
		}
	}
	return false
}

// isExecCommand reports whether a call is os/exec.Command or CommandContext.
func isExecCommand(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return false
	}
	return sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext"
}

// literalExecutable returns the executable name when it is a string literal.
//
// Only a literal counts. A variable executable is something this derivation
// cannot see, and treating it as "not the command in question" would let an
// invocation hide behind a local — so it is reported as unobservable in Limits
// rather than silently assumed harmless.
func literalExecutable(call *ast.CallExpr) (string, bool) {
	arg := call.Args[0]
	if _, isCtx := call.Fun.(*ast.SelectorExpr); isCtx && len(call.Args) > 1 {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "CommandContext" {
			arg = call.Args[1]
		}
	}
	lit, ok := arg.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

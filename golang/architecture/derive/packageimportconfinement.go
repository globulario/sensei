// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The fifth registered derivation: is a package isolated from the production
// dependency graph outside its allowed owner.
//
// # Why a fifth family
//
// The four earlier families are about state (a field under a lock, a field
// written or minted only by its owner) or about an external process boundary.
// Each needs a struct field or a literal executable to be ABOUT. A package
// whose architecture is expressed through functions -- an adapter, a converter,
// a decoder -- offers neither, so no truthful proposition could be stated over
// it and every file in it stayed ungoverned. The measured specimen was a
// function-only adapter whose whole architectural boundary was that the
// governed program never reaches it: a fact real enough that a test enforced
// it, and one no registered family could say.
//
// The boundary of a function-owned seam is its import edge. "No production
// source outside the owner imports this package" is a property of the source,
// not of a name or a convention, and it is falsifiable by a single added
// import.
//
// # What it answers, and what it does not
//
// Answered: within the scope searched, every observable non-test import of the
// package declared by Dir originates from Dir itself or from the named Owner
// directory. With no Owner, that no non-test source outside Dir imports it.
//
// Not answered: that the isolation SHOULD hold, that no code reaches the
// package by a route this cannot see (reflection, plugins, a dependency
// outside the repository, code generation), or anything about who calls which
// function once a package is imported -- that is a different, narrower
// proposition.
//
// # Zero importers is the fact, not vacuity
//
// The other families refuse to derive over zero sites because a confinement of
// nothing constrains nothing. This family is different in kind: the
// proposition is an isolation, and an empty set of outside import edges is
// exactly the arrangement it states. What it will not do is derive over a
// package it could not identify. A DERIVED here requires that Dir declares one
// Go package in at least one non-test file, that the package's import path is
// bound from the pinned go.mod, and that every non-test source in the searched
// scope was read. The subjects are the package's own files, which exist and
// were read whether or not anything imports them.
//
// # Tests are not the production graph
//
// *_test.go and testdata/ are excluded exactly as in every other family. A test
// that imports the package from outside is a test dependency, not evidence that
// the production architecture reaches it, so it neither refutes the isolation
// nor counts as an import site.

import (
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

type packageImportConfinement struct{}

func (packageImportConfinement) ID() string      { return "derive.package_import_confined_to" }
func (packageImportConfinement) Version() string { return "v1" }

func (packageImportConfinement) Limits() []string {
	return []string{
		"an import from a *_test.go file, which is a test dependency and not part of the production dependency graph this proposition is about",
		"an import from a testdata/ directory, which the Go toolchain excludes from the program",
		"an import from a file outside the scope searched, or from a dependency outside the repository",
		"a file excluded from every build by a build constraint, which is still read and still counted: this does not evaluate build tags",
		"a route to the package that is not an import: reflection, plugins, generated code not present at the pinned commit, or a vendored copy under another import path",
		"which functions of the package an allowed importer calls, which is a different and narrower proposition",
	}
}

func (packageImportConfinement) Applies(p Proposition) bool {
	return p.Kind == KindPackageImportConfinedTo &&
		strings.TrimSpace(p.Dir) != "" && len(p.SearchPaths) != 0
}

func (packageImportConfinement) Derive(src PinnedSource, p Proposition) Attempt {
	files, read, fset, failure := parseScope(src, p.SearchPaths)
	if failure != nil {
		return *failure
	}
	dir := cleanDir(p.Dir)
	owner := ""
	if strings.TrimSpace(p.Owner) != "" {
		owner = cleanDir(p.Owner)
	}

	// The package's own non-test files are read directly, whether or not Dir lies
	// inside the scope searched: they are what the proposition is ABOUT, and
	// whether the package exists is decided here, not inferred from its importers.
	pkgFiles, pkgName, pkgRead, failure := readPackage(src, dir, read)
	read = pkgRead
	if failure != nil {
		return *failure
	}

	modulePath, modRead := modulePathOf(src)
	if modRead {
		read = append(read, "go.mod")
	}
	if modulePath == "" {
		return Attempt{Outcome: Unresolved, Inputs: read, Detail: fmt.Sprintf(
			"the import path of %s cannot be bound: the pinned tree has no readable module path in go.mod", dir)}
	}
	// A go.mod between the root and Dir starts a different module, whose import
	// paths the root module path does not describe. Binding Dir under the root
	// module would then look for imports of a path nothing uses, and find none.
	if nested := nestedModule(src, dir); nested != "" {
		return Attempt{Outcome: Unresolved, Inputs: append(read, nested), Detail: fmt.Sprintf(
			"the import path of %s cannot be bound from the root go.mod: %s starts a nested module", dir, nested)}
	}
	importPath := modulePath
	if dir != "." {
		importPath = modulePath + "/" + dir
	}

	var subjects []Subject
	for _, pf := range pkgFiles {
		subjects = append(subjects, Subject{File: pf, Entity: "package " + pkgName, Role: "package-file"})
	}

	var outside []string
	allowed := 0
	for i, f := range files {
		filePath := read[i]
		importer := cleanDir(path.Dir(filePath))
		for _, spec := range f.Imports {
			got, err := strconv.Unquote(spec.Path.Value)
			if err != nil || got != importPath {
				continue
			}
			if importer == dir || (owner != "" && importer == owner) {
				allowed++
				continue
			}
			line := fset.Position(spec.Pos()).Line
			outside = append(outside, fmt.Sprintf("%s:%d", filePath, line))
			subjects = append(subjects, Subject{File: filePath, Line: line, Entity: "import " + importPath, Role: "import-site"})
		}
	}

	allowedText := "outside " + dir
	if owner != "" {
		allowedText = "outside " + dir + " and " + owner
	}
	if len(outside) != 0 {
		sort.Strings(outside)
		return Attempt{Outcome: Refuted, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
			"%d non-test import(s) of %s originate %s: %s",
			len(outside), importPath, allowedText, strings.Join(outside, "; "))}
	}
	return Attempt{Outcome: Derived, Inputs: read, Subjects: subjects, Detail: fmt.Sprintf(
		"no non-test source %s under %s imports %s (%d allowed import(s); %d non-test file(s) read; "+
			"the package is declared in %d file(s))",
		allowedText, strings.Join(p.SearchPaths, ", "), importPath, allowed, len(files), len(pkgFiles))}
}

// readPackage reads the non-test Go files directly in dir and requires that
// they declare exactly one package.
//
// read is the scope already read; a package file found there is not read
// twice, and one outside it is appended so the receipt names every input.
func readPackage(src PinnedSource, dir string, read []string) ([]string, string, []string, *Attempt) {
	listed, err := src.List(dir)
	if err != nil {
		return nil, "", read, &Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("cannot list %s at the pinned commit: %v", dir, err)}
	}
	seen := map[string]bool{}
	for _, r := range read {
		seen[r] = true
	}
	fset := token.NewFileSet()
	var files []string
	names := map[string]bool{}
	sort.Strings(listed)
	for _, p := range listed {
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || inTestdata(p) {
			continue
		}
		b, err := src.Read(p)
		if err != nil {
			return nil, "", read, &Attempt{Outcome: Unknown, Inputs: read,
				Detail: fmt.Sprintf("cannot read %s: %v", p, err)}
		}
		f, err := parser.ParseFile(fset, p, b, parser.PackageClauseOnly)
		if err != nil {
			return nil, "", read, &Attempt{Outcome: Unknown, Inputs: read,
				Detail: fmt.Sprintf("cannot parse %s: %v", p, err)}
		}
		if !seen[p] {
			read = append(read, p)
			seen[p] = true
		}
		files = append(files, p)
		names[f.Name.Name] = true
	}
	if len(files) == 0 {
		return nil, "", read, &Attempt{Outcome: Unknown, Inputs: read,
			Detail: fmt.Sprintf("no non-test Go files in %s at the pinned commit; no package to establish anything about", dir)}
	}
	if len(names) != 1 {
		var list []string
		for n := range names {
			list = append(list, n)
		}
		sort.Strings(list)
		return nil, "", read, &Attempt{Outcome: Unresolved, Inputs: read,
			Detail: fmt.Sprintf("%s declares more than one package (%s); which one the import path names cannot be bound",
				dir, strings.Join(list, ", "))}
	}
	var name string
	for n := range names {
		name = n
	}
	return files, name, read, nil
}

// nestedModule returns the path of a go.mod in dir or in any directory between
// the repository root and dir, or "" when the root module governs dir.
func nestedModule(src PinnedSource, dir string) string {
	if dir == "." {
		return ""
	}
	parts := strings.Split(dir, "/")
	for i := len(parts); i > 0; i-- {
		candidate := strings.Join(parts[:i], "/") + "/go.mod"
		if _, err := src.Read(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

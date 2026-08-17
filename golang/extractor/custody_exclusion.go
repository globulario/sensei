// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/packcustody"
)

// custodyExclusion decides whether a repository-scoped publication must leave
// this document out of its slice, and returns the typed report entry saying so.
//
// The decision is delegated entirely to packcustody, which derives it from the
// project's governed provenance records and the document's content digest. This
// function deliberately contains no rule of its own — in particular no rule
// about the document's name or directory. "The file is called
// meta_principles.yaml" and "the file is inside this repository's corpus" are
// the two inferences that produced the two-authoring-domain defect, and either
// one reintroduced here would defeat the whole exercise while looking like a
// harmless shortcut.
//
// An unreadable file is NOT excluded: it falls through to classifyAndImport,
// which already reports read failures as StatusInvalid. Reporting the same
// failure twice in two vocabularies would give an operator two different names
// for one problem.
func custodyExclusion(custodyRoot, path string) (FileReport, bool) {
	if custodyRoot == "" {
		return FileReport{}, false // no custody authority supplied; historical behaviour
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return FileReport{}, false
	}
	verdict := packcustody.Derive(custodyRoot, content)
	if !verdict.Excluded() {
		return FileReport{}, false
	}
	status := StatusSharedCustody
	if verdict.Custody == packcustody.Refused {
		status = StatusCustodyRefused
	}
	return FileReport{
		Path:   path,
		Status: status,
		Reason: verdict.Describe(),
	}, true
}

// CustodyExcluded returns the files a custody derivation kept out of this
// publication. Callers surface it to the operator: an exclusion nobody is told
// about is indistinguishable from knowledge silently going missing, which is
// the failure this whole mechanism was built to make impossible.
func (r *ImportReport) CustodyExcluded() []FileReport {
	var out []FileReport
	for _, f := range r.Files {
		if f.Status == StatusSharedCustody || f.Status == StatusCustodyRefused {
			out = append(out, f)
		}
	}
	return out
}

// HasCustodyRefusal reports whether any document's author could not be
// established. Distinct from CustodyExcluded: a shared projection has a known
// owner and needs no action, whereas a refusal is unresolved and does.
func (r *ImportReport) HasCustodyRefusal() bool {
	for _, f := range r.Files {
		if f.Status == StatusCustodyRefused {
			return true
		}
	}
	return false
}

// FormatCustodyExclusions renders the exclusions for build output.
func (r *ImportReport) FormatCustodyExclusions() string {
	excluded := r.CustodyExcluded()
	if len(excluded) == 0 {
		return ""
	}
	out := ""
	for _, f := range excluded {
		out += fmt.Sprintf("  custody: %s not published by this repository — %s\n", f.Path, f.Reason)
	}
	return out
}

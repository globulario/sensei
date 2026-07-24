// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "os"

// AwarenessDir is the repo-relative root of authored governed sources.
const AwarenessDir = "docs/awareness"

// generatedDir and candidatesDir are excluded from unconditional governed
// protection: their contents are machine-derived, never authored authority,
// no matter where they sit under AwarenessDir (contract §3.2, §12 "generated
// and candidates are not mistaken for authored authority merely by
// location").
const (
	generatedDir  = AwarenessDir + "/generated"
	candidatesDir = AwarenessDir + "/candidates"
)

// GovernedSourceFiles returns every authored file directly under
// docs/awareness/ (recursively), EXCLUDING docs/awareness/generated/ and
// docs/awareness/candidates/. These are unconditionally protected: they are
// Sensei's own governed authority surface (contract §3.2).
func GovernedSourceFiles(repoRoot string) ([]string, error) {
	all, err := walkFiles(repoRoot, joinRepo(repoRoot, AwarenessDir))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, f := range all {
		if isUnderAny(f, generatedDir, candidatesDir) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// GovernedSourceReasons builds the unconditional-protection reasons for
// every file GovernedSourceFiles returns, plus the manual registry file
// itself (which is always protected regardless of its own content —
// contract §3.2 lists it explicitly).
func GovernedSourceReasons(repoRoot string) (map[string][]ProtectionReason, error) {
	files, err := GovernedSourceFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]ProtectionReason, len(files)+1)
	addGovernedReason := func(path string) {
		out[path] = append(out[path], ProtectionReason{
			Origin: OriginGovernedSource,
			Kind:   "authored_awareness_source",
			Source: path,
		})
	}
	for _, f := range files {
		addGovernedReason(f)
	}
	// The manual registry is unconditionally protected regardless of
	// whether it currently lists anything — editing it IS an architectural
	// decision about what gets protected. This only applies once the file
	// actually EXISTS; a repository where it was never scaffolded has no
	// protected path to speak of yet (that repo should read as EMPTY, not
	// acquire a phantom protected path for a file that is not there).
	if _, present := out[ManualRegistryFile]; !present {
		if _, statErr := os.Stat(joinRepo(repoRoot, ManualRegistryFile)); statErr == nil {
			out[ManualRegistryFile] = append(out[ManualRegistryFile], ProtectionReason{
				Origin: OriginGovernedSource,
				Kind:   "manual_registry_definition",
				Source: ManualRegistryFile,
			})
		}
	}
	return out, nil
}

// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture/binding"
)

const ProjectorID = "ledger.projection.v1"

type ProjectionSet struct {
	Files map[string][]byte
}

type ProjectionManifest struct {
	SchemaVersion          string                   `json:"schema_version" yaml:"schema_version"`
	TaskID                 string                   `json:"task_id" yaml:"task_id"`
	LedgerHeadDigestSHA256 string                   `json:"ledger_head_digest_sha256" yaml:"ledger_head_digest_sha256"`
	Files                  []ProjectionManifestFile `json:"files" yaml:"files"`
	ProjectorID            string                   `json:"projector_id" yaml:"projector_id"`
}

type ProjectionManifestFile struct {
	Path         string `json:"path" yaml:"path"`
	DigestSHA256 string `json:"digest_sha256" yaml:"digest_sha256"`
}

func Project(chain VerifiedChain) (ProjectionSet, error) {
	files := map[string][]byte{}
	for _, entry := range chain.Entries {
		data, err := os.ReadFile(entry.PayloadPath)
		if err != nil {
			return ProjectionSet{}, err
		}
		payload, err := ParseTaskEventPayload(data)
		if err != nil {
			continue
		}
		if err := applyProjectionArtifacts(chain.TaskDir, files, payload); err != nil {
			return ProjectionSet{}, err
		}
	}
	manifest, err := buildProjectionManifest(chain.TaskID, chain.Head.EntryDigestSHA256, files)
	if err != nil {
		return ProjectionSet{}, err
	}
	files["projections/manifest.yaml"] = manifest
	return ProjectionSet{Files: files}, nil
}

func RebuildProjections(taskDir string, validator PayloadValidator) (ProjectionSet, error) {
	chain, err := loadVerifiedChain(taskDir, validator)
	if err != nil {
		return ProjectionSet{}, err
	}
	set, err := Project(chain)
	if err != nil {
		return ProjectionSet{}, err
	}
	for path, data := range set.Files {
		if err := writeFileAtomic(filepath.Join(taskDir, filepath.FromSlash(path)), data); err != nil {
			return ProjectionSet{}, err
		}
	}
	return set, nil
}

func ProjectionState(taskDir string, set ProjectionSet) string {
	if len(set.Files) == 0 {
		return "absent"
	}
	for path, want := range set.Files {
		got, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(path)))
		if err != nil || string(got) != string(want) {
			return "projection_drift"
		}
	}
	return "current"
}

func applyProjectionArtifacts(taskDir string, files map[string][]byte, payload TaskEventPayload) error {
	session, ok := payload.Artifacts["session"]
	if ok {
		data, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(session.Path)))
		if err != nil {
			return err
		}
		files["projections/session.yaml"] = data
		files["session.yaml"] = data
	}
	taskControl, ok := payload.Artifacts["task_control"]
	if ok {
		data, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(taskControl.Path)))
		if err != nil {
			return err
		}
		files["projections/task-control.yaml"] = data
		files["control/latest.yaml"] = data
	}
	status, ok := payload.Artifacts["status"]
	if ok {
		data, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(status.Path)))
		if err != nil {
			return err
		}
		files["projections/status.yaml"] = data
		files["receipts/task-status.yaml"] = data
	}
	return nil
}

func buildProjectionManifest(taskID, headDigest string, files map[string][]byte) ([]byte, error) {
	manifest := ProjectionManifest{
		SchemaVersion:          "1",
		TaskID:                 taskID,
		LedgerHeadDigestSHA256: headDigest,
		ProjectorID:            ProjectorID,
	}
	for path, data := range files {
		if path == "projections/manifest.yaml" {
			continue
		}
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, ProjectionManifestFile{
			Path:         filepath.ToSlash(path),
			DigestSHA256: hex.EncodeToString(sum[:]),
		})
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return binding.CanonicalYAML(manifest)
}

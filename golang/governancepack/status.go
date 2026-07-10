// SPDX-License-Identifier: Apache-2.0

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	StateNotDetected = "not_detected"
	StateNone        = "none"
	StateUnverified  = "unverified"
	StateCurrent     = "current"
	StateStale       = "stale"
	StateUnknown     = "unknown"
)

type LocalStatus struct {
	State             string
	Detail            string
	Active            *ActiveRecord
	VerifiedPack      *VerifiedPack
	FetchedState      string
	FetchedDetail     string
	LatestFetched     *FetchedRecord
	StagedTrustState  string
	StagedTrustDetail string
	StagedTrust       *StagedTrustRecord
	CombinedGraph     seedmeta.Marker
	TrustStorePath    string
}

func ManagedModeEnabled(root string) bool {
	_, err := os.Stat(TrustedKeysPath(root))
	return err == nil
}

func AssessLocalStatus(root, currentVersion string) LocalStatus {
	dir := GovernanceDirPath(root)
	if _, err := os.Stat(dir); err != nil {
		return LocalStatus{State: StateNotDetected}
	}
	status := LocalStatus{
		FetchedState:     StateNone,
		StagedTrustState: StateNone,
		TrustStorePath:   TrustedKeysPath(root),
	}
	stagedPath := StagedTrustStorePath(root)
	if _, err := os.Stat(stagedPath); err == nil {
		record, recErr := ReadStagedTrustRecord(StagedTrustRecordPath(root))
		if recErr == nil {
			status.StagedTrust = &record
		}
		store, loadErr := LoadTrustStore(stagedPath)
		switch {
		case loadErr != nil:
			status.StagedTrustState = StateStale
			status.StagedTrustDetail = loadErr.Error()
		case recErr != nil:
			status.StagedTrustState = StateUnknown
			status.StagedTrustDetail = recErr.Error()
		default:
			status.StagedTrustState = StateCurrent
			status.StagedTrustDetail = "staged trust root validates locally"
			if activeBytes, err := os.ReadFile(TrustedKeysPath(root)); err == nil {
				var activeStore TrustStore
				if json.Unmarshal(activeBytes, &activeStore) == nil && activeStore.Validate() == nil {
					activeNorm, _ := json.Marshal(activeStore)
					stagedNorm, _ := json.Marshal(store)
					if string(activeNorm) == string(stagedNorm) {
						status.StagedTrustDetail = "staged trust root matches active trust store"
					} else {
						status.StagedTrustDetail = "staged trust root differs from active trust store"
					}
				}
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		status.StagedTrustState = StateUnknown
		status.StagedTrustDetail = err.Error()
	}
	records, err := ListFetchedRecords(root)
	if err != nil {
		status.FetchedState = StateUnknown
		status.FetchedDetail = err.Error()
	} else if len(records) > 0 {
		status.LatestFetched = &records[0]
		manifestPath := records[0].ManifestPath
		if manifestPath != "" && !filepath.IsAbs(manifestPath) {
			manifestPath = filepath.Join(root, filepath.FromSlash(manifestPath))
		}
		if !ManagedModeEnabled(root) {
			status.FetchedState = StateUnverified
			status.FetchedDetail = "trusted publisher key set missing"
		} else {
			verified, err := VerifyPack(manifestPath, status.TrustStorePath, currentVersion)
			if err != nil {
				status.FetchedState = StateStale
				status.FetchedDetail = err.Error()
			} else if verified.PayloadMarker.Digest != records[0].PayloadDigestSHA256 || verified.PayloadMarker.TripleCount != records[0].PayloadTripleCount || verified.PayloadMarker.IRI != records[0].PayloadMarkerIRI {
				status.FetchedState = StateStale
				status.FetchedDetail = "fetched record does not match fetched pack payload"
			} else {
				status.FetchedState = StateCurrent
				status.FetchedDetail = "latest fetched governance pack verifies locally"
			}
		}
	}
	activePath := ActiveRecordPath(root)
	active, err := ReadActiveRecord(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			status.State = StateNone
			return status
		}
		status.State = StateUnknown
		status.Detail = err.Error()
		return status
	}
	status.State = StateUnverified
	status.Active = &active
	if !ManagedModeEnabled(root) {
		status.Detail = "trusted publisher key set missing"
		return status
	}
	manifestPath := active.ManifestPath
	if manifestPath != "" && !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(root, filepath.FromSlash(manifestPath))
	}
	verified, err := VerifyPack(manifestPath, status.TrustStorePath, currentVersion)
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	status.VerifiedPack = &verified
	marker, err := seedmeta.ReadMarkerFile(seedmeta.RuntimeMarkerPath(root))
	if err != nil {
		if !os.IsNotExist(err) {
			status.Detail = fmt.Sprintf("read graph authority: %v", err)
		}
		return status
	}
	status.CombinedGraph = marker
	if strings.TrimSpace(active.CombinedGraphDigestSHA256) == "" || active.CombinedGraphTripleCount <= 0 {
		status.Detail = "active governance record missing combined graph identity"
		return status
	}
	if active.CombinedGraphDigestSHA256 != marker.Digest || active.CombinedGraphTripleCount != marker.TripleCount {
		status.State = StateStale
		status.Detail = "active governance record does not match graph authority marker"
		return status
	}
	status.State = StateCurrent
	status.Detail = "active governance record matches local graph authority marker"
	return status
}

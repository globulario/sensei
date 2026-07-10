// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import "path/filepath"

const StagedTrustStoreRelativePath = ".awg/governance/incoming/trusted-publishers.json"

func StagedTrustStorePath(root string) string {
	if root == "" {
		return StagedTrustStoreRelativePath
	}
	return filepath.Join(root, StagedTrustStoreRelativePath)
}

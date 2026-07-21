// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import "github.com/globulario/sensei/golang/statedir"

func StagedTrustStorePath(root string) string {
	return statedir.Path(root, "governance", "incoming", "trusted-publishers.json")
}

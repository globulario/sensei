// SPDX-License-Identifier: Apache-2.0

package governancepack

import "github.com/globulario/sensei/golang/statedir"

func StagedTrustStorePath(root string) string {
	return statedir.Path(root, "governance", "incoming", "trusted-publishers.json")
}

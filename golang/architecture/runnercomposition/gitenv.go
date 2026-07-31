// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "os"

// gitIsolatedEnv returns a from-scratch environment for invoking git: no
// inherited GIT_* variables, HOME/XDG_CONFIG_HOME pointed at isolatedHome
// (a fresh, empty directory the caller creates and owns), and
// GIT_CONFIG_SYSTEM/GIT_CONFIG_GLOBAL explicitly pointed at /dev/null
// (overriding even an ambient GIT_CONFIG_GLOBAL the calling process's own
// environment might already have set). Every git invocation in this
// package uses this, so the configuration/attribute independence
// GitChangeDigest's doc comment explains is enforced identically
// everywhere, not just for change-digest computation.
func gitIsolatedEnv(isolatedHome string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + isolatedHome,
		"XDG_CONFIG_HOME=" + isolatedHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
	}
}

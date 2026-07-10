// Positive-control fixture for circular_dependency_break_glass_declarative.
// exec.Command("go", "build", ...) in production code (hot-deploy anti-pattern).
package badfix

import "os/exec"

func hotDeploy() error {
	cmd := exec.Command("go", "build", "-o", "/usr/lib/globular/bin/svc", "./svc") // BAD
	return cmd.Run()
}

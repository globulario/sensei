// Positive-control fixture for competing_writers_singleflight_restart.
// exec.Command(... "systemctl" ... "restart" ...) in a convergence path.
package badfix

import "os/exec"

func reconcileRestart(unit string) error {
	cmd := exec.Command("sudo", "systemctl", "--no-block", "restart", unit) // BAD
	return cmd.Run()
}

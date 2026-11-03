//go:build !linux

package codexfrontend

import "os/exec"

// Adaptive normal-shutdown certification is Linux-only. Other platforms retain
// the native wait contract; no unverified process-group ownership is inferred.
func waitOwnedFrontend(command *exec.Cmd, retireSignals func()) error {
	err := command.Wait()
	retireSignals()
	return err
}

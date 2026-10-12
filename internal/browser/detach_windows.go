//go:build windows

package browser

import "os/exec"

// Detach is a no-op on Windows; Chrome detaches itself.
func Detach(cmd *exec.Cmd) {}

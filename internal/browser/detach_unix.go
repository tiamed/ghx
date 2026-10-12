//go:build !windows

package browser

import (
	"os/exec"
	"syscall"
)

// Detach starts the browser in its own session so ghx can exit.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

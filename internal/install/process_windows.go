//go:build windows

package install

import "syscall"

func windowsProcessExists(pid uint32) bool {
	const processQueryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

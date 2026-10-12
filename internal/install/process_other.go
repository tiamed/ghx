//go:build !windows

package install

func windowsProcessExists(uint32) bool { return false }

//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"fmt"
	"os/exec"
)

// lookupBSDSystemCommand supports native invocations and service managers
// whose PATH omits /sbin or /usr/sbin, which is common on macOS.
func lookupBSDSystemCommand(name string, fallbackPaths ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	for _, fallback := range fallbackPaths {
		if path, fallbackErr := exec.LookPath(fallback); fallbackErr == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("find %s command: %w", name, err)
}

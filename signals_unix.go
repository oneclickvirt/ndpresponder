//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
)

func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

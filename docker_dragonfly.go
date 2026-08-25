//go:build dragonfly

package main

import (
	"context"
	"fmt"
)

// The Docker client dependency currently omits DragonFly from its BSD terminal
// implementation. Keeping that dependency out of DragonFly builds preserves
// the native static and CNI NDP proxy modes instead of making the entire
// binary unavailable.
var (
	dockerLogger   = logger.Named("Docker")
	dockerNetworks []string
)

func dockerListen(context.Context) error {
	return fmt.Errorf("Docker-compatible API mode is unavailable on DragonFly BSD; use static /128 targets or CNI host-local leases")
}

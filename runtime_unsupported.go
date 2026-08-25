//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package main

import (
	"context"
	"fmt"
)

func runResponder(context.Context) error {
	return fmt.Errorf("ndpresponder supports packet responses on Linux and proxy NDP entries on macOS or supported BSD hosts")
}

//go:build linux

package main

import (
	"net/netip"

	"go.uber.org/zap"
)

var newActiveIP = make(chan netip.Addr, 64)

func publishNewActiveIP(address netip.Addr) {
	select {
	case newActiveIP <- address:
	default:
		dockerLogger.Warn("new IPv6 address announcement queue is full", zap.Stringer("ip", address))
	}
}

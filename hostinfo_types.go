package main

import (
	"net"
	"net/netip"
)

// HostInfo contains address information used to construct NDP frames.
type HostInfo struct {
	HostMAC   net.HardwareAddr
	GatewayIP netip.Addr
}

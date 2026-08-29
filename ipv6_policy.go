package main

import "net/netip"

// runtimeAddressNeedsAnnouncement limits runtime addresses to globally
// routed unicast space. Container runtimes commonly expose an internal ULA or
// link-local address alongside a public address; proxying those internal
// addresses on the host uplink can create an unintended upstream neighbor
// entry and is unnecessary for NAT66 networks.
func runtimeAddressNeedsAnnouncement(ip netip.Addr) bool {
	return ip.IsValid() && ip.Is6() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast()
}

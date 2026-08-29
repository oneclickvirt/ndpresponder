//go:build linux

package main

import (
	"net"
	"net/netip"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestDefaultGatewayChoosesLowestPriorityIPv6Default(t *testing.T) {
	defaultRoute := &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	routes := []netlink.Route{
		{Dst: defaultRoute, Gw: net.ParseIP("fe80::2"), Priority: 1024},
		{Dst: defaultRoute, Gw: net.ParseIP("fe80::1"), Priority: 100},
		{Dst: &net.IPNet{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(64, 128)}, Gw: net.ParseIP("fe80::9"), Priority: 1},
	}

	got := defaultGateway(routes)
	want := netip.MustParseAddr("fe80::1")
	if got != want {
		t.Fatalf("defaultGateway() = %s, want %s", got, want)
	}
}

func TestDefaultGatewayIgnoresOnLinkAndIPv4Routes(t *testing.T) {
	routes := []netlink.Route{
		{Dst: &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}},
		{Dst: &net.IPNet{IP: net.ParseIP("0.0.0.0"), Mask: net.CIDRMask(0, 32)}, Gw: net.ParseIP("192.0.2.1")},
	}
	if got := defaultGateway(routes); got.IsValid() {
		t.Fatalf("defaultGateway() = %s, want invalid", got)
	}
}

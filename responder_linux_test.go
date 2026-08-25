//go:build linux

package main

import (
	"net"
	"net/netip"
	"testing"
)

func TestChooseResponderRuntimePrefersIPv6Gateway(t *testing.T) {
	withoutGateway := &responderRuntime{netif: &net.Interface{Name: "dummy0"}}
	withGateway := &responderRuntime{
		netif: &net.Interface{Name: "uplink0"},
		hi:    HostInfo{GatewayIP: netip.MustParseAddr("2001:db8::1")},
	}

	selected := chooseResponderRuntime([]*responderRuntime{withoutGateway, withGateway})
	if selected != withGateway {
		t.Fatalf("selected %s, want uplink0", selected.netif.Name)
	}
}

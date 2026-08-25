//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestBSDProxyUsesNDPSetAndDeleteCommands(t *testing.T) {
	target := netip.MustParseAddr("2001:db8::10")
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x10}
	state := 0
	var calls [][]string
	responder := &bsdProxyResponder{
		netif: &net.Interface{Name: "en0", HardwareAddr: mac},
		path:  "ndp",
		route: func(netip.Addr) (string, error) { return "en0", nil },
		owned: make(map[netip.Addr]struct{}),
		run: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			switch args[0] {
			case "-an":
				if state == 1 {
					return []byte(testNDPProxyOutput(target, "en0")), nil
				}
				return []byte("Neighbor Linklayer Address Netif Expire St Flgs Prbs\n"), nil
			case "-s":
				state = 1
				return nil, nil
			case "-d":
				state = 0
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected arguments %v", args)
			}
		},
	}

	if err := responder.ensure(target); err != nil {
		t.Fatalf("ensure() error = %v", err)
	}
	if err := responder.remove(target); err != nil {
		t.Fatalf("remove() error = %v", err)
	}

	want := [][]string{
		{"ndp", "-an"},
		{"ndp", "-s", "2001:db8::10", "02:00:00:00:00:10", "proxy"},
		{"ndp", "-an"},
		{"ndp", "-an"},
		{"ndp", "-d", "2001:db8::10"},
		{"ndp", "-an"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %v, want %v", calls, want)
	}
}

func TestBSDProxyRejectsOrdinaryNeighborWithoutOverwritingIt(t *testing.T) {
	target := netip.MustParseAddr("2001:db8::10")
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x10}
	calledSet := false
	responder := &bsdProxyResponder{
		netif: &net.Interface{Name: "en0", HardwareAddr: mac},
		path:  "ndp",
		route: func(netip.Addr) (string, error) { return "en0", nil },
		owned: make(map[netip.Addr]struct{}),
		run: func(_ string, args ...string) ([]byte, error) {
			if reflect.DeepEqual(args, []string{"-an"}) {
				return []byte("Neighbor Linklayer Address Netif Expire St Flgs Prbs\n2001:db8::10 02:00:00:00:00:10 en0 permanent S\n"), nil
			}
			if len(args) > 0 && args[0] == "-s" {
				calledSet = true
			}
			return nil, fmt.Errorf("unexpected command %v", args)
		},
	}

	err := responder.ensure(target)
	if err == nil || !strings.Contains(err.Error(), "ordinary neighbor") {
		t.Fatalf("ensure() error = %v, want ordinary-neighbor conflict", err)
	}
	if calledSet {
		t.Fatal("ensure() attempted to overwrite an ordinary neighbor entry")
	}
}

func TestBSDProxyRejectsTargetRoutedThroughAnotherInterfaceBeforeCreatingEntry(t *testing.T) {
	target := netip.MustParseAddr("2001:db8::10")
	calledSet := false
	responder := &bsdProxyResponder{
		netif: &net.Interface{
			Name:         "en0",
			HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x10},
		},
		path:  "ndp",
		route: func(netip.Addr) (string, error) { return "bridge100", nil },
		owned: make(map[netip.Addr]struct{}),
		run: func(_ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "-s" {
				calledSet = true
			}
			if reflect.DeepEqual(args, []string{"-an"}) {
				return []byte("Neighbor Linklayer Address Netif Expire St Flgs Prbs\n"), nil
			}
			return nil, fmt.Errorf("unexpected command %v", args)
		},
	}

	err := responder.ensure(target)
	if err == nil || !strings.Contains(err.Error(), "routes through bridge100") {
		t.Fatalf("ensure() error = %v, want route mismatch", err)
	}
	if calledSet {
		t.Fatal("ensure() created an NDP entry despite a route mismatch")
	}
}

func TestBSDProxyChecksEveryInitialTargetRoute(t *testing.T) {
	first := netip.MustParseAddr("2001:db8::10")
	second := netip.MustParseAddr("2001:db8::11")
	responder := &bsdProxyResponder{
		netif: &net.Interface{Name: "en0"},
		route: func(target netip.Addr) (string, error) {
			if target == first {
				return "en0", nil
			}
			return "bridge100", nil
		},
	}

	err := responder.checkTargetRoutes([]netip.Addr{first, second, first})
	if err == nil || !strings.Contains(err.Error(), second.String()) || !strings.Contains(err.Error(), "bridge100") {
		t.Fatalf("checkTargetRoutes() error = %v, want second target route mismatch", err)
	}
}

func TestBSDNDPProxyUplinkRejectsVirtualInterfaces(t *testing.T) {
	for _, ifname := range []string{"bridge100", "docker0", "utun4", "vmnet8", "veth1234", "epair0a", "vether0", "gif0", "wg0"} {
		if isBSDNDPProxyUplink(ifname) {
			t.Fatalf("isBSDNDPProxyUplink(%q) = true, want false", ifname)
		}
	}
	for _, ifname := range []string{"en0", "vlan0", "lagg0", "vtnet0"} {
		if !isBSDNDPProxyUplink(ifname) {
			t.Fatalf("isBSDNDPProxyUplink(%q) = false, want true", ifname)
		}
	}
}

func TestBSDRouteInterfaceFromOutput(t *testing.T) {
	ifname, err := bsdRouteInterfaceFromOutput("route to: 2001:db8::10\n  interface: en0\n")
	if err != nil || ifname != "en0" {
		t.Fatalf("bsdRouteInterfaceFromOutput() = (%q, %v), want (en0, nil)", ifname, err)
	}
	if _, err := bsdRouteInterfaceFromOutput("route to: 2001:db8::10\n"); err == nil {
		t.Fatal("bsdRouteInterfaceFromOutput() accepted output without an interface")
	}
}

func TestNDPEntriesRecognizesPermanentProxyWithoutStateColumn(t *testing.T) {
	target := netip.MustParseAddr("2001:db8::10")
	output := "Neighbor Linklayer Address Netif Expire St Flgs Prbs\n" +
		"2001:db8::10 02:00:00:00:00:10 en0 permanent p\n"

	entry, ok := ndpEntries(output)[target]
	if !ok {
		t.Fatal("proxy entry was not parsed")
	}
	if entry.ifname != "en0" || !entry.proxy {
		t.Fatalf("entry = %#v, want proxy on en0", entry)
	}
}

func TestBSDRejectsStaticPrefixBroaderThan128(t *testing.T) {
	previous := staticTargets
	staticTargets = []netip.Prefix{netip.MustParsePrefix("2001:db8::/64")}
	defer func() { staticTargets = previous }()

	err := runResponder(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/128") {
		t.Fatalf("runResponder() error = %v, want /128 validation error", err)
	}
}

func testNDPProxyOutput(target netip.Addr, ifname string) string {
	return fmt.Sprintf("Neighbor Linklayer Address Netif Expire St Flgs Prbs\n%s 02:00:00:00:00:10 %s permanent S p\n", target, ifname)
}

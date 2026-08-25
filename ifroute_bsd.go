//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
)

// defaultIPv6RouteIndexes uses the BSD route utility. macOS and the supported
// BSD hosts print an "interface:" line for a successful IPv6 route lookup.
func defaultIPv6RouteIndexes() ([]int, error) {
	ifname, err := bsdIPv6RouteInterface(netip.MustParseAddr("2000::"))
	if err != nil {
		return nil, err
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	return []int{ifi.Index}, nil
}

// bsdIPv6RouteInterface returns the interface that BSD ndp will select for a
// proxy entry. The ndp utility itself performs the same route lookup before it
// can add an entry, so checking first avoids partially applying bad targets.
func bsdIPv6RouteInterface(target netip.Addr) (string, error) {
	if !target.IsValid() || !target.Is6() {
		return "", fmt.Errorf("%s is not a valid IPv6 route target", target)
	}
	routePath, err := lookupBSDSystemCommand("route", "/sbin/route", "/usr/sbin/route")
	if err != nil {
		return "", err
	}
	out, err := exec.Command(routePath, "-n", "get", "-inet6", target.String()).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("route lookup failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return bsdRouteInterfaceFromOutput(string(out))
}

func bsdRouteInterfaceFromOutput(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.TrimSpace(key) != "interface" {
			continue
		}
		ifname := strings.TrimSpace(value)
		if ifname != "" {
			return ifname, nil
		}
	}
	return "", fmt.Errorf("could not determine the IPv6 route interface")
}

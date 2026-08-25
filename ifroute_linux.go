//go:build linux

package main

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

func defaultIPv6RouteIndexes() ([]int, error) {
	routes, err := netlink.RouteGet(net.ParseIP("2000::"))
	if err != nil {
		return nil, err
	}
	indexes := make([]int, 0, len(routes))
	for _, route := range routes {
		if route.LinkIndex > 0 {
			indexes = append(indexes, route.LinkIndex)
		}
	}
	if len(indexes) == 0 {
		return nil, fmt.Errorf("IPv6 default route has no usable interface")
	}
	return indexes, nil
}

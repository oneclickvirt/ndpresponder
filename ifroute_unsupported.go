//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package main

import "fmt"

func defaultIPv6RouteIndexes() ([]int, error) {
	return nil, fmt.Errorf("IPv6 default-route lookup is not available on this platform")
}

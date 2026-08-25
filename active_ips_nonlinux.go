//go:build !linux

package main

import "net/netip"

// BSD proxy mode synchronizes the complete address set through
// activeIPsChanged, so it does not need Linux's gratuitous-announcement queue.
func publishNewActiveIP(netip.Addr) {}

package main

import (
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"

	"go4.org/netipx"
)

var (
	activeIPs        atomic.Pointer[netipx.IPSet]
	activeIPSourceMu sync.Mutex
	activeIPSources  = map[string]map[netip.Addr]struct{}{}
	activeIPsChanged = make(chan struct{}, 1)
)

func init() {
	activeIPs.Store(new(netipx.IPSet))
}

// replaceActiveIPSource atomically replaces one runtime's address view and
// publishes only genuinely new addresses for gratuitous announcements.
func replaceActiveIPSource(source string, addresses []netip.Addr) {
	next := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if address.IsValid() && address.Is6() {
			next[address] = struct{}{}
		}
	}

	activeIPSourceMu.Lock()
	previous := activeIPSources[source]
	changed := !sameAddressSet(previous, next)
	newAddresses := make([]netip.Addr, 0, len(next))
	for address := range next {
		if _, known := previous[address]; !known {
			newAddresses = append(newAddresses, address)
		}
	}
	activeIPSources[source] = next

	var union netipx.IPSetBuilder
	for _, sourceAddresses := range activeIPSources {
		for address := range sourceAddresses {
			union.Add(address)
		}
	}
	set, _ := union.IPSet()
	// Publish while the source map is still locked. Otherwise, two concurrent
	// runtime refreshes can build two valid snapshots and the older one can be
	// stored last, dropping addresses learned by the newer refresh.
	activeIPs.Store(set)
	activeIPSourceMu.Unlock()

	if changed {
		select {
		case activeIPsChanged <- struct{}{}:
		default:
		}
	}
	for _, address := range newAddresses {
		publishNewActiveIP(address)
	}
}

func sameAddressSet(a, b map[netip.Addr]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for address := range a {
		if _, ok := b[address]; !ok {
			return false
		}
	}
	return true
}

func activeAddressSnapshot() []netip.Addr {
	activeIPSourceMu.Lock()
	defer activeIPSourceMu.Unlock()

	seen := make(map[netip.Addr]struct{})
	for _, sourceAddresses := range activeIPSources {
		for address := range sourceAddresses {
			seen[address] = struct{}{}
		}
	}
	addresses := make([]netip.Addr, 0, len(seen))
	for address := range seen {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Compare(addresses[j]) < 0
	})
	return addresses
}

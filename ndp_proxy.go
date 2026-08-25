package main

import (
	"net/netip"
	"strings"
)

type ndpEntry struct {
	ifname string
	proxy  bool
}

// ndpEntries recognizes the stable columns emitted by BSD ndp -an. The
// proxy flag is lowercase "p"; an ordinary neighbor entry must never be
// mistaken for a proxy owned by this program. Empty state columns are omitted
// by strings.Fields, so the flag cannot be addressed by a fixed index.
func ndpEntries(output string) map[netip.Addr]ndpEntry {
	entries := make(map[netip.Addr]ndpEntry)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		address, err := netip.ParseAddr(strings.Split(fields[0], "%")[0])
		if err != nil || !address.Is6() {
			continue
		}

		entry := ndpEntry{ifname: fields[2]}
		// ndp prints: address, lladdr, interface, expiry, state, flags,
		// probes. Its state column is blank for permanent entries, and
		// strings.Fields then shifts the flags column left. Lowercase p is the
		// RTF_ANNOUNCE marker; uppercase P means something different.
		for _, field := range fields[3:] {
			if isNDPProxyFlag(field) {
				entry.proxy = true
				break
			}
		}
		entries[address.Unmap()] = entry
	}
	return entries
}

// isNDPProxyFlag accepts only the compact flags column from ndp output. BSD
// versions may append an NS probe count directly to that column (for example
// "Rp3"). A naive strings.Contains(field, "p") also matches ordinary states
// such as "incomplete" and "probe", which must never be treated as proxies.
func isNDPProxyFlag(field string) bool {
	if field == "" {
		return false
	}

	letterEnd := 0
	for letterEnd < len(field) && ((field[letterEnd] >= 'A' && field[letterEnd] <= 'Z') || (field[letterEnd] >= 'a' && field[letterEnd] <= 'z')) {
		letterEnd++
	}
	if letterEnd == 0 {
		return false
	}
	for _, r := range field[:letterEnd] {
		switch r {
		case 'R', 'P', 'W', 'l', 'p':
		default:
			return false
		}
	}
	for _, r := range field[letterEnd:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return strings.Contains(field[:letterEnd], "p")
}

// ndpProxyEntryExists accepts only a proxy on the selected uplink.  Existing
// ordinary neighbor entries are deliberately left untouched because they may
// belong to another administrator or service.
func ndpProxyEntryExists(output string, target netip.Addr, ifname string) bool {
	entry, ok := ndpEntries(output)[target.Unmap()]
	return ok && entry.proxy && (ifname == "" || entry.ifname == ifname)
}

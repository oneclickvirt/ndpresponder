package main

import (
	"net/netip"
	"testing"
)

func TestNDPProxyEntryExists(t *testing.T) {
	output := "Neighbor                              Linklayer Address  Netif Expire    St Flgs Prbs\n2001:db8::10                          02:00:00:00:00:10 en0 permanent S p\n2001:db8::11                          02:00:00:00:00:11 en0 permanent S\n"
	if !ndpProxyEntryExists(output, netip.MustParseAddr("2001:db8::10"), "en0") {
		t.Fatal("expected proxy entry to be found")
	}
	if ndpProxyEntryExists(output, netip.MustParseAddr("2001:db8::10"), "en1") {
		t.Fatal("expected mismatched interface to be rejected")
	}
	if ndpProxyEntryExists(output, netip.MustParseAddr("2001:db8::11"), "en0") {
		t.Fatal("expected ordinary neighbor entry to be rejected")
	}
}

func TestNDPEntriesDoesNotTreatOrdinaryStatesAsProxyFlags(t *testing.T) {
	for _, state := range []string{"incomplete", "probe"} {
		t.Run(state, func(t *testing.T) {
			output := "Neighbor Linklayer Address Netif Expire St Flgs Prbs\n" +
				"2001:db8::10 02:00:00:00:00:10 en0 permanent " + state + "\n"
			if ndpProxyEntryExists(output, netip.MustParseAddr("2001:db8::10"), "en0") {
				t.Fatalf("ordinary %s neighbor was parsed as a proxy", state)
			}
		})
	}
}

func TestNDPEntriesRecognizesProxyFlagWithProbeCount(t *testing.T) {
	output := "Neighbor Linklayer Address Netif Expire St Flgs Prbs\n" +
		"2001:db8::10 02:00:00:00:00:10 en0 permanent S Rp3\n"
	if !ndpProxyEntryExists(output, netip.MustParseAddr("2001:db8::10"), "en0") {
		t.Fatal("expected proxy entry with an attached probe count to be found")
	}
}

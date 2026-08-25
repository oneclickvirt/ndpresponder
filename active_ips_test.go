package main

import (
	"net/netip"
	"testing"
)

func TestReplaceActiveIPSourceSignalsOnlyForChanges(t *testing.T) {
	const source = "test:active-ip-source"
	drainAddressChangeSignal()

	first := netip.MustParseAddr("2001:db8::10")
	second := netip.MustParseAddr("2001:db8::11")
	replaceActiveIPSource(source, []netip.Addr{first})
	expectAddressChangeSignal(t, true)

	replaceActiveIPSource(source, []netip.Addr{first})
	expectAddressChangeSignal(t, false)

	replaceActiveIPSource(source, []netip.Addr{second})
	expectAddressChangeSignal(t, true)

	replaceActiveIPSource(source, nil)
	drainAddressChangeSignal()
}

func TestReplaceActiveIPSourceMergesIndependentRuntimeSources(t *testing.T) {
	const firstSource = "test:active-ip-source-first"
	const secondSource = "test:active-ip-source-second"
	first := netip.MustParseAddr("2001:db8::21")
	second := netip.MustParseAddr("2001:db8::22")
	drainAddressChangeSignal()
	t.Cleanup(func() {
		replaceActiveIPSource(firstSource, nil)
		replaceActiveIPSource(secondSource, nil)
		drainAddressChangeSignal()
	})

	replaceActiveIPSource(firstSource, []netip.Addr{first})
	replaceActiveIPSource(secondSource, []netip.Addr{second})
	if !activeIPs.Load().Contains(first) || !activeIPs.Load().Contains(second) {
		t.Fatalf("active snapshot lost a runtime source: %v", activeAddressSnapshot())
	}
}

func expectAddressChangeSignal(t *testing.T, want bool) {
	t.Helper()
	select {
	case <-activeIPsChanged:
		if !want {
			t.Fatal("unexpected address change signal")
		}
	default:
		if want {
			t.Fatal("missing address change signal")
		}
	}
}

func drainAddressChangeSignal() {
	for {
		select {
		case <-activeIPsChanged:
		default:
			return
		}
	}
}

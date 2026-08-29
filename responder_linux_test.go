//go:build linux

package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"go4.org/netipx"
)

func TestSolicitationCaptureStopError(t *testing.T) {
	if err := solicitationCaptureStopError(context.Background()); !errors.Is(err, errSolicitationCaptureStopped) {
		t.Fatalf("solicitationCaptureStopError() = %v, want unexpected-stop error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := solicitationCaptureStopError(ctx); err != nil {
		t.Fatalf("solicitationCaptureStopError(canceled context) = %v, want nil", err)
	}
}

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

func TestPrepareResponderStopsAtFirstGatewayCandidate(t *testing.T) {
	candidates := []interfaceCandidate{
		{Interface: net.Interface{Name: "eth0", Index: 2}, Reason: "requested"},
		{Interface: net.Interface{Name: "veth0", Index: 3}, Reason: "interface-scan"},
	}
	var initialized []string
	selected, err := prepareResponderWith(candidates, func(candidate interfaceCandidate) (*responderRuntime, error) {
		initialized = append(initialized, candidate.Interface.Name)
		return &responderRuntime{
			netif:  &candidate.Interface,
			hi:     HostInfo{GatewayIP: netip.MustParseAddr("fe80::1")},
			reason: candidate.Reason,
		}, nil
	})
	if err != nil {
		t.Fatalf("prepareResponderWith() error = %v", err)
	}
	if selected.netif.Name != "eth0" {
		t.Fatalf("selected interface = %q, want eth0", selected.netif.Name)
	}
	if len(initialized) != 1 || initialized[0] != "eth0" {
		t.Fatalf("initialized candidates = %#v, want only eth0", initialized)
	}
}

func TestPrepareResponderHonorsInitializedRequestedCandidate(t *testing.T) {
	candidates := []interfaceCandidate{
		{Interface: net.Interface{Name: "wrong0", Index: 2}, Reason: "requested"},
		{Interface: net.Interface{Name: "eth0", Index: 3}, Reason: "default-ipv6-route"},
	}
	var initialized []string
	selected, err := prepareResponderWith(candidates, func(candidate interfaceCandidate) (*responderRuntime, error) {
		initialized = append(initialized, candidate.Interface.Name)
		if candidate.Interface.Name == "wrong0" {
			return &responderRuntime{netif: &candidate.Interface, reason: candidate.Reason}, nil
		}
		return nil, errors.New("unexpected candidate")
	})
	if err != nil {
		t.Fatalf("prepareResponderWith() error = %v", err)
	}
	if selected.netif.Name != "wrong0" {
		t.Fatalf("selected interface = %q, want wrong0", selected.netif.Name)
	}
	if len(initialized) != 1 || initialized[0] != "wrong0" {
		t.Fatalf("initialized candidates = %#v, want only requested candidate", initialized)
	}
}

func TestPrepareResponderFallsBackWhenRequestedCandidateFails(t *testing.T) {
	candidates := []interfaceCandidate{
		{Interface: net.Interface{Name: "wrong0", Index: 2}, Reason: "requested"},
		{Interface: net.Interface{Name: "eth0", Index: 3}, Reason: "default-ipv6-route"},
	}
	var initialized []string
	selected, err := prepareResponderWith(candidates, func(candidate interfaceCandidate) (*responderRuntime, error) {
		initialized = append(initialized, candidate.Interface.Name)
		if candidate.Interface.Name == "wrong0" {
			return nil, errors.New("interface initialization failed")
		}
		return &responderRuntime{
			netif:  &candidate.Interface,
			hi:     HostInfo{GatewayIP: netip.MustParseAddr("2001:db8::1")},
			reason: candidate.Reason,
		}, nil
	})
	if err != nil {
		t.Fatalf("prepareResponderWith() error = %v", err)
	}
	if selected.netif.Name != "eth0" {
		t.Fatalf("selected interface = %q, want eth0", selected.netif.Name)
	}
	if len(initialized) != 2 {
		t.Fatalf("initialized candidates = %#v, want requested failure and fallback candidate", initialized)
	}
}

func TestSolicitationTargetReasonFiltersInternalRuntimeAddresses(t *testing.T) {
	var runtimeBuilder, staticBuilder netipx.IPSetBuilder
	public := netip.MustParseAddr("2001:4860:4860::8844")
	ula := netip.MustParseAddr("fd42::10")
	staticULA := netip.MustParseAddr("fd42::20")
	runtimeBuilder.Add(public)
	runtimeBuilder.Add(ula)
	staticBuilder.Add(ula)
	staticBuilder.Add(staticULA)
	runtimeTargets, _ := runtimeBuilder.IPSet()
	staticTargets, _ := staticBuilder.IPSet()
	if reason, ok := solicitationTargetReason(public, runtimeTargets, staticTargets); !ok || reason != "runtime" {
		t.Fatalf("public runtime target decision = (%q, %t), want (runtime, true)", reason, ok)
	}
	if reason, ok := solicitationTargetReason(ula, runtimeTargets, nil); ok || reason != "" {
		t.Fatalf("ULA runtime-only target decision = (%q, %t), want (\"\", false)", reason, ok)
	}
	if reason, ok := solicitationTargetReason(ula, runtimeTargets, staticTargets); !ok || reason != "static" {
		t.Fatalf("explicit static ULA target decision = (%q, %t), want (static, true)", reason, ok)
	}
	if reason, ok := solicitationTargetReason(staticULA, nil, staticTargets); !ok || reason != "static" {
		t.Fatalf("static target decision = (%q, %t), want (static, true)", reason, ok)
	}
	unknown := netip.MustParseAddr("2001:db8::99")
	if reason, ok := solicitationTargetReason(unknown, runtimeTargets, staticTargets); ok || reason != "" {
		t.Fatalf("unknown target decision = (%q, %t), want (\"\", false)", reason, ok)
	}
}

func TestRuntimeAddressNeedsAnnouncementExcludesInternalIPv6(t *testing.T) {
	for _, test := range []struct {
		address string
		want    bool
	}{
		{address: "2001:4860:4860::8844", want: true},
		{address: "fc00::10", want: false},
		{address: "fe80::10", want: false},
		{address: "::1", want: false},
	} {
		if got := runtimeAddressNeedsAnnouncement(netip.MustParseAddr(test.address)); got != test.want {
			t.Fatalf("runtimeAddressNeedsAnnouncement(%s) = %t, want %t", test.address, got, test.want)
		}
	}
}

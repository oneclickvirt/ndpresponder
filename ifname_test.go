package main

import (
	"errors"
	"net"
	"strings"
	"testing"
)

type fakeInterfaceProvider struct {
	byIndex    map[int]net.Interface
	interfaces []net.Interface
	listErr    error
}

func (f fakeInterfaceProvider) InterfaceByIndex(index int) (*net.Interface, error) {
	ifi, ok := f.byIndex[index]
	if !ok {
		return nil, errors.New("not found")
	}
	return &ifi, nil
}

func (f fakeInterfaceProvider) Interfaces() ([]net.Interface, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]net.Interface(nil), f.interfaces...), nil
}

func TestResolveInterfaceIncludesRequestedNameFirst(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{usableInterface("ens3", 2)},
	}
	routeIndexes := func() ([]int, error) {
		return nil, errors.New("no default route")
	}

	candidates, err := resolveInterfaceCandidatesWithIPv6AddressCheck("ens3", provider, routeIndexes, noIPv6Addresses)
	if err != nil {
		t.Fatalf("resolveInterfaceCandidatesWith returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	assertCandidate(t, candidates[0], "ens3", "requested,interface-scan")
}

func TestResolveInterfaceIncludesDefaultIPv6RouteWhenRequestedNameIsWrong(t *testing.T) {
	provider := fakeInterfaceProvider{
		byIndex: map[int]net.Interface{
			7: usableInterface("enp1s0", 7),
		},
		interfaces: []net.Interface{usableInterface("docker0", 3)},
	}
	routeIndexes := func() ([]int, error) {
		return []int{7}, nil
	}

	candidates, err := resolveInterfaceCandidatesWithIPv6AddressCheck("eth0", provider, routeIndexes, noIPv6Addresses)
	if err != nil {
		t.Fatalf("resolveInterfaceCandidatesWith returned error: %v", err)
	}
	assertCandidate(t, candidates[0], "enp1s0", "default-ipv6-route")
	assertCandidate(t, candidates[1], "docker0", "interface-scan")
}

func TestResolveInterfaceIncludesInterfaceScanCandidates(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Name: "lo", Index: 1, Flags: net.FlagUp | net.FlagLoopback},
			{Name: "sit0", Index: 2, Flags: net.FlagUp | net.FlagMulticast},
			usableInterface("eth1", 3),
		},
	}
	routeIndexes := func() ([]int, error) {
		return nil, errors.New("no default route")
	}

	candidates, err := resolveInterfaceCandidatesWithIPv6AddressCheck(autoIfname, provider, routeIndexes, noIPv6Addresses)
	if err != nil {
		t.Fatalf("resolveInterfaceCandidatesWith returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	assertCandidate(t, candidates[0], "eth1", "interface-scan")
}

func TestResolveInterfaceReportsAvailableInterfaces(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			{Name: "lo", Index: 1, Flags: net.FlagUp | net.FlagLoopback},
		},
	}
	routeIndexes := func() ([]int, error) {
		return nil, errors.New("no default route")
	}

	_, err := resolveInterfaceCandidatesWithIPv6AddressCheck(autoIfname, provider, routeIndexes, noIPv6Addresses)
	if err == nil {
		t.Fatal("resolveInterfaceCandidatesWith returned nil error")
	}
	if !strings.Contains(err.Error(), "available interfaces: lo") {
		t.Fatalf("error = %q, want available interface list", err)
	}
}

func TestResolveInterfacePrioritizesIPv6AddressCandidates(t *testing.T) {
	provider := fakeInterfaceProvider{
		interfaces: []net.Interface{
			usableInterface("eth1", 3),
			usableInterface("eth2", 4),
		},
	}
	routeIndexes := func() ([]int, error) {
		return nil, errors.New("no default route")
	}

	candidates, err := resolveInterfaceCandidatesWithIPv6AddressCheck(autoIfname, provider, routeIndexes, func(ifi *net.Interface) bool {
		return ifi != nil && ifi.Name == "eth2"
	})
	if err != nil {
		t.Fatalf("resolveInterfaceCandidatesWithIPv6AddressCheck returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(candidates))
	}
	assertCandidate(t, candidates[0], "eth2", "interface-ipv6-address,interface-scan")
	assertCandidate(t, candidates[1], "eth1", "interface-scan")
}

func assertCandidate(t *testing.T, candidate interfaceCandidate, name string, reason string) {
	t.Helper()
	if candidate.Interface.Name != name {
		t.Fatalf("candidate interface = %q, want %q", candidate.Interface.Name, name)
	}
	if candidate.Reason != reason {
		t.Fatalf("candidate reason = %q, want %q", candidate.Reason, reason)
	}
}

func usableInterface(name string, index int) net.Interface {
	return net.Interface{
		Name:         name,
		Index:        index,
		Flags:        net.FlagUp | net.FlagBroadcast | net.FlagMulticast,
		HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, byte(index)},
	}
}

func noIPv6Addresses(*net.Interface) bool {
	return false
}

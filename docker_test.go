//go:build !dragonfly

package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestRefreshDockerNetworksRefreshesEachConfiguredNetworkOnce(t *testing.T) {
	var refreshed []string
	if err := refreshDockerNetworks([]string{"ipv6-a", "ipv6-b", "ipv6-a"}, func(network string) error {
		refreshed = append(refreshed, network)
		return nil
	}); err != nil {
		t.Fatalf("refreshDockerNetworks() error = %v", err)
	}

	want := []string{"ipv6-a", "ipv6-b"}
	if !reflect.DeepEqual(refreshed, want) {
		t.Fatalf("refreshed = %v, want %v", refreshed, want)
	}
}

func TestDockerRefreshFailureClearsPreviouslyAdvertisedAddresses(t *testing.T) {
	const networkName = "missing-runtime-network"
	source := dockerIPSource(networkName)
	address := runtimeMustParseAddr("2001:db8::bad")
	replaceActiveIPSource(source, []netip.Addr{address})
	if !activeIPs.Load().Contains(address) {
		t.Fatal("test setup did not add the runtime address")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "network unavailable", http.StatusNotFound)
	}))
	defer server.Close()
	client, err := docker.NewClient(server.URL)
	if err != nil {
		t.Fatalf("docker.NewClient() error = %v", err)
	}
	previousClient := dockerClient
	dockerClient = client
	t.Cleanup(func() {
		dockerClient = previousClient
		replaceActiveIPSource(source, nil)
		drainAddressChangeSignal()
	})

	if err := dockerRefreshNetwork(networkName); err == nil {
		t.Fatal("dockerRefreshNetwork() succeeded for an unavailable network")
	}
	if activeIPs.Load().Contains(address) {
		t.Fatal("unavailable Docker network left its old IPv6 address advertised")
	}
}

func TestRuntimeIPv6AddressAcceptsDockerAndPodmanForms(t *testing.T) {
	for _, value := range []string{"2001:db8::10/64", "2001:db8::10"} {
		address, ok := runtimeIPv6Address(value)
		if !ok || address.String() != "2001:db8::10" {
			t.Fatalf("runtimeIPv6Address(%q) = (%s, %t)", value, address, ok)
		}
	}
	if _, ok := runtimeIPv6Address("not-an-ip"); ok {
		t.Fatal("runtimeIPv6Address accepted malformed input")
	}
}

func TestDockerIPSourceUsesConfiguredNetworkIdentifier(t *testing.T) {
	if got, want := dockerIPSource("podman-ipv6"), "docker:podman-ipv6"; got != want {
		t.Fatalf("dockerIPSource() = %q, want %q", got, want)
	}
}

func runtimeMustParseAddr(value string) netip.Addr {
	return netip.MustParseAddr(value)
}

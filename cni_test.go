package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadCNILeaseAddressesIgnoresNonIPv6Entries(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2001:db8::10", "10.0.0.3", "last_reserved_ip.0", "lock"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("container"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	addresses, err := readCNILeaseAddresses(dir)
	if err != nil {
		t.Fatalf("readCNILeaseAddresses() error = %v", err)
	}
	want := []netip.Addr{netip.MustParseAddr("2001:db8::10")}
	if !reflect.DeepEqual(addresses, want) {
		t.Fatalf("addresses = %v, want %v", addresses, want)
	}
}

func TestCNINetworkLeaseDirectories(t *testing.T) {
	dirs, err := cniLeaseDirectories([]string{"containerd-ipv6", "/tmp/leases"}, "/var/lib/cni/networks")
	if err != nil {
		t.Fatalf("cniLeaseDirectories() error = %v", err)
	}
	want := []string{"/var/lib/cni/networks/containerd-ipv6", "/tmp/leases"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
}

func TestCNINetworkLeaseDirectoriesRejectParentDirectory(t *testing.T) {
	_, err := cniLeaseDirectories([]string{".."}, "/var/lib/cni/networks")
	if err == nil {
		t.Fatal("cniLeaseDirectories() accepted parent directory network name")
	}
}

func TestCNIRefreshFailureClearsPreviouslyAdvertisedAddresses(t *testing.T) {
	dir := t.TempDir()
	notADirectory := filepath.Join(dir, "lease-file")
	if err := os.WriteFile(notADirectory, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "cni:" + notADirectory
	address := netip.MustParseAddr("2001:db8::23")
	drainAddressChangeSignal()
	replaceActiveIPSource(source, []netip.Addr{address})
	t.Cleanup(func() {
		replaceActiveIPSource(source, nil)
		drainAddressChangeSignal()
	})

	cniRefreshNetwork(notADirectory)
	if activeIPs.Load().Contains(address) {
		t.Fatal("unreadable CNI lease source left its old IPv6 address advertised")
	}
}

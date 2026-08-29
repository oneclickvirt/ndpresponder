package main

import (
	"bytes"
	"net"
	"net/netip"
	"testing"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

func TestUnsolicitedAdvertisementBuildsAllNodesNeighborAdvertisement(t *testing.T) {
	target := netip.MustParseAddr("2001:db8::42")
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x42}
	buffer := gopacket.NewSerializeBuffer()
	if err := UnsolicitedAdvertisement(buffer, HostInfo{HostMAC: mac}, target); err != nil {
		t.Fatalf("UnsolicitedAdvertisement() error = %v", err)
	}

	packet := gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	eth, ok := packet.Layer(layers.LayerTypeEthernet).(*layers.Ethernet)
	if !ok {
		t.Fatal("serialized packet has no Ethernet layer")
	}
	if !bytes.Equal(eth.SrcMAC, mac) || !bytes.Equal(eth.DstMAC, net.HardwareAddr{0x33, 0x33, 0, 0, 0, 1}) {
		t.Fatalf("Ethernet addresses = %s -> %s", eth.SrcMAC, eth.DstMAC)
	}
	ip6, ok := packet.Layer(layers.LayerTypeIPv6).(*layers.IPv6)
	if !ok {
		t.Fatal("serialized packet has no IPv6 layer")
	}
	if !ip6.SrcIP.Equal(target.AsSlice()) || !ip6.DstIP.Equal(net.IPv6linklocalallnodes) || ip6.HopLimit != 255 {
		t.Fatalf("IPv6 header = src=%s dst=%s hop=%d", ip6.SrcIP, ip6.DstIP, ip6.HopLimit)
	}
	na, ok := packet.Layer(layers.LayerTypeICMPv6NeighborAdvertisement).(*layers.ICMPv6NeighborAdvertisement)
	if !ok {
		t.Fatal("serialized packet has no Neighbor Advertisement layer")
	}
	if na.Flags != 0xa0 || !net.IP(na.TargetAddress).Equal(target.AsSlice()) {
		t.Fatalf("Neighbor Advertisement = flags=%#x target=%s", na.Flags, na.TargetAddress)
	}
	if len(na.Options) != 1 || na.Options[0].Type != layers.ICMPv6OptTargetAddress || !bytes.Equal(na.Options[0].Data, mac) {
		t.Fatalf("Neighbor Advertisement options = %#v", na.Options)
	}
}

func TestUnsolicitedAdvertisementRejectsInvalidInput(t *testing.T) {
	buffer := gopacket.NewSerializeBuffer()
	validMAC := net.HardwareAddr{0, 1, 2, 3, 4, 5}
	for _, target := range []netip.Addr{
		netip.IPv6Unspecified(),
		netip.MustParseAddr("ff02::1"),
		netip.MustParseAddr("127.0.0.1"),
	} {
		if err := UnsolicitedAdvertisement(buffer, HostInfo{HostMAC: validMAC}, target); err == nil {
			t.Fatalf("target %s was accepted", target)
		}
	}
	if err := UnsolicitedAdvertisement(buffer, HostInfo{HostMAC: validMAC[:5]}, netip.MustParseAddr("2001:db8::1")); err == nil {
		t.Fatal("short host MAC was accepted")
	}
}

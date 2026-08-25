//go:build linux

package main

import (
	"net"
	"net/netip"
	"time"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// maxGatewayRetries is the maximum number of attempts to resolve the gateway
// neighbor cache entry before giving up (one attempt per second).
const maxGatewayRetries = 30
const gatewayProbeTimeout = time.Second

func gatherHostInfo(netif *net.Interface) (hi HostInfo, e error) {
	logEntry := logger.Named("HostInfo")
	hi.HostMAC = netif.HardwareAddr
	logEntry.Info("found MAC", zap.Stringer("mac", hi.HostMAC))

	nl, e := netlink.NewHandle()
	if e != nil {
		logEntry.Error("netlink.NewHandle error", zap.Error(e))
		return hi, nil
	}
	defer nl.Close()

	link, e := nl.LinkByIndex(netif.Index)
	if e != nil {
		logEntry.Error("netlink.LinkByIndex error", zap.Error(e))
		return hi, nil
	}

	routes, e := nl.RouteList(link, unix.AF_INET6)
	if e != nil {
		logEntry.Error("netlink.RouteList error", zap.Error(e))
		return hi, nil
	}

	for _, route := range routes {
		maskLen := 0
		if route.Dst != nil {
			maskLen, _ = route.Dst.Mask.Size()
		}
		if maskLen == 0 && route.Gw != nil {
			if gateway, ok := netip.AddrFromSlice(route.Gw); ok {
				hi.GatewayIP = gateway.Unmap()
			}
		}
	}
	if !hi.GatewayIP.IsValid() {
		logEntry.Warn("no default gateway")
		return hi, nil
	}
	logEntry.Info("found gateway", zap.Stringer("gateway", hi.GatewayIP))

	var gatewayNeigh *netlink.Neigh
	needNoARP := false
	for attempt := 0; attempt < maxGatewayRetries; attempt++ {
		neighs, err := nl.NeighList(netif.Index, unix.AF_INET6)
		if err != nil {
			logEntry.Error("netlink.NeighList error", zap.Error(err))
			return hi, nil
		}
		done := false
		for _, neigh := range neighs {
			ip, _ := netip.AddrFromSlice(neigh.IP)
			ip = ip.Unmap()
			if ip != hi.GatewayIP || len(neigh.HardwareAddr) != 6 {
				continue
			}
			switch neigh.State {
			case unix.NUD_REACHABLE, unix.NUD_NOARP:
				n := neigh
				gatewayNeigh = &n
				needNoARP = true
				done = true
			case unix.NUD_PERMANENT:
				done = true
			}
		}
		if done {
			break
		}

		probeGatewayNeighbor(logEntry, netif, hi.GatewayIP)
		logEntry.Debug("waiting for gateway neigh entry", zap.Int("attempt", attempt+1))
		time.Sleep(time.Second)
	}

	if needNoARP && gatewayNeigh != nil {
		gatewayNeigh.State = unix.NUD_NOARP
		if e = nl.NeighSet(gatewayNeigh); e != nil {
			logEntry.Error("netlink.NeighSet error", zap.Error(e))
		} else {
			logEntry.Info("netlink.NeighSet OK", zap.Stringer("lladdr", gatewayNeigh.HardwareAddr))
		}
	} else if gatewayNeigh == nil && hi.GatewayIP.IsValid() {
		logEntry.Warn("gateway neigh entry not found after max retries, proceeding without NUD_NOARP")
	}

	return hi, nil
}

func probeGatewayNeighbor(logEntry *zap.Logger, netif *net.Interface, gateway netip.Addr) {
	if !gateway.IsValid() || !gateway.Is6() {
		return
	}

	host := gateway.String()
	if gateway.IsLinkLocalUnicast() && netif != nil && netif.Name != "" {
		host += "%" + netif.Name
	}
	target := net.JoinHostPort(host, "9")

	conn, err := (&net.Dialer{Timeout: gatewayProbeTimeout}).Dial("udp6", target)
	if err != nil {
		logEntry.Debug("gateway probe error", zap.String("target", target), zap.Error(err))
		return
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{0}); err != nil {
		logEntry.Debug("gateway probe write error", zap.String("target", target), zap.Error(err))
	}
}

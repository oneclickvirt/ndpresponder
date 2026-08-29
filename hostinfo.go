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

// Gateway discovery runs during boot while cloud-init/network managers may
// still be installing the IPv6 default route. Keep the probe bounded so a
// stale veth can never delay all other candidate interfaces for half a minute.
const (
	maxGatewayRouteRetries = 8
	maxGatewayRetries      = 8
	gatewayRouteRetryDelay = 250 * time.Millisecond
	gatewayProbeTimeout    = 500 * time.Millisecond
)

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

	for attempt := 0; attempt < maxGatewayRouteRetries; attempt++ {
		routes, routeErr := nl.RouteList(link, unix.AF_INET6)
		if routeErr != nil {
			logEntry.Error("netlink.RouteList error", zap.Error(routeErr))
			return hi, nil
		}
		hi.GatewayIP = defaultGateway(routes)
		if hi.GatewayIP.IsValid() {
			break
		}
		if attempt+1 < maxGatewayRouteRetries {
			time.Sleep(gatewayRouteRetryDelay)
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

		if attempt+1 < maxGatewayRetries {
			probeGatewayNeighbor(logEntry, netif, hi.GatewayIP)
			logEntry.Debug("waiting for gateway neigh entry", zap.Int("attempt", attempt+1))
			time.Sleep(gatewayRouteRetryDelay)
		}
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

// defaultGateway returns the lowest-priority IPv6 default route with a
// gateway. A host can briefly expose both an old and a new RA route while a
// network service is restarting; selecting by priority avoids nondeterministic
// interface selection from netlink iteration order.
func defaultGateway(routes []netlink.Route) netip.Addr {
	var gateway netip.Addr
	bestPriority := int(^uint(0) >> 1)
	for _, route := range routes {
		maskLen := 0
		if route.Dst != nil {
			maskLen, _ = route.Dst.Mask.Size()
		}
		if maskLen != 0 || route.Gw == nil {
			continue
		}
		candidate, ok := netip.AddrFromSlice(route.Gw)
		if !ok {
			continue
		}
		candidate = candidate.Unmap()
		if !candidate.Is6() {
			continue
		}
		if gateway.IsValid() && route.Priority >= bestPriority {
			continue
		}
		gateway = candidate
		bestPriority = route.Priority
	}
	return gateway
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

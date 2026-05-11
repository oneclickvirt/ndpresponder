package main

import (
	"net"
	"net/netip"
	"os/exec"
	"time"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// HostInfo contains address information of the host machine.
type HostInfo struct {
	HostMAC   net.HardwareAddr
	GatewayIP netip.Addr
}

// maxGatewayRetries is the maximum number of attempts to resolve the gateway
// neighbor cache entry before giving up (one attempt per second).
const maxGatewayRetries = 30

func gatherHostInfo() (hi HostInfo, e error) {
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
		if maskLen == 0 {
			hi.GatewayIP, _ = netip.AddrFromSlice(route.Gw)
			hi.GatewayIP = hi.GatewayIP.Unmap()
		}
	}
	if !hi.GatewayIP.IsValid() {
		logEntry.Warn("no default gateway")
		return hi, nil
	}
	logEntry.Info("found gateway", zap.Stringer("gateway", hi.GatewayIP))

	// Locate ping at runtime so the binary works on Alpine (/bin/ping)
	// and standard distros (/usr/bin/ping) alike.
	pingBin, err := exec.LookPath("ping")
	if err != nil {
		pingBin = "/bin/ping" // best-effort fallback
	}

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

		exec.Command(pingBin, "-c", "1", hi.GatewayIP.String()).Run()
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

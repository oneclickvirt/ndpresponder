package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"go.uber.org/zap"
)

const autoIfname = "auto"

type interfaceProvider interface {
	InterfaceByIndex(index int) (*net.Interface, error)
	Interfaces() ([]net.Interface, error)
}

type interfaceCandidate struct {
	Interface net.Interface
	Reason    string
}

type systemInterfaceProvider struct{}

func (systemInterfaceProvider) InterfaceByIndex(index int) (*net.Interface, error) {
	return net.InterfaceByIndex(index)
}

func (systemInterfaceProvider) Interfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func resolveInterfaceCandidates(ifname string) ([]interfaceCandidate, error) {
	return resolveInterfaceCandidatesWith(ifname, systemInterfaceProvider{}, defaultIPv6RouteIndexes)
}

func resolveInterfaceCandidatesWith(ifname string, ifs interfaceProvider, routeIndexes func() ([]int, error)) ([]interfaceCandidate, error) {
	return resolveInterfaceCandidatesWithIPv6AddressCheck(ifname, ifs, routeIndexes, interfaceHasIPv6Address)
}

// resolveInterfaceCandidatesWithIPv6AddressCheck keeps the candidate ordering
// deterministic in tests while production uses the host interface addresses.
func resolveInterfaceCandidatesWithIPv6AddressCheck(ifname string, ifs interfaceProvider, routeIndexes func() ([]int, error), hasIPv6Address func(*net.Interface) bool) ([]interfaceCandidate, error) {
	allInterfaces, err := ifs.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}

	var candidates []interfaceCandidate
	ifname = strings.TrimSpace(ifname)
	if ifname != "" && !strings.EqualFold(ifname, autoIfname) {
		ifi := interfaceByNameFromList(allInterfaces, ifname)
		if ifi != nil {
			if isUsableResponderInterface(ifi) {
				addInterfaceCandidate(&candidates, *ifi, "requested")
			} else {
				logger.Warn("requested network interface is not usable, falling back to auto-detection",
					zap.String("ifname", ifname),
					zap.String("flags", ifi.Flags.String()),
					zap.Stringer("mac", ifi.HardwareAddr),
				)
			}
		} else {
			logger.Warn("requested network interface not found, falling back to auto-detection",
				zap.String("ifname", ifname),
			)
		}
	}

	if routeIndexes != nil {
		if indexes, err := routeIndexes(); err == nil {
			for _, index := range indexes {
				if index <= 0 {
					continue
				}
				ifi, err := ifs.InterfaceByIndex(index)
				if err != nil {
					continue
				}
				if isUsableResponderInterface(ifi) {
					addInterfaceCandidate(&candidates, *ifi, "default-ipv6-route")
				}
			}
		} else {
			logger.Debug("default IPv6 route lookup failed, scanning interfaces", zap.Error(err))
		}
	}

	for i := range allInterfaces {
		ifi := &allInterfaces[i]
		if isUsableResponderInterface(ifi) && hasIPv6Address != nil && hasIPv6Address(ifi) {
			addInterfaceCandidate(&candidates, *ifi, "interface-ipv6-address")
		}
	}

	for i := range allInterfaces {
		ifi := &allInterfaces[i]
		if isUsableResponderInterface(ifi) {
			addInterfaceCandidate(&candidates, *ifi, "interface-scan")
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no usable network interface found (need an up non-loopback multicast interface with a 6-byte MAC); available interfaces: %s", describeInterfaces(allInterfaces))
	}

	return candidates, nil
}

func interfaceByNameFromList(interfaces []net.Interface, name string) *net.Interface {
	for i := range interfaces {
		if interfaces[i].Name == name {
			return &interfaces[i]
		}
	}
	return nil
}

func addInterfaceCandidate(candidates *[]interfaceCandidate, ifi net.Interface, reason string) {
	for i := range *candidates {
		if (*candidates)[i].Interface.Index != ifi.Index {
			continue
		}
		if !strings.Contains((*candidates)[i].Reason, reason) {
			(*candidates)[i].Reason += "," + reason
		}
		return
	}
	*candidates = append(*candidates, interfaceCandidate{Interface: ifi, Reason: reason})
}

func isUsableResponderInterface(ifi *net.Interface) bool {
	return ifi != nil &&
		ifi.Flags&net.FlagUp != 0 &&
		ifi.Flags&net.FlagLoopback == 0 &&
		ifi.Flags&net.FlagMulticast != 0 &&
		len(ifi.HardwareAddr) == 6
}

func interfaceHasIPv6Address(ifi *net.Interface) bool {
	addrs, err := ifi.Addrs()
	if err != nil {
		logger.Debug("interface address lookup failed",
			zap.String("ifname", ifi.Name),
			zap.Error(err),
		)
		return false
	}

	for _, addr := range addrs {
		if ip := addrIP(addr); ip.IsValid() && ip.Is6() && !ip.IsLoopback() && !ip.IsMulticast() {
			return true
		}
	}
	return false
}

func addrIP(addr net.Addr) netip.Addr {
	var ip net.IP
	switch a := addr.(type) {
	case *net.IPNet:
		ip = a.IP
	case *net.IPAddr:
		ip = a.IP
	default:
		return netip.Addr{}
	}

	out, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}
	}
	return out.Unmap()
}

func describeInterfaces(interfaces []net.Interface) string {
	if len(interfaces) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(interfaces))
	for _, ifi := range interfaces {
		mac := "none"
		if len(ifi.HardwareAddr) > 0 {
			mac = ifi.HardwareAddr.String()
		}
		parts = append(parts, fmt.Sprintf("%s(index=%d flags=%s mac=%s)", ifi.Name, ifi.Index, ifi.Flags, mac))
	}
	return strings.Join(parts, ", ")
}

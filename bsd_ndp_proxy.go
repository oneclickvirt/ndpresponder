//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

type ndpCommandRunner func(name string, args ...string) ([]byte, error)
type bsdRouteInterfaceLookup func(netip.Addr) (string, error)

type bsdProxyResponder struct {
	netif *net.Interface
	path  string
	run   ndpCommandRunner
	route bsdRouteInterfaceLookup
	owned map[netip.Addr]struct{}
}

// prepareBSDProxyResponder chooses an interface that can proxy every target
// known at startup. This matters on macOS, where bridge interfaces can appear
// before the physical uplink in net.Interfaces().
func prepareBSDProxyResponder(candidates []interfaceCandidate, targets []netip.Addr) (*bsdProxyResponder, error) {
	var failures []string
	for _, candidate := range candidates {
		if !isBSDNDPProxyUplink(candidate.Interface.Name) {
			failures = append(failures, fmt.Sprintf("%s(%s): virtual interface is not an NDP proxy uplink", candidate.Interface.Name, candidate.Reason))
			continue
		}
		responder, err := newBSDProxyResponder(&candidate.Interface)
		if err == nil {
			err = responder.checkTargetRoutes(targets)
		}
		if err == nil {
			logger.Info("selected BSD NDP proxy interface",
				zap.String("ifname", candidate.Interface.Name),
				zap.String("reason", candidate.Reason),
				zap.Stringer("mac", candidate.Interface.HardwareAddr),
			)
			return responder, nil
		}
		failures = append(failures, fmt.Sprintf("%s(%s): %v", candidate.Interface.Name, candidate.Reason, err))
	}
	return nil, fmt.Errorf("no candidate interface can manage BSD NDP proxies: %s", strings.Join(failures, "; "))
}

// isBSDNDPProxyUplink excludes interfaces that represent a local VM, tunnel,
// or software bridge. Their addresses are not on the physical Ethernet link
// where macOS and BSD hosts install proxy NDP neighbors.
func isBSDNDPProxyUplink(ifname string) bool {
	name := strings.ToLower(strings.TrimSpace(ifname))
	for _, prefix := range []string{
		"bridge", "docker", "podman", "veth", "virbr", "epair", "vether", "vnet",
		"utun", "tun", "tap", "gif", "gre", "stf", "ipsec", "wg", "wireguard",
		"vmnet", "vboxnet", "feth", "anpi", "awdl", "llw", "lo",
	} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return name != ""
}

func newBSDProxyResponder(netif *net.Interface) (*bsdProxyResponder, error) {
	if netif == nil || len(netif.HardwareAddr) != 6 {
		return nil, fmt.Errorf("interface does not have an Ethernet MAC address")
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("BSD NDP proxy mode must run as root")
	}
	path, err := lookupBSDSystemCommand("ndp", "/usr/sbin/ndp", "/sbin/ndp")
	if err != nil {
		return nil, fmt.Errorf("BSD ndp command not found: %w", err)
	}
	return &bsdProxyResponder{
		netif: netif,
		path:  path,
		run:   runNDPCommand,
		route: bsdIPv6RouteInterface,
		owned: make(map[netip.Addr]struct{}),
	}, nil
}

func runNDPCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (r *bsdProxyResponder) Sync(targets []netip.Addr) error {
	wanted := make(map[netip.Addr]struct{}, len(targets))
	for _, target := range targets {
		target = target.Unmap()
		if target.IsValid() && target.Is6() {
			wanted[target] = struct{}{}
		}
	}

	var failures []string
	for target := range wanted {
		if err := r.ensure(target); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for target := range r.owned {
		if _, keep := wanted[target]; keep {
			continue
		}
		if err := r.remove(target); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func (r *bsdProxyResponder) ensure(target netip.Addr) error {
	entries, err := r.list()
	if err != nil {
		return err
	}
	if entry, exists := entries[target]; exists {
		if entry.proxy && entry.ifname == r.netif.Name {
			if _, owned := r.owned[target]; !owned {
				logger.Info("BSD NDP proxy is already managed externally; leaving it unchanged",
					zap.Stringer("ip", target),
					zap.String("ifname", entry.ifname),
				)
			}
			return nil
		}
		if _, owned := r.owned[target]; owned {
			delete(r.owned, target)
		}
		entryKind := "ordinary neighbor"
		if entry.proxy {
			entryKind = fmt.Sprintf("proxy on %s", entry.ifname)
		}
		// Continuing here would make the process appear healthy while the
		// requested address is not proxied on the selected uplink. Refuse to
		// overwrite another entry, but make the conflict visible to callers.
		return fmt.Errorf("NDP entry for %s already exists as %s; refusing to overwrite it", target, entryKind)
	}

	// BSD ndp does not permit -i together with -s. It selects the
	// interface from the route to the target address. Check that route before
	// changing the NDP table so a target routed through a bridge or VPN cannot
	// leave a proxy entry on an unintended interface.
	if err := r.checkTargetRoute(target); err != nil {
		return err
	}
	output, err := r.run(r.path, "-s", target.String(), r.netif.HardwareAddr.String(), "proxy")
	if err != nil {
		return fmt.Errorf("add NDP proxy for %s on %s: %w: %s", target, r.netif.Name, err, strings.TrimSpace(string(output)))
	}
	entries, err = r.list()
	if err != nil {
		return err
	}
	entry, exists := entries[target]
	if !exists || !entry.proxy || entry.ifname != r.netif.Name {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return fmt.Errorf("BSD did not create an NDP proxy for %s on %s: %s", target, r.netif.Name, detail)
		}
		return fmt.Errorf("BSD did not create an NDP proxy for %s on %s; ensure the target routes through that interface", target, r.netif.Name)
	}
	r.owned[target] = struct{}{}
	logger.Info("BSD NDP proxy added", zap.Stringer("ip", target), zap.String("ifname", r.netif.Name))
	return nil
}

func (r *bsdProxyResponder) checkTargetRoute(target netip.Addr) error {
	if r.netif == nil || r.netif.Name == "" {
		return fmt.Errorf("BSD NDP proxy has no selected uplink interface")
	}
	if r.route == nil {
		return fmt.Errorf("BSD IPv6 route lookup is not configured")
	}

	ifname, err := r.route(target)
	if err != nil {
		return fmt.Errorf("look up IPv6 route for NDP proxy target %s: %w", target, err)
	}
	if ifname != r.netif.Name {
		return fmt.Errorf("NDP proxy target %s routes through %s, not selected uplink %s", target, ifname, r.netif.Name)
	}
	return nil
}

func (r *bsdProxyResponder) checkTargetRoutes(targets []netip.Addr) error {
	seen := make(map[netip.Addr]struct{}, len(targets))
	for _, target := range targets {
		target = target.Unmap()
		if !target.IsValid() || !target.Is6() {
			continue
		}
		if _, alreadyChecked := seen[target]; alreadyChecked {
			continue
		}
		seen[target] = struct{}{}
		if err := r.checkTargetRoute(target); err != nil {
			return err
		}
	}
	return nil
}

func (r *bsdProxyResponder) remove(target netip.Addr) error {
	entries, err := r.list()
	if err != nil {
		return err
	}
	entry, exists := entries[target]
	if !exists || !entry.proxy || entry.ifname != r.netif.Name {
		delete(r.owned, target)
		return nil
	}

	output, err := r.run(r.path, "-d", target.String())
	if err != nil {
		return fmt.Errorf("remove NDP proxy for %s on %s: %w: %s", target, r.netif.Name, err, strings.TrimSpace(string(output)))
	}
	entries, err = r.list()
	if err != nil {
		return err
	}
	if entry, exists := entries[target]; exists && entry.proxy && entry.ifname == r.netif.Name {
		return fmt.Errorf("BSD did not remove NDP proxy for %s on %s", target, r.netif.Name)
	}
	delete(r.owned, target)
	logger.Info("BSD NDP proxy removed", zap.Stringer("ip", target), zap.String("ifname", r.netif.Name))
	return nil
}

func (r *bsdProxyResponder) list() (map[netip.Addr]ndpEntry, error) {
	output, err := r.run(r.path, "-an")
	if err != nil {
		return nil, fmt.Errorf("inspect BSD NDP entries: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return ndpEntries(string(output)), nil
}

func (r *bsdProxyResponder) Close() {
	for target := range r.owned {
		if err := r.remove(target); err != nil {
			logger.Warn("remove BSD NDP proxy during shutdown failed", zap.Stringer("ip", target), zap.Error(err))
		}
	}
}

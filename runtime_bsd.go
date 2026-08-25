//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"context"
	"fmt"
	"net/netip"

	"go.uber.org/zap"
)

func runResponder(ctx context.Context) error {
	for _, prefix := range staticTargets {
		if prefix.Bits() != 128 {
			return fmt.Errorf("macOS and BSD hosts support static NDP proxy targets only as /128 addresses; use -N or -C for dynamic runtime addresses")
		}
	}

	if len(dockerNetworks) > 0 {
		if err := dockerListen(ctx); err != nil {
			return err
		}
	}
	if len(cniNetworks) > 0 {
		if err := cniListen(ctx); err != nil {
			return err
		}
	}

	// Populate runtime addresses before selecting an interface. A macOS host can
	// expose both Docker Desktop bridges and a physical uplink; only the latter
	// can proxy an address routed to the external network.
	targets := bsdProxyTargets()
	responder, err := prepareBSDProxyResponder(interfaceCandidates, targets)
	if err != nil {
		return err
	}
	defer responder.Close()

	if err := responder.Sync(targets); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-activeIPsChanged:
			if err := responder.Sync(bsdProxyTargets()); err != nil {
				logger.Warn("synchronize BSD NDP proxy targets failed", zap.Error(err))
			}
		}
	}
}

func bsdProxyTargets() []netip.Addr {
	targets := make([]netip.Addr, 0, len(staticTargets))
	for _, prefix := range staticTargets {
		targets = append(targets, prefix.Addr())
	}
	targets = append(targets, activeAddressSnapshot()...)
	return targets
}

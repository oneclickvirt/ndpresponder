//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"go.uber.org/zap"
)

func runResponder(ctx context.Context) error {
	if err := clearResponderReadyFile(); err != nil {
		return err
	}
	defer func() {
		if err := clearResponderReadyFile(); err != nil {
			logger.Warn("clear responder ready file failed", zap.Error(err))
		}
	}()

	for _, prefix := range currentStaticTargets() {
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
	if err := markResponderReady(responder.netif.Name); err != nil {
		return err
	}

	var targetTicker *time.Ticker
	var targetUpdates <-chan time.Time
	if staticTargetFile != "" {
		targetTicker = time.NewTicker(targetFileReloadEvery)
		targetUpdates = targetTicker.C
		defer targetTicker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-targetUpdates:
			reloadStaticTargetFile()
			if err := responder.Sync(bsdProxyTargets()); err != nil {
				logger.Warn("synchronize BSD NDP proxy targets failed", zap.Error(err))
			}
		case <-activeIPsChanged:
			if err := responder.Sync(bsdProxyTargets()); err != nil {
				logger.Warn("synchronize BSD NDP proxy targets failed", zap.Error(err))
			}
		}
	}
}

func bsdProxyTargets() []netip.Addr {
	static := currentStaticTargets()
	targets := make([]netip.Addr, 0, len(static))
	for _, prefix := range static {
		targets = append(targets, prefix.Addr())
	}
	for _, address := range activeAddressSnapshot() {
		if runtimeAddressNeedsAnnouncement(address) {
			targets = append(targets, address)
		}
	}
	return targets
}

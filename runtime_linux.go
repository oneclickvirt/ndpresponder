//go:build linux

package main

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"time"

	"github.com/gopacket/gopacket"
	"go.uber.org/zap"
	"go4.org/netipx"
)

var errSolicitationCaptureStopped = errors.New("neighbor solicitation capture stopped unexpectedly")

func runResponder(ctx context.Context) error {
	if err := clearResponderReadyFile(); err != nil {
		return err
	}
	defer func() {
		if err := clearResponderReadyFile(); err != nil {
			logger.Warn("clear responder ready file failed", zap.Error(err))
		}
	}()

	rt, err := prepareResponder(interfaceCandidates)
	if err != nil {
		return err
	}
	defer rt.handle.Close()

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

	sbuf := gopacket.NewSerializeBuffer()
	solicitations := CaptureNeighSolicitation(rt.handle)
	announcedStatic := make(map[netip.Addr]struct{})
	announceStaticTargets(rt, sbuf, announcedStatic)
	for _, ip := range activeAddressSnapshot() {
		if runtimeAddressNeedsAnnouncement(ip) {
			announceAddress(rt, sbuf, ip, "runtime-startup")
		}
	}
	if err := markResponderReady(rt.netif.Name); err != nil {
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
			announceStaticTargets(rt, sbuf, announcedStatic)

		case ns, ok := <-solicitations:
			if !ok {
				return solicitationCaptureStopError(ctx)
			}
			logEntry := logger.With(zap.Stringer("ns", ns))
			reason, shouldRespond := solicitationTargetReason(ns.TargetIP, activeIPs.Load(), currentStaticTargetSet())
			if !shouldRespond {
				logEntry.Debug("IGNORE")
				continue
			}
			logEntry = logEntry.With(zap.String("reason", reason))

			if err := ns.Respond(sbuf, rt.hi); err != nil {
				logEntry.Warn("RESPOND error", zap.Error(err))
				continue
			}
			logEntry.Info("RESPOND")
			if err := rt.handle.WritePacketData(sbuf.Bytes()); err != nil {
				logEntry.Warn("WritePacketData error", zap.Error(err))
			}

		case ip := <-newActiveIP:
			logEntry := logger.With(zap.Stringer("ip", ip))
			if !runtimeAddressNeedsAnnouncement(ip) {
				logEntry.Debug("skip unsolicited announcement for non-public runtime address")
				continue
			}
			if err := UnsolicitedAdvertisement(sbuf, rt.hi, ip); err != nil {
				logEntry.Warn("UNSOLICITED advertisement error", zap.Error(err))
			} else if err := rt.handle.WritePacketData(sbuf.Bytes()); err != nil {
				logEntry.Warn("WritePacketData unsolicited advertisement error", zap.Error(err))
			} else {
				logEntry.Info("UNSOLICITED")
			}
			if err := Gratuitous(sbuf, rt.hi, ip); err != nil {
				logEntry.Warn("GRATUITOUS error", zap.Error(err))
				continue
			}
			logEntry.Info("GRATUITOUS")
			if err := rt.handle.WritePacketData(sbuf.Bytes()); err != nil {
				logEntry.Warn("WritePacketData error", zap.Error(err))
			}

			if !rt.hi.GatewayIP.IsValid() {
				continue
			}
			if err := Solicit(sbuf, rt.hi, ip); err != nil {
				logEntry.Warn("SOLICIT error", zap.Error(err))
				continue
			}
			logEntry.Info("SOLICIT")
			if err := rt.handle.WritePacketData(sbuf.Bytes()); err != nil {
				logEntry.Warn("WritePacketData error", zap.Error(err))
			}
		}
	}
}

func solicitationCaptureStopError(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	return errSolicitationCaptureStopped
}

// solicitationTargetReason keeps runtime announcements limited to public
// routed addresses. Runtime collectors also see ULA and link-local addresses
// from private container networks; answering those on the physical uplink
// would accidentally proxy internal addresses upstream.
func solicitationTargetReason(target netip.Addr, runtimeTargets, staticTargets *netipx.IPSet) (string, bool) {
	if runtimeTargets != nil && runtimeTargets.Contains(target) {
		if runtimeAddressNeedsAnnouncement(target) {
			return "runtime", true
		}
	}
	if staticTargets != nil && staticTargets.Contains(target) {
		return "static", true
	}
	return "", false
}

func announceAddress(rt *responderRuntime, sbuf gopacket.SerializeBuffer, ip netip.Addr, reason string) bool {
	if err := UnsolicitedAdvertisement(sbuf, rt.hi, ip); err != nil {
		logger.Warn("UNSOLICITED advertisement error", zap.Stringer("ip", ip), zap.Error(err))
		return false
	}
	if err := rt.handle.WritePacketData(sbuf.Bytes()); err != nil {
		logger.Warn("WritePacketData unsolicited advertisement error", zap.Stringer("ip", ip), zap.Error(err))
		return false
	}
	logger.Info("UNSOLICITED", zap.Stringer("ip", ip), zap.String("reason", reason))
	return true
}

func announceStaticTargets(rt *responderRuntime, sbuf gopacket.SerializeBuffer, announced map[netip.Addr]struct{}) {
	current := make(map[netip.Addr]struct{})
	for _, prefix := range currentStaticTargets() {
		if prefix.Bits() != 128 {
			continue
		}
		current[prefix.Addr()] = struct{}{}
	}
	addresses := make([]netip.Addr, 0, len(current))
	for ip := range current {
		addresses = append(addresses, ip)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].Compare(addresses[j]) < 0 })
	for _, ip := range addresses {
		if _, ok := announced[ip]; ok {
			continue
		}
		if announceAddress(rt, sbuf, ip, "static") {
			announced[ip] = struct{}{}
		}
	}
	for ip := range announced {
		if _, ok := current[ip]; !ok {
			delete(announced, ip)
		}
	}
}

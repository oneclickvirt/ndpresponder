//go:build linux

package main

import (
	"context"

	"github.com/gopacket/gopacket"
	"go.uber.org/zap"
)

func runResponder(ctx context.Context) error {
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

	solicitations := CaptureNeighSolicitation(rt.handle)
	sbuf := gopacket.NewSerializeBuffer()
	for {
		select {
		case <-ctx.Done():
			return nil

		case ns, ok := <-solicitations:
			if !ok {
				return nil
			}
			logEntry := logger.With(zap.Stringer("ns", ns))
			switch {
			case activeIPs.Load().Contains(ns.TargetIP):
				logEntry = logEntry.With(zap.String("reason", "runtime"))
			case targetSubnets.Contains(ns.TargetIP):
				logEntry = logEntry.With(zap.String("reason", "static"))
			default:
				logEntry.Debug("IGNORE")
				continue
			}

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

//go:build linux

package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/gopacket/gopacket/afpacket"
	"go.uber.org/zap"
)

type responderRuntime struct {
	netif  *net.Interface
	hi     HostInfo
	handle *afpacket.TPacket
	reason string
}

func prepareResponder(candidates []interfaceCandidate) (*responderRuntime, error) {
	return prepareResponderWith(candidates, tryResponderCandidate)
}

func prepareResponderWith(candidates []interfaceCandidate, initialize func(interfaceCandidate) (*responderRuntime, error)) (*responderRuntime, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidate network interfaces")
	}

	var ready []*responderRuntime
	var failures []string
	for _, candidate := range candidates {
		rt, err := initialize(candidate)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(%s): %v", candidate.Interface.Name, candidate.Reason, err))
			logger.Warn("candidate network interface failed",
				zap.String("ifname", candidate.Interface.Name),
				zap.String("reason", candidate.Reason),
				zap.Error(err),
			)
			continue
		}
		ready = append(ready, rt)

		// An explicitly requested interface is authoritative once it can be
		// initialized. A delegated bridge may not have an IPv6 default route of
		// its own, while the host's default route belongs to another interface.
		if strings.Contains(candidate.Reason, "requested") {
			return finalizeResponderRuntime(rt, ready)
		}

		// For automatic detection, a candidate with an IPv6 default gateway is
		// preferred and opening every container veth only delays startup.
		if rt.hi.GatewayIP.IsValid() {
			return finalizeResponderRuntime(rt, ready)
		}
	}

	if len(ready) == 0 {
		return nil, fmt.Errorf("no candidate network interface could be initialized: %s", strings.Join(failures, "; "))
	}

	return finalizeResponderRuntime(chooseResponderRuntime(ready), ready)
}

func finalizeResponderRuntime(selected *responderRuntime, ready []*responderRuntime) (*responderRuntime, error) {
	for _, rt := range ready {
		if rt == selected {
			continue
		}
		if rt.handle != nil {
			rt.handle.Close()
		}
		logger.Info("candidate network interface initialized but not selected",
			zap.String("ifname", rt.netif.Name),
			zap.String("reason", rt.reason),
			zap.Stringer("gateway", rt.hi.GatewayIP),
		)
	}

	logger.Info("selected responder interface",
		zap.String("ifname", selected.netif.Name),
		zap.Int("index", selected.netif.Index),
		zap.Stringer("mac", selected.netif.HardwareAddr),
		zap.String("flags", selected.netif.Flags.String()),
		zap.String("reason", selected.reason),
		zap.Stringer("gateway", selected.hi.GatewayIP),
	)
	return selected, nil
}

func tryResponderCandidate(candidate interfaceCandidate) (*responderRuntime, error) {
	ifi := candidate.Interface
	h, err := afpacket.NewTPacket(afpacket.OptInterface(ifi.Name))
	if err != nil {
		return nil, err
	}

	if err := h.SetBPF(bpfFilter); err != nil {
		h.Close()
		return nil, err
	}

	hi, err := gatherHostInfo(&ifi)
	if err != nil {
		h.Close()
		return nil, err
	}

	return &responderRuntime{
		netif:  &ifi,
		hi:     hi,
		handle: h,
		reason: candidate.Reason,
	}, nil
}

func chooseResponderRuntime(ready []*responderRuntime) *responderRuntime {
	selected := ready[0]
	for _, rt := range ready {
		if rt.hi.GatewayIP.IsValid() {
			return rt
		}
	}
	return selected
}

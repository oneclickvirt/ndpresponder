package main

import (
	"net/netip"
	"sync/atomic"

	docker "github.com/fsouza/go-dockerclient"
	"go.uber.org/zap"
	"go4.org/netipx"
)

var (
	dockerLogger    = logger.Named("Docker")
	dockerNetworks  []string
	dockerClient    *docker.Client
	dockerNetIPSets = map[string]*netipx.IPSet{}
	dockerNewIP     = make(chan netip.Addr, 64)
)

var dockerActiveIPs atomic.Pointer[netipx.IPSet]

func init() {
	dockerActiveIPs.Store(new(netipx.IPSet))
}

func dockerListen() (e error) {
	if dockerClient, e = docker.NewClientFromEnv(); e != nil {
		return e
	}
	events := make(chan *docker.APIEvents, 64)
	if e = dockerClient.AddEventListenerWithOptions(docker.EventsOptions{
		Filters: map[string][]string{
			"type":    {"network"},
			"event":   {"connect", "disconnect"},
			"network": dockerNetworks,
		},
	}, events); e != nil {
		return e
	}

	for _, network := range dockerNetworks {
		dockerRefreshNetwork(network, func(string) bool { return true })
	}

	go func() {
		for evt := range events {
			ctID := evt.Actor.Attributes["container"]
			dockerRefreshNetwork(evt.Actor.Attributes["name"],
				func(ct string) bool { return ct == ctID })
		}
	}()

	return nil
}

func dockerRefreshNetwork(name string, isNewContainer func(ctID string) bool) {
	network, e := dockerClient.NetworkInfo(name)
	if e != nil {
		dockerLogger.Warn("NetworkInfo error", zap.Error(e))
		return
	}

	var b netipx.IPSetBuilder
	var ipAddrs []string
	var newIPs []netip.Addr
	for ctID, ct := range network.Containers {
		prefix, _ := netip.ParsePrefix(ct.IPv6Address)
		ip := prefix.Addr()
		b.Add(ip)
		ipAddrs = append(ipAddrs, ip.String())

		if isNewContainer(ctID) {
			newIPs = append(newIPs, ip)
		}
	}
	dockerLogger.Info("active IPs updated",
		zap.String("network", network.Name),
		zap.Strings("ip", ipAddrs),
	)
	dockerNetIPSets[network.ID], _ = b.IPSet()

	for net, ipset := range dockerNetIPSets {
		if net != network.ID {
			b.AddSet(ipset)
		}
	}
	newActiveIPs, _ := b.IPSet()
	dockerActiveIPs.Store(newActiveIPs)

	for _, ip := range newIPs {
		dockerNewIP <- ip
	}
}

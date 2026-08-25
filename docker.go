//go:build !dragonfly

package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"go.uber.org/zap"
)

var (
	dockerLogger   = logger.Named("Docker")
	dockerNetworks []string
	dockerClient   *docker.Client
)

const (
	dockerAPIProbeTimeout = 2 * time.Second
	dockerRefreshInterval = 30 * time.Second
)

func dockerListen(ctx context.Context) (e error) {
	if dockerClient, e = docker.NewClientFromEnv(); e != nil {
		return fmt.Errorf("create Docker-compatible API client: %w", e)
	}
	probeCtx, cancel := context.WithTimeout(ctx, dockerAPIProbeTimeout)
	defer cancel()
	if e = dockerClient.PingWithContext(probeCtx); e != nil {
		return fmt.Errorf("ping Docker-compatible API: %w", e)
	}
	// Populate the first address snapshot before a platform-specific responder
	// selects its uplink. A running responder with no usable API cannot answer
	// for runtime addresses, so treat an initial inspection failure as fatal.
	if e = dockerRefreshConfiguredNetworks(); e != nil {
		return fmt.Errorf("inspect configured Docker-compatible networks: %w", e)
	}
	go dockerEventLoop(ctx)
	return nil
}

// dockerEventLoop runs the Docker network event listener and automatically
// reconnects if the event stream is closed (e.g. Docker daemon restart).
func dockerEventLoop(ctx context.Context) {
	const reconnectDelay = 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := dockerRefreshConfiguredNetworks(); err != nil {
			dockerLogger.Warn("refresh configured networks failed", zap.Error(err))
		}

		events := make(chan *docker.APIEvents, 64)
		if e := dockerClient.AddEventListenerWithOptions(docker.EventsOptions{
			Filters: map[string][]string{
				"type":    {"network"},
				"event":   {"connect", "disconnect"},
				"network": dockerNetworks,
			},
		}, events); e != nil {
			dockerLogger.Warn("AddEventListener error, will retry",
				zap.Error(e), zap.Duration("delay", reconnectDelay))
			if !waitForContext(ctx, reconnectDelay) {
				return
			}
			continue
		}

		refreshTicker := time.NewTicker(dockerRefreshInterval)
		listening := true
		for listening {
			select {
			case <-ctx.Done():
				refreshTicker.Stop()
				_ = dockerClient.RemoveEventListener(events)
				return
			case _, ok := <-events:
				if !ok {
					listening = false
					continue
				}
				// Podman and Docker do not expose identical event attributes.
				// Refreshing configured networks avoids relying on either schema.
				if err := dockerRefreshConfiguredNetworks(); err != nil {
					dockerLogger.Warn("refresh configured networks failed", zap.Error(err))
				}
			case <-refreshTicker.C:
				if err := dockerRefreshConfiguredNetworks(); err != nil {
					dockerLogger.Warn("refresh configured networks failed", zap.Error(err))
				}
			}
		}
		refreshTicker.Stop()
		_ = dockerClient.RemoveEventListener(events)

		dockerLogger.Warn("Docker event stream closed, reconnecting",
			zap.Duration("delay", reconnectDelay))
		if !waitForContext(ctx, reconnectDelay) {
			return
		}
	}
}

func waitForContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func dockerRefreshConfiguredNetworks() error {
	return refreshDockerNetworks(dockerNetworks, dockerRefreshNetwork)
}

func refreshDockerNetworks(networks []string, refresh func(string) error) error {
	seen := make(map[string]struct{}, len(networks))
	var refreshErrors []error
	for _, network := range networks {
		if _, alreadyRefreshed := seen[network]; alreadyRefreshed {
			continue
		}
		seen[network] = struct{}{}
		if err := refresh(network); err != nil {
			refreshErrors = append(refreshErrors, err)
		}
	}
	return errors.Join(refreshErrors...)
}

func dockerRefreshNetwork(name string) error {
	source := dockerIPSource(name)
	network, e := dockerClient.NetworkInfo(name)
	if e != nil {
		dockerLogger.Warn("NetworkInfo error", zap.Error(e))
		// A removed network must stop being advertised. This also mirrors CNI's
		// behavior when its lease directory is no longer present.
		replaceActiveIPSource(source, nil)
		return fmt.Errorf("inspect network %q: %w", name, e)
	}
	if network == nil {
		replaceActiveIPSource(source, nil)
		return fmt.Errorf("inspect network %q: empty response", name)
	}

	var ipAddrs []string
	var addresses []netip.Addr
	for _, ct := range network.Containers {
		if ct.IPv6Address == "" {
			continue
		}
		ip, ok := runtimeIPv6Address(ct.IPv6Address)
		if !ok {
			continue
		}
		addresses = append(addresses, ip)
		ipAddrs = append(ipAddrs, ip.String())
	}
	dockerLogger.Info("active IPs updated",
		zap.String("network", network.Name),
		zap.Strings("ip", ipAddrs),
	)
	replaceActiveIPSource(source, addresses)
	return nil
}

// dockerIPSource is intentionally keyed by the configured network name (or
// ID supplied through -N), so an API lookup failure can clear the same source
// without requiring a now-unavailable NetworkInfo response.
func dockerIPSource(name string) string {
	return "docker:" + name
}

// runtimeIPv6Address accepts Docker's address/CIDR form and Podman versions
// that expose a bare IPv6 address through the Docker-compatible API.
func runtimeIPv6Address(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		address := prefix.Addr().Unmap()
		return address, address.Is6()
	}
	if address, err := netip.ParseAddr(value); err == nil {
		address = address.Unmap()
		return address, address.Is6()
	}
	return netip.Addr{}, false
}

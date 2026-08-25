package main

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

const defaultCNIDataDir = "/var/lib/cni/networks"

var (
	cniNetworks []string
	cniDataDir  = defaultCNIDataDir
)

func cniListen(ctx context.Context) error {
	dirs, err := cniLeaseDirectories(cniNetworks, cniDataDir)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		cniRefreshNetwork(dir)
	}
	go cniRefreshLoop(ctx, dirs)
	return nil
}

func cniRefreshLoop(ctx context.Context, dirs []string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, dir := range dirs {
			cniRefreshNetwork(dir)
		}
	}
}

func cniRefreshNetwork(dir string) {
	addresses, err := readCNILeaseAddresses(dir)
	if err != nil {
		dockerLogger.Warn("read CNI lease directory failed", zap.String("dir", dir), zap.Error(err))
		// An unreadable lease source cannot be trusted. Keep behavior aligned
		// with a Docker API lookup failure and stop advertising stale addresses.
		replaceActiveIPSource("cni:"+dir, nil)
		return
	}
	replaceActiveIPSource("cni:"+dir, addresses)
}

func cniLeaseDirectories(networks []string, dataDir string) ([]string, error) {
	if len(networks) == 0 {
		return nil, nil
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = defaultCNIDataDir
	}

	dirs := make([]string, 0, len(networks))
	seen := make(map[string]struct{})
	for _, network := range networks {
		network = strings.TrimSpace(network)
		if network == "" {
			return nil, fmt.Errorf("CNI network name cannot be empty")
		}
		var dir string
		if filepath.IsAbs(network) {
			dir = filepath.Clean(network)
		} else {
			if filepath.Base(network) != network || network == "." || network == ".." {
				return nil, fmt.Errorf("invalid CNI network name %q", network)
			}
			dir = filepath.Join(dataDir, network)
		}
		if _, ok := seen[dir]; !ok {
			seen[dir] = struct{}{}
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

func readCNILeaseAddresses(dir string) ([]netip.Addr, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	addresses := make([]netip.Addr, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		address, err := netip.ParseAddr(entry.Name())
		if err == nil && address.Is6() {
			addresses = append(addresses, address.Unmap())
		}
	}
	return addresses, nil
}

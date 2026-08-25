package main

import (
	"bufio"
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"

	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go4.org/netipx"
)

var logger = func() *zap.Logger {
	var lvl zapcore.Level
	if environ, ok := os.LookupEnv("NDPRESPONDER_LOG"); ok {
		lvl.Set(environ)
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		os.Stderr,
		lvl,
	)
	return zap.New(core)
}()

var (
	targetSubnets       *netipx.IPSet
	staticTargets       []netip.Prefix
	staticCLITargets    []netip.Prefix
	interfaceCandidates []interfaceCandidate
	staticTargetFile    string
	staticTargetMu      sync.RWMutex
)

var app = &cli.App{
	Name:        "ndpresponder",
	Description: "IPv6 Neighbor Discovery responder (Linux) and proxy manager (macOS/BSD)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "ifname",
			Aliases: []string{"i"},
			Usage:   "uplink network interface, or auto to detect it",
			Value:   autoIfname,
		},
		&cli.StringSliceFlag{
			Name:    "subnet",
			Aliases: []string{"n"},
			Usage:   "static IPv6 target subnet (/128 targets only on macOS and BSD proxy hosts)",
		},
		&cli.StringFlag{
			Name:  "target-file",
			Usage: "file containing one IPv6 address or prefix per line; reloaded while running",
		},
		&cli.StringSliceFlag{
			Name:    "docker-network",
			Aliases: []string{"N"},
			Usage:   "Docker-API-compatible network name (Docker or rootful Podman)",
		},
		&cli.StringSliceFlag{
			Name:    "cni-network",
			Aliases: []string{"C"},
			Usage:   "CNI host-local network name or lease directory (for example containerd or nerdctl)",
		},
		&cli.StringFlag{
			Name:  "cni-data-dir",
			Value: "/var/lib/cni/networks",
			Usage: "base directory for named CNI host-local leases",
		},
	},
	HideHelpCommand: true,
	Before: func(c *cli.Context) (e error) {
		staticTargets = nil
		staticCLITargets = nil
		staticTargetFile = strings.TrimSpace(c.String("target-file"))
		for _, subnet := range c.StringSlice("subnet") {
			prefix, e := parseStaticTarget(subnet)
			if e != nil {
				return cli.Exit(e, 1)
			}
			staticCLITargets = append(staticCLITargets, prefix)
		}
		if staticTargetFile != "" {
			prefixes, err := readStaticTargetFile(staticTargetFile)
			if err != nil {
				return cli.Exit(err, 1)
			}
			staticTargets = append(append([]netip.Prefix(nil), staticCLITargets...), prefixes...)
		}
		var staticBuilder netipx.IPSetBuilder
		if staticTargetFile == "" {
			staticTargets = append([]netip.Prefix(nil), staticCLITargets...)
		}
		if err := validateStaticTargetsForPlatform(staticTargets); err != nil {
			return cli.Exit(err, 1)
		}
		for _, prefix := range staticTargets {
			staticBuilder.AddPrefix(prefix)
		}
		targetSubnets, e = staticBuilder.IPSet()
		if e != nil {
			return cli.Exit(e, 1)
		}

		dockerNetworks = c.StringSlice("docker-network")
		cniNetworks = c.StringSlice("cni-network")
		cniDataDir = c.String("cni-data-dir")

		if interfaceCandidates, e = resolveInterfaceCandidates(c.String("ifname")); e != nil {
			return cli.Exit(e, 1)
		}

		return nil
	},
	Action: func(c *cli.Context) error {
		ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
		defer stop()
		if err := runResponder(ctx); err != nil {
			return cli.Exit(err, 1)
		}
		return nil
	},
}

func parseStaticTarget(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	prefix, err := netip.ParsePrefix(value)
	if err == nil {
		if !prefix.Addr().Is6() {
			return netip.Prefix{}, fmt.Errorf("static target %q is not an IPv6 address or prefix", value)
		}
		return prefix.Masked(), nil
	}
	address, addressErr := netip.ParseAddr(value)
	if addressErr == nil && address.Is6() {
		return netip.PrefixFrom(address, 128), nil
	}
	return netip.Prefix{}, fmt.Errorf("invalid static target %q: %w", value, err)
}

func validateStaticTargetsForPlatform(prefixes []netip.Prefix) error {
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "netbsd", "openbsd":
		for _, prefix := range prefixes {
			if prefix.Bits() != 128 {
				return fmt.Errorf("macOS and BSD hosts support static NDP proxy targets only as /128 addresses; use -N or -C for dynamic runtime addresses")
			}
		}
	}
	return nil
}

func readStaticTargetFile(path string) ([]netip.Prefix, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open static target file %q: %w", path, err)
	}
	defer file.Close()

	var prefixes []netip.Prefix
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		prefix, parseErr := parseStaticTarget(value)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid static target file %q line %d: %w", path, line, parseErr)
		}
		prefixes = append(prefixes, prefix)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read static target file %q: %w", path, err)
	}
	return prefixes, nil
}

func reloadStaticTargetFile() {
	if staticTargetFile == "" {
		return
	}
	prefixes, err := readStaticTargetFile(staticTargetFile)
	if err != nil {
		logger.Warn("reload static target file failed", zap.String("file", staticTargetFile), zap.Error(err))
		return
	}
	var builder netipx.IPSetBuilder
	merged := append(append([]netip.Prefix(nil), staticCLITargets...), prefixes...)
	if err := validateStaticTargetsForPlatform(merged); err != nil {
		logger.Warn("reload static target file rejected", zap.String("file", staticTargetFile), zap.Error(err))
		return
	}
	for _, prefix := range merged {
		builder.AddPrefix(prefix)
	}
	set, err := builder.IPSet()
	if err != nil {
		logger.Warn("build static target set failed", zap.String("file", staticTargetFile), zap.Error(err))
		return
	}
	staticTargetMu.Lock()
	targetSubnets = set
	staticTargets = merged
	staticTargetMu.Unlock()
}

func currentStaticTargetSet() *netipx.IPSet {
	staticTargetMu.RLock()
	defer staticTargetMu.RUnlock()
	return targetSubnets
}

func currentStaticTargets() []netip.Prefix {
	staticTargetMu.RLock()
	defer staticTargetMu.RUnlock()
	return append([]netip.Prefix(nil), staticTargets...)
}

func main() {
	if err := app.Run(os.Args); err != nil {
		logger.Error("ndpresponder failed", zap.Error(err))
		os.Exit(1)
	}
}

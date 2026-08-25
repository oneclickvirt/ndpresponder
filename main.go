package main

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strings"

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
	interfaceCandidates []interfaceCandidate
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
		var ipset netipx.IPSetBuilder
		staticTargets = nil
		for _, subnet := range c.StringSlice("subnet") {
			prefix, e := netip.ParsePrefix(strings.TrimSpace(subnet))
			if e != nil || !prefix.Addr().Is6() {
				if e == nil {
					e = fmt.Errorf("static target %q is not an IPv6 prefix", subnet)
				}
				return cli.Exit(e, 1)
			}
			prefix = prefix.Masked()
			ipset.AddPrefix(prefix)
			staticTargets = append(staticTargets, prefix)
		}
		targetSubnets, e = ipset.IPSet()
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

func main() {
	if err := app.Run(os.Args); err != nil {
		logger.Error("ndpresponder failed", zap.Error(err))
		os.Exit(1)
	}
}

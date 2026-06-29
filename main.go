package main

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/afpacket"
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
	netif               *net.Interface
	targetSubnets       *netipx.IPSet
	interfaceCandidates []interfaceCandidate
	handle              *afpacket.TPacket
)

var app = &cli.App{
	Name:        "ndpresponder",
	Description: "IPv6 Neighbor Discovery Responder",
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
			Usage:   "static target subnet",
		},
		&cli.StringSliceFlag{
			Name:    "docker-network",
			Aliases: []string{"N"},
			Usage:   "Docker network name",
		},
	},
	HideHelpCommand: true,
	Before: func(c *cli.Context) (e error) {
		var ipset netipx.IPSetBuilder
		for _, subnet := range c.StringSlice("subnet") {
			prefix, e := netip.ParsePrefix(subnet)
			if e != nil {
				return cli.Exit(e, 1)
			}
			ipset.AddPrefix(prefix)
		}
		targetSubnets, e = ipset.IPSet()
		if e != nil {
			return cli.Exit(e, 1)
		}

		dockerNetworks = c.StringSlice("docker-network")

		if interfaceCandidates, e = resolveInterfaceCandidates(c.String("ifname")); e != nil {
			return cli.Exit(e, 1)
		}

		return nil
	},
	Action: func(c *cli.Context) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		rt, e := prepareResponder(interfaceCandidates)
		if e != nil {
			return cli.Exit(e, 1)
		}
		netif = rt.netif
		hi := rt.hi
		handle = rt.handle
		solicitations := CaptureNeighSolicitation(handle)

		if len(dockerNetworks) > 0 {
			if e = dockerListen(); e != nil {
				return cli.Exit(e, 1)
			}
		}

		sbuf := gopacket.NewSerializeBuffer()
	L:
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
				case dockerActiveIPs.Load().Contains(ns.TargetIP):
					logEntry = logEntry.With(zap.String("reason", "docker"))
				case targetSubnets.Contains(ns.TargetIP):
					logEntry = logEntry.With(zap.String("reason", "static"))
				default:
					logEntry.Debug("IGNORE")
					continue L
				}

				if e := ns.Respond(sbuf, hi); e != nil {
					logEntry.Warn("RESPOND error", zap.Error(e))
					continue L
				}
				logEntry.Info("RESPOND")
				if err := handle.WritePacketData(sbuf.Bytes()); err != nil {
					logEntry.Warn("WritePacketData error", zap.Error(err))
				}

			case ip := <-dockerNewIP:
				logEntry := logger.With(zap.Stringer("ip", ip))
				if e := Gratuitous(sbuf, hi, ip); e != nil {
					logEntry.Warn("GRATUITOUS error", zap.Error(e))
					continue L
				}
				logEntry.Info("GRATUITOUS")
				if err := handle.WritePacketData(sbuf.Bytes()); err != nil {
					logEntry.Warn("WritePacketData error", zap.Error(err))
				}

				if !hi.GatewayIP.IsValid() {
					break
				}
				if e := Solicit(sbuf, hi, ip); e != nil {
					logEntry.Warn("SOLICIT error", zap.Error(e))
					continue L
				}
				logEntry.Info("SOLICIT")
				if err := handle.WritePacketData(sbuf.Bytes()); err != nil {
					logEntry.Warn("WritePacketData error", zap.Error(err))
				}
			}
		}
	},
	After: func(c *cli.Context) error {
		if handle != nil {
			handle.Close()
		}
		return nil
	},
}

func main() {
	if err := app.Run(os.Args); err != nil {
		os.Exit(1)
	}
}

# IPv6 Neighbor Discovery Responder

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/oneclickvirt/ndpresponder/build.yml)](https://github.com/oneclickvirt/ndpresponder/actions) [![GitHub code size](https://img.shields.io/github/languages/code-size/oneclickvirt/ndpresponder?style=flat&logo=GitHub)](https://github.com/oneclickvirt/ndpresponder)

[中文说明](README_CN.md)

**ndpresponder** exposes container or static IPv6 addresses through [RFC 4861](https://tools.ietf.org/html/rfc4861) Neighbor Discovery.

On Linux, it listens for ICMPv6 neighbor solicitations on the uplink and sends neighbor advertisements directly. On macOS and supported BSD hosts, where Linux packet sockets are unavailable, it manages the operating system's `ndp` proxy entries instead.

The source IPv6 address of the neighbor advertisement is set to the same value as the target address in the neighbor solicitation.
This enables **ndpresponder** to work in certain KVM virtual servers where NDP uses link-local addresses but *ebtables* drops outgoing packets from link-local addresses.

Both unicast (`is-alive`) and multicast (`who-has`) neighbor solicitations are handled, so IPv6 addresses remain reachable even after the router's neighbor cache expires.

## Platform Support

| Host platform | Static addresses | Docker / Podman API mode | CNI host-local mode |
| --- | --- | --- | --- |
| Linux | IPv6 prefix | Supported | Supported |
| macOS | Individual `/128` addresses | Supported only when the runtime addresses are routed through the physical macOS uplink | Supported only when the lease addresses are routed through the physical macOS uplink |
| FreeBSD, NetBSD, OpenBSD | Individual `/128` addresses | Supported only when a Docker-compatible runtime and its addresses are routed through the physical uplink | Supported only when the lease addresses are routed through the physical uplink |
| DragonFly BSD | Individual `/128` addresses | Not supported by the upstream Docker client dependency | Supported only when the lease addresses are routed through the physical uplink |
| Other platforms | Not supported | Not supported | Not supported |

macOS and supported BSD proxy entries require `root`, a physical uplink with an IPv6 route to the target address, and the system `ndp` and `route` commands. `ndpresponder` never overwrites ordinary NDP neighbors or proxy entries created by another service. An ordinary entry, or a proxy on another interface, is reported as a startup or synchronization error instead of being mistaken for a working proxy.

Docker Desktop normally runs containers in a Linux VM behind NAT. Those VM-internal addresses are not on the macOS physical link, so creating macOS NDP entries does **not** make them publicly reachable. Use a Linux host or VM with a routed IPv6 prefix for that use case.

## Installation

This program is written in Go. Compile and install with:

```bash
env CGO_ENABLED=0 go install github.com/oneclickvirt/ndpresponder@main
```

Also available as a Docker container:

```bash
docker build -t localhost/ndpresponder 'github.com/oneclickvirt/ndpresponder#main'
docker run -d --name ndpresponder --network host localhost/ndpresponder [arguments]
```

The Linux image publishing contract uses `spiritlhl/ndpresponder_x86:latest`
for `linux/amd64` and `spiritlhl/ndpresponder_aarch64:latest` for
`linux/arm64`. The CI verifies each OCI config's platform before publishing
these tags from the main branch when the `DOCKERHUB_TOKEN` repository secret
is configured.

Use the native binary for macOS and BSD proxy mode. A Linux container cannot modify the host's NDP table on those platforms.

## Static Mode

The program can respond to neighbor solicitations for any address within one or more subnets.
Keep subnets as small as possible.

```bash
sudo ndpresponder -n 2001:db8:3988:486e:ff2f:add3:31e3:7b00/120
```

* `-i` optionally specifies the uplink network interface name. The default value is `auto`, which selects the interface from the IPv6 default route and falls back to scanning usable interfaces.
* `-n` specifies the IPv6 subnet to respond to. Repeat to add multiple subnets.

See [ndpresponder.service](ndpresponder.service) for a sample systemd unit file.

### macOS / BSD static mode

macOS and the supported BSD hosts use one proxy entry per target, so static targets must be `/128` addresses:

```bash
sudo ndpresponder -i <uplink> -n 2001:db8:3988:486e::100/128
```

Use the physical uplink, commonly `en0` on macOS and `em0`, `vtnet0`, or `vioif0` on BSD hosts. The target must resolve through the selected interface in the BSD IPv6 routing table. Before changing the NDP table, ndpresponder verifies that route and reports a mismatch without creating a proxy entry. With `-i auto`, known static and runtime targets are used to avoid selecting a Docker bridge ahead of the physical uplink; local bridge, tunnel, and VM interfaces are not eligible NDP proxy uplinks. The process removes only entries it created when it stops.

## Docker Network Mode

The program can respond to neighbor solicitations for addresses assigned in Docker networks.
When a container connects to a network, it notifies the gateway router of the new address.

```bash
docker network create --ipv6 --subnet=172.26.0.0/16 \
  --subnet=2001:db8:1972:beb0:dce3:9c1a:d150::/112 ipv6exposed

docker run -d \
  --restart always --cpus 0.02 --memory 64M \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  --cap-drop=ALL --cap-add=NET_RAW --cap-add=NET_ADMIN \
  --network host --name ndpresponder \
  localhost/ndpresponder -N ipv6exposed
```

* `-i` optionally specifies the uplink network interface name. The default value is `auto`, which avoids assuming that the host interface is named `eth0`.
* `-N` specifies the Docker network name. Repeat to add multiple networks.

### Podman API mode

Rootful Podman exposes a Docker-compatible API socket. Start its socket service and point `DOCKER_HOST` at it:

```bash
sudo systemctl start podman.socket
sudo DOCKER_HOST=unix:///run/podman/podman.sock ndpresponder -N podman-ipv6
```

When running ndpresponder in a container, mount that socket read-only at `/var/run/docker.sock` and set `DOCKER_HOST=unix:///var/run/docker.sock`. The Podman installer does this automatically. At startup, ndpresponder verifies the Docker-compatible API and every requested network; an unavailable socket or network is a startup failure rather than a false-ready responder. Rootless Podman sockets and slirp/pasta networking do not provide a host-routed public IPv6 bridge, so they are outside this mode's scope. Podman Machine on macOS has the same VM-boundary limitation as Docker Desktop; use a Linux host or VM with the routed prefix instead.

## CNI Host-Local Network Mode

For runtimes using the CNI `host-local` IPAM plugin, ndpresponder can watch the lease files instead of requiring a Docker API. This covers common rootful Containerd and nerdctl setups.

```bash
sudo ndpresponder -i eth0 -C containerd-ipv6
```

`-C` accepts a CNI network name below `/var/lib/cni/networks`, or an absolute lease directory. Use `--cni-data-dir` when the runtime stores leases elsewhere:

```bash
sudo ndpresponder -i eth0 \
  --cni-data-dir /custom/cni/networks \
  -C containerd-ipv6
```

Only IPv6 lease filenames are announced. The lease directory should be mounted read-only when ndpresponder itself runs in a container.

## Other Options

Set the `NDPRESPONDER_LOG` environment variable to change the log level.
Acceptable values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.

```bash
sudo NDPRESPONDER_LOG=WARN ndpresponder [arguments]
docker run -e NDPRESPONDER_LOG=WARN [other arguments]
```

## Acknowledgements

This project is based on the original work by [yoursunny/ndpresponder](https://github.com/yoursunny/ndpresponder). Thanks for the great foundation.

# IPv6 Neighbor Discovery Responder

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/oneclickvirt/ndpresponder/build.yml)](https://github.com/oneclickvirt/ndpresponder/actions) [![GitHub code size](https://img.shields.io/github/languages/code-size/oneclickvirt/ndpresponder?style=flat&logo=GitHub)](https://github.com/oneclickvirt/ndpresponder)

[中文说明](README_CN.md)

**ndpresponder** is a Go program that listens for ICMPv6 neighbor solicitations on a network interface and responds with neighbor advertisements, as described in [RFC 4861](https://tools.ietf.org/html/rfc4861) - IPv6 Neighbor Discovery Protocol.

The source IPv6 address of the neighbor advertisement is set to the same value as the target address in the neighbor solicitation.
This enables **ndpresponder** to work in certain KVM virtual servers where NDP uses link-local addresses but *ebtables* drops outgoing packets from link-local addresses.

Both unicast (`is-alive`) and multicast (`who-has`) neighbor solicitations are handled, so IPv6 addresses remain reachable even after the router's neighbor cache expires.

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

## Static Mode

The program can respond to neighbor solicitations for any address within one or more subnets.
Keep subnets as small as possible.

```bash
sudo ndpresponder -n 2001:db8:3988:486e:ff2f:add3:31e3:7b00/120
```

* `-i` optionally specifies the uplink network interface name. The default value is `auto`, which selects the interface from the IPv6 default route and falls back to scanning usable interfaces.
* `-n` specifies the IPv6 subnet to respond to. Repeat to add multiple subnets.

See [ndpresponder.service](ndpresponder.service) for a sample systemd unit file.

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

## Other Options

Set the `NDPRESPONDER_LOG` environment variable to change the log level.
Acceptable values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.

```bash
sudo NDPRESPONDER_LOG=WARN ndpresponder [arguments]
docker run -e NDPRESPONDER_LOG=WARN [other arguments]
```

## Acknowledgements

This project is based on the original work by [yoursunny/ndpresponder](https://github.com/yoursunny/ndpresponder). Thanks for the great foundation.


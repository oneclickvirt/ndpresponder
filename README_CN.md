# IPv6 邻居发现响应程序

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/oneclickvirt/ndpresponder/build.yml)](https://github.com/oneclickvirt/ndpresponder/actions) [![GitHub code size](https://img.shields.io/github/languages/code-size/oneclickvirt/ndpresponder?style=flat&logo=GitHub)](https://github.com/oneclickvirt/ndpresponder)

[English](README.md)

**ndpresponder** 用于通过 [RFC 4861](https://tools.ietf.org/html/rfc4861) 邻居发现协议暴露容器或静态 IPv6 地址。

在 Linux 上，它会监听上联网卡的 ICMPv6 邻居请求并直接发送邻居通告；在 macOS 和受支持的 BSD 宿主机上无法使用 Linux 的报文套接字，因此改为管理系统自带 `ndp` 的代理邻居条目。

邻居通告的源 IPv6 地址与邻居请求中的目标地址保持一致，使 **ndpresponder** 能够在某些 KVM 虚拟服务器上正常工作——这类环境中 NDP 使用链路本地地址，但 *ebtables* 会丢弃来自链路本地地址的出站数据包。

程序同时响应单播（`is-alive`）和组播（`who-has`）邻居请求，确保路由器邻居缓存过期后 IPv6 地址仍然可达。

## 平台支持

| 宿主平台 | 静态地址 | Docker / Podman API 模式 | CNI host-local 模式 |
| --- | --- | --- | --- |
| Linux | IPv6 网段 | 支持 | 支持 |
| macOS | 单个 `/128` 地址 | 仅当运行时地址经 macOS 物理上联网卡路由时支持 | 仅当租约地址经 macOS 物理上联网卡路由时支持 |
| FreeBSD、NetBSD、OpenBSD | 单个 `/128` 地址 | 仅当存在 Docker 兼容运行时且地址经物理上联网卡路由时支持 | 仅当租约地址经物理上联网卡路由时支持 |
| DragonFly BSD | 单个 `/128` 地址 | 上游 Docker 客户端依赖不支持 | 仅当租约地址经物理上联网卡路由时支持 |
| 其他平台 | 不支持 | 不支持 | 不支持 |

macOS 和受支持 BSD 宿主机的代理条目需要 `root` 权限、可路由到目标地址的物理 IPv6 上联网卡，以及系统自带的 `ndp` 与 `route` 命令。程序不会覆盖普通 NDP 邻居条目，也不会覆盖其他服务创建的代理条目；若已存在普通邻居条目或位于其他接口的代理条目，会明确报启动或同步失败，避免被误判为可用代理。

Docker Desktop 通常在 NAT 后的 Linux 虚拟机中运行容器，其内部地址不在 macOS 的物理链路上。因此即使建立 macOS NDP 代理条目，也不能让这些地址直接获得公网可达性；此场景请使用具有路由 IPv6 前缀的 Linux 宿主机或虚拟机。

## 安装

本程序使用 Go 编写，编译安装命令：

```bash
env CGO_ENABLED=0 go install github.com/oneclickvirt/ndpresponder@main
```

也可使用 Docker 容器方式运行：

```bash
docker build -t localhost/ndpresponder 'github.com/oneclickvirt/ndpresponder#main'
docker run -d --name ndpresponder --network host localhost/ndpresponder [参数]
```

Linux 镜像发布约定为 `spiritlhl/ndpresponder_x86:latest` 对应
`linux/amd64`，`spiritlhl/ndpresponder_aarch64:latest` 对应
`linux/arm64`。仓库配置 `DOCKERHUB_TOKEN` secret 后，CI 会在主分支发布前
核验每个 OCI 镜像配置中的平台信息。

macOS 和 BSD 代理模式请使用原生二进制文件。Linux 容器无法在这些平台上修改宿主机的 NDP 表。

## 静态模式

程序可对一个或多个子网内的任意地址响应邻居请求，建议将子网范围尽量缩小。

```bash
sudo ndpresponder -n 2001:db8:3988:486e:ff2f:add3:31e3:7b00/120
```

* `-i` 可选指定上联网卡名称。默认值为 `auto`，程序会优先根据 IPv6 默认路由选择接口，失败时再扫描可用接口。
* `-n` 指定需要响应的 IPv6 子网，可重复使用以指定多个子网。

systemd 单元文件示例参见 [ndpresponder.service](ndpresponder.service)。

### macOS / BSD 静态模式

macOS 和受支持 BSD 宿主机会为每个目标安装一个代理条目，因此静态目标只能使用 `/128`：

```bash
sudo ndpresponder -i <uplink> -n 2001:db8:3988:486e::100/128
```

请填写物理上联网卡，macOS 常见为 `en0`，BSD 上常见为 `em0`、`vtnet0` 或 `vioif0`。目标地址必须在 BSD IPv6 路由表中经由所选接口转发。ndpresponder 会在修改 NDP 表前核验该路由；若接口不匹配会直接报错，不会创建代理条目。使用 `-i auto` 时，已知的静态和运行时目标会参与接口选择，避免 Docker bridge 先于物理上联网卡被选中；本地 bridge、隧道和虚拟机接口不会被当作 NDP 代理上联。进程退出时只会移除自身创建的条目。

## Docker 网络模式

程序可对 Docker 网络中已分配的容器地址响应邻居请求。容器连接到网络时，程序会向网关路由器通告新地址的存在。

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

* `-i` 可选指定上联网卡名称。默认值为 `auto`，避免假设宿主机接口一定叫 `eth0`。
* `-N` 指定 Docker 网络名称，可重复使用以指定多个网络。

### Podman API 模式

Rootful Podman 提供 Docker 兼容 API socket。启动 socket 服务并将 `DOCKER_HOST` 指向它：

```bash
sudo systemctl start podman.socket
sudo DOCKER_HOST=unix:///run/podman/podman.sock ndpresponder -N podman-ipv6
```

若以容器运行 ndpresponder，请将该 socket 只读挂载到 `/var/run/docker.sock`，并设置 `DOCKER_HOST=unix:///var/run/docker.sock`。Podman 安装脚本会自动完成此项。启动时会校验 Docker 兼容 API 和每个指定网络；socket 或网络不可用会直接启动失败，不会把空响应误报为 IPv6 已就绪。Rootless Podman 的 socket 及 slirp/pasta 网络并不提供宿主机路由的公网 IPv6 bridge，因此不属于此模式的适用范围。macOS 上的 Podman Machine 与 Docker Desktop 一样受虚拟机边界限制；应改用拥有已路由前缀的 Linux 宿主机或虚拟机。

## CNI Host-Local 网络模式

对于使用 CNI `host-local` IPAM 的运行时，ndpresponder 可直接监听租约文件，无需 Docker API，适用于常见的 rootful Containerd 和 nerdctl 部署。

```bash
sudo ndpresponder -i eth0 -C containerd-ipv6
```

`-C` 可以是 `/var/lib/cni/networks` 下的 CNI 网络名称，也可以是绝对租约目录。若运行时使用其他租约根目录，请使用 `--cni-data-dir`：

```bash
sudo ndpresponder -i eth0 \
  --cni-data-dir /custom/cni/networks \
  -C containerd-ipv6
```

程序只会通告文件名为 IPv6 地址的租约。若 ndpresponder 运行在容器中，应将租约目录以只读方式挂载。

## 其他选项

通过设置 `NDPRESPONDER_LOG` 环境变量调整日志级别，可选值为 `DEBUG`、`INFO`、`WARN`、`ERROR`、`FATAL`。

```bash
sudo NDPRESPONDER_LOG=WARN ndpresponder [参数]
docker run -e NDPRESPONDER_LOG=WARN [其他参数]
```

## 致谢

本项目基于 [yoursunny/ndpresponder](https://github.com/yoursunny/ndpresponder) 的原始工作改进而来，感谢其提供的优秀基础实现。

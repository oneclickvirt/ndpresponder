# IPv6 邻居发现响应程序

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/oneclickvirt/ndpresponder/build.yml)](https://github.com/oneclickvirt/ndpresponder/actions) [![GitHub code size](https://img.shields.io/github/languages/code-size/oneclickvirt/ndpresponder?style=flat&logo=GitHub)](https://github.com/oneclickvirt/ndpresponder)

[English](README.md)

**ndpresponder** 是一个 Go 程序，监听网络接口上的 ICMPv6 邻居请求（Neighbor Solicitation），并按照 [RFC 4861](https://tools.ietf.org/html/rfc4861) IPv6 邻居发现协议规范回复邻居通告（Neighbor Advertisement）。

邻居通告的源 IPv6 地址与邻居请求中的目标地址保持一致，使 **ndpresponder** 能够在某些 KVM 虚拟服务器上正常工作——这类环境中 NDP 使用链路本地地址，但 *ebtables* 会丢弃来自链路本地地址的出站数据包。

程序同时响应单播（`is-alive`）和组播（`who-has`）邻居请求，确保路由器邻居缓存过期后 IPv6 地址仍然可达。

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

## 静态模式

程序可对一个或多个子网内的任意地址响应邻居请求，建议将子网范围尽量缩小。

```bash
sudo ndpresponder -n 2001:db8:3988:486e:ff2f:add3:31e3:7b00/120
```

* `-i` 可选指定上联网卡名称。默认值为 `auto`，程序会优先根据 IPv6 默认路由选择接口，失败时再扫描可用接口。
* `-n` 指定需要响应的 IPv6 子网，可重复使用以指定多个子网。

systemd 单元文件示例参见 [ndpresponder.service](ndpresponder.service)。

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

## 其他选项

通过设置 `NDPRESPONDER_LOG` 环境变量调整日志级别，可选值为 `DEBUG`、`INFO`、`WARN`、`ERROR`、`FATAL`。

```bash
sudo NDPRESPONDER_LOG=WARN ndpresponder [参数]
docker run -e NDPRESPONDER_LOG=WARN [其他参数]
```

## 致谢

本项目基于 [yoursunny/ndpresponder](https://github.com/yoursunny/ndpresponder) 的原始工作改进而来，感谢其提供的优秀基础实现。

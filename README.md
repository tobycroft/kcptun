[English](README_EN.md) | 简体中文

# kcptun

基于 KCP 协议的 TCP 加速隧道：客户端把本地 TCP 连接复用到 KCP（UDP）上发往服务端，服务端再还原成 TCP 转发到目标服务。适合跨境线路、弱网、移动网络等高丢包高延迟场景。

- **抗丢包**：KCP ARQ + 快速重传 + Reed-Solomon 前向纠错
- **多路复用**：smux 在一条 KCP 会话上承载多条 TCP 流
- **安全**：预共享密钥 + 可选加密、压缩、QPP
- **稳定**：支持端口定时轮换，且不切断已建立的连接

## 一键部署服务端（推荐）

服务端推荐用 [kcptun-script](https://github.com/TuuzNet/kcptun-script) 一键脚本安装，自动完成下载、配置、守护和防火墙设置：

```sh
wget --no-check-certificate -O kcptun.sh https://github.com/tobycroft/shell-scripts/raw/master/kcptun/kcptun.sh
sh kcptun.sh
```

脚本会：

- 自动下载本仓库 Releases 中与当前架构匹配的服务端程序
- 交互式引导填写端口、加速目标、加密方式、加速模式等（全部有默认值，一路回车即可）
- 安装 Supervisor 并配置开机自启、崩溃自动拉起
- 自动放行 firewalld / iptables 的 UDP 端口
- 安装完成后输出**客户端 JSON 配置**和手机端参数字符串
- 支持多实例、在线更新、自定义版本安装

常用命令：

| 命令 | 说明 |
| --- | --- |
| `sh kcptun.sh` | 直接运行进入安装向导 |
| `sh kcptun.sh add` | 添加一个实例（多端口加速） |
| `sh kcptun.sh reconfig 1` | 重新配置实例 1 |
| `sh kcptun.sh show 1` | 查看实例 1 的详细配置 |
| `sh kcptun.sh log 1` | 查看实例 1 的日志 |
| `sh kcptun.sh del 1` | 删除实例 1 |
| `sh kcptun.sh update` | 检查并更新脚本与 Kcptun |
| `sh kcptun.sh uninstall` | 卸载 |

> 脚本需要 root 权限，支持 Debian/Ubuntu、CentOS/Fedora/RedHat 等主流发行版，仅支持 x86 / x86_64 架构。

## 手动部署

### 1. 下载

到 [Releases](https://github.com/tobycroft/kcptun/releases) 下载对应平台的文件：

| 平台 | 客户端 | 服务端 |
| --- | --- | --- |
| Linux x86_64 | `client_linux_amd64` | `server_linux_amd64` |
| Linux x86 | `client_linux_386` | `server_linux_386` |
| Linux arm64 | `client_linux_arm64` | `server_linux_arm64` |
| Linux arm | `client_linux_arm` | `server_linux_arm` |
| macOS Intel / Apple Silicon | `client_darwin_amd64` / `client_darwin_arm64` | 同左 |
| Windows x86_64 / x86 / arm64 | `client_windows_*.exe` | 同左 |
| FreeBSD x86_64 | `client_freebsd_amd64` | `server_freebsd_amd64` |

Linux x86 / x86_64 另外提供 `kcptun-linux-*.tar.gz` 压缩包（一键脚本使用，内含 client 与 server）。

### 2. 启动服务端

```sh
chmod +x server_linux_amd64
./server_linux_amd64 -c server-config.json
```

### 3. 启动客户端

```sh
chmod +x client_linux_amd64
./client_linux_amd64 -c client-config.json
```

### 4. 开始使用

客户端会在本地监听 `localaddr`（默认 `:12948`），把应用连接指到该地址即可。例如加速 SSH：

```sh
ssh -p 12948 user@127.0.0.1
```

流量路径：本地应用 → 客户端 `:12948` → KCP → 服务端 `:29900` → 目标 `127.0.0.1:22`。

### 5. 后台运行

临时测试可直接前台运行；长期运行建议使用上面的**一键脚本**（自带 Supervisor 守护）。手动部署时可用 systemd：

```ini
# /etc/systemd/system/kcptun.service
[Unit]
Description=kcptun server
After=network.target

[Service]
ExecStart=/usr/local/kcptun/server_linux_amd64 -c /usr/local/kcptun/server-config.json
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload && systemctl enable --now kcptun
```

## 配置示例

### 服务端 server-config.json

```json
{
  "listen": ":29900",
  "target": "127.0.0.1:22",
  "key": "very fast",
  "crypt": "aes",
  "mode": "fast",
  "mtu": 1350,
  "sndwnd": 1024,
  "rcvwnd": 1024,
  "datashard": 10,
  "parityshard": 3,
  "dscp": 0,
  "nocomp": true,
  "quiet": false,
  "tcp": false,
  "sockbuf": 67108864,
  "smuxbuf": 67108864,
  "streambuf": 16777216,
  "smuxver": 2,
  "keepalive": 10,
  "closewait": 30,
  "snmplog": "",
  "snmpperiod": 60,
  "pprof": false
}
```

### 客户端 client-config.json

```json
{
  "localaddr": ":12948",
  "remoteaddr": "vps.example.com:29900",
  "key": "very fast",
  "crypt": "aes",
  "mode": "fast",
  "mtu": 1350,
  "sndwnd": 1024,
  "rcvwnd": 1024,
  "datashard": 10,
  "parityshard": 3,
  "dscp": 0,
  "nocomp": true,
  "quiet": false,
  "tcp": false,
  "conn": 64,
  "autoexpire": 300,
  "autoexpiremax": 360,
  "scavengettl": 0,
  "sockbuf": 67108864,
  "smuxbuf": 67108864,
  "streambuf": 16777216,
  "smuxver": 2,
  "keepalive": 10,
  "closewait": 0,
  "snmplog": "",
  "snmpperiod": 60,
  "pprof": false
}
```

说明：

- 两端必须一致的参数：`key`、`crypt`、`datashard`、`parityshard`、`mode`（manual 模式下为 `nodelay/interval/resend/nc`）
- 窗口镜像：客户端 `sndwnd` = 服务端 `rcvwnd`，客户端 `rcvwnd` = 服务端 `sndwnd`
- 上面的 `autoexpire: 300` + `autoexpiremax: 360` 表示每 300~360 秒（随机）更换一次端口；不需要轮换时把两者都设为 `0` 即可固定端口

## 参数说明

### 服务端

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `listen` | string | `:29900` | 监听地址，`IP:29900` 为单端口，`IP:29900-30100` 为端口段 |
| `target` | string | `127.0.0.1:12948` | 加速目标，TCP 地址或 Unix Socket 路径 |
| `key` | string | `it's a secrect` | 预共享密钥，两端一致（也可用环境变量 `KCPTUN_KEY`） |
| `crypt` | string | `aes` | 加密方式：`aes`、`aes-128`、`aes-192`、`salsa20`、`blowfish`、`twofish`、`cast5`、`3des`、`tea`、`xtea`、`xor`、`sm4`、`none`、`null` |
| `mode` | string | `fast` | 加速模式：`fast3` / `fast2` / `fast` / `normal` / `manual` |
| `mtu` | int | `1350` | UDP 包最大传输单元 |
| `sndwnd` / `rcvwnd` | int | `1024` | 发送 / 接收窗口（包数量） |
| `datashard` / `parityshard` | int | `10` / `3` | Reed-Solomon 前向纠错分片与冗余，两端一致 |
| `dscp` | int | `0` | DSCP 差分服务代码点（6bit） |
| `nocomp` | bool | `false` | 关闭 Snappy 压缩 |
| `sockbuf` | int | `4194304` | 单 socket 收发缓冲区（字节） |
| `smuxver` | int | `1` | smux 版本，可选 `1` / `2` |
| `smuxbuf` | int | `4194304` | smux 会话级缓冲区（字节） |
| `streambuf` | int | `2097152` | smux v2+ 单流接收缓冲区（字节） |
| `keepalive` | int | `10` | 心跳间隔（秒） |
| `closewait` | int | `30` | 连接拆除前的等待时间（秒） |
| `snmplog` / `snmpperiod` | string / int | `""` / `60` | 流量统计输出文件与周期 |
| `pprof` | bool | `false` | 在 `:6060` 开启性能剖析 |
| `log` | string | `""` | 日志文件，默认输出到 stderr |
| `quiet` | bool | `false` | 屏蔽 stream open/close 日志 |
| `tcp` | bool | `false` | 同时监听 TCP 伪装栈（仅 Linux） |
| `qpp` / `qpp-count` | bool / int | `false` / `61` | QPP 量子置换垫层开关与垫层数量 |

### 客户端

客户端包含服务端的全部传输层参数（含义相同），另有：

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `localaddr` | string | `:12948` | 本地监听地址；不含 `:` 端口时视为 Unix Socket 路径 |
| `remoteaddr` | string | `vps:29900` | 服务端地址，支持 `IP:29900` 或 `IP:29900-30100` 端口段（每次拨号随机选端口） |
| `conn` | int | `1` | 建立到服务端的 KCP 连接数，本地连接按轮询分配 |
| `autoexpire` | int | `0` | 端口轮换周期（秒），0 表示关闭；同时是 `autoexpiremax` 的下限 |
| `autoexpiremax` | int | `0` | 轮换周期的随机上限（秒），0 表示不随机；需大于 `autoexpire` |
| `scavengettl` | int | `0` | 旧连接强制回收时限（秒），0 表示永不强制关闭（推荐） |
| `closewait` | int | `0` | 连接拆除前的等待时间（秒） |

## 丢包测试

客户端内置了一个基于 KCP OOB 的丢包测试模式，用来回答“线路到底丢不丢包、延迟稳不稳”。

### 原理

KCP 的可靠传输会把丢失的包重传补上，所以从业务流里看不到丢包；`ping`/`mtr` 测的又是 ICMP，而很多运营商对 ICMP 和 UDP 的策略并不相同。丢包测试使用 kcp-go 的 OOB（out-of-band）通道：

- OOB 包**不重传、不排序、不 ACK**，与真实 UDP 探测一致；
- 探测包复用 KCP 会话的加密、端口、MTU 与 FEC 参数，走的就是代理业务所用的同一条 UDP 链路；
- 测试分两个阶段依次进行：**先测下行**（服务端发送探测包，客户端统计收到的序号），**再测上行**（客户端发送探测包，服务端统计收到的数量并通过控制包回报），两个方向的丢包率互相独立；
- 上行阶段的探测包由服务端原样回射，客户端据此计算 RTT 与抖动。

因此它测到的是“KCP 会话所走 UDP 链路的真实表现”，不会被重传掩盖。

### 前提条件

| 条件 | 说明 |
| --- | --- |
| 版本 | 服务端与客户端都需使用支持 `losstest` 的新版本，且两端版本一致 |
| FEC | 必须开启，`datashard` 与 `parityshard` 均大于 0 |
| 客户端 | 需要 CLI 版客户端；手机端 App 暂不支持 |
| 端口 | 与服务端监听端口相同，无需额外放行 |

服务端无需额外配置，新版本会在每个 KCP 会话上自动注册丢包测试服务逻辑，只响应带测试标记的 OOB 包。

### 使用方式

客户端命令（传输参数需与服务端一致）：

```sh
./client -losstest -losstest-duration 30 -losstest-rate mid \
  -remoteaddr SERVER:29900 -key 'your key' -crypt aes \
  -datashard 10 -parityshard 3 -mtu 1350 -nocomp
```

也可以使用一键脚本生成的命令（推荐，自动对齐实例参数）：

```sh
sh kcptun.sh losstest [id]
```

测试结束后输出：

```
==================== KCPTUN LOSS TEST ====================
target        : 203.0.113.5:29900
local         : 192.0.2.10:54321
rate          : mid (1000 pps)
payload       : 64 bytes
duration      : 30s per direction
----------------------------------------------------------
phase 1       : downlink (server -> client)
sent          : 29986 packets (by server)
received      : 29742 packets (at client)
lost          : 244 packets (0.814%)
----------------------------------------------------------
phase 2       : uplink (client -> server)
sent          : 29991 packets (by client)
received      : 29768 packets (at server)
lost          : 223 packets (0.744%)
rtt (echo)    : min 18.20ms / avg 25.40ms / p50 24.00ms / p95 41.00ms / p99 60.00ms / max 120.30ms
jitter        : 3.20ms
actual rate   : 999 pps over 30s
send errors   : 0
==========================================================
```

### 参数说明

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `losstest` | `false` | 进入一次性测试模式，不监听本地端口 |
| `losstest-duration` | `30` | 测试时长（秒，1-300） |
| `losstest-rate` | `mid` | `low`=100pps、`mid`=1000pps、`full`=约 20000pps |
| `losstest-size` | `64` | OOB 探测包字节数（32-1024） |
| `losstest-pps` | `20000` | `full` 档速率上限；调低可进一步减少对业务的影响 |

### 档位选择与环境适用性

| 档位 | 速率 | 额外流量 | 适用环境 | 建议时长 |
| --- | --- | --- | --- | --- |
| 低速 `low` | 100 pps | ≈0.1 Mbps | 家用宽带、移动网络、跨境高延迟线路的日常体检；可与业务同时运行 | 60 秒 |
| 中速 `mid` | 1000 pps | ≈1 Mbps | 常规 VPS / 家宽，评估日常负载下的线路质量 | 15~30 秒 |
| 全速 `full` | 20000 pps | ≈20 Mbps | 大带宽 VPS、专线、千兆家宽；排查“轻载正常、重载丢包” | 15 秒，避开高峰 |

时长建议：

- **15 秒**：快速体检，确认连通性与大致丢包水平；
- **30 秒**：常规测试，适合中速档；
- **60 秒**：观察稳定性与抖动，低速档建议用它提高丢包率置信度。

适用场景：

- `ping`/`mtr` 正常但代理仍然卡顿、重传明显——ICMP 与 UDP 路径可能不同，用 OOB 测真实 UDP 链路；
- 怀疑运营商对 UDP 限速/QoS——同一时段分别跑低、中、全速三档：低速正常、全速丢包上升，通常是带宽或 QoS 限制；三档都丢包则是链路本身问题；
- 跨境线路时段性抖动——在问题时段跑 60 秒低速，重点看 RTT p95 与 jitter；
- 调整 FEC 前后对比——用同一档位、同一时长做前后基线。

### 结果解读

| 指标 | 参考 |
| --- | --- |
| 丢包率 <0.1% | 优秀 |
| 0.1%~1% | 可用；KCP 重传可吸收，FEC 1~3 片足够 |
| 1%~3% | 有感知；建议提高 `parityshard`（4~6），并检查线路 |
| >3% | 明显影响体验；优先排查线路、UDP QoS 或拥塞 |
| RTT p95 远高于 min | 存在排队/抖动，关注 bufferbloat 或链路拥塞 |
| jitter 持续偏高 | 链路不稳定（移动网络、跨境拥塞） |
| 接近 100% 丢失 | 服务端未升级、FEC 未开启、端口不通或 UDP 被拦截 |

### 限制

- 上下行分别统计，下行在客户端计数、上行由服务端回报计数；总测试时长约为单方向时长的两倍；
- OOB 探测本身不受 FEC 保护，这是有意设计——只有不经纠错/重传，才能看到链路真实丢包；
- `full` 档默认上限 20000 pps，避免发送端队列静默丢弃污染结果；本机性能较弱或带宽较小时，可调低 `-losstest-pps`；
- 手机端客户端暂不支持该测试。

## 与上游 kcptun 的差异

1. **新增 `autoexpiremax`**：端口轮换时间在 `[autoexpire, autoexpiremax]` 秒内随机，避免多条连接集中切换。
2. **轮换不再断连**：到期后新连接走新端口，旧连接留在旧会话上继续收发，等自然结束后才关闭。
3. **`scavengettl` 默认 0**：默认永不强制关闭仍有连接的会话，只有显式设置才会兜底回收。
4. **资源有上限、日志更干净**：最多保留 `conn` 条待回收会话（总量 ≤ `2 × conn`）；正常关闭不再打印 `use of closed network connection`。

## 从源码构建

```sh
go build -o client/client ./client
go build -o server/server ./server
```

需要 Go 1.26+。

## License

MIT License

# kcptun

基于 KCP 协议的快速可靠 UDP 隧道，用于加速任意 TCP 连接（SSH、Web 服务、游戏连接等）。

kcptun 在两端之间建立一条经 **KCP 协议**优化的 UDP 通道：本地应用连接 kcptun 客户端后，流量经过多路复用、加密（可选压缩）后通过 UDP 发往服务端，服务端再还原为 TCP 连接转发给真正的目标服务。相比原生 TCP，KCP 以牺牲约 10%~20% 的带宽为代价，将平均延迟降低 30%~40%，最大延迟降低约三倍，在高丢包、高延迟链路（如跨境线路、弱网环境）下效果显著。

## 实现原理

### 总体架构

```
应用程序 (SSH / 浏览器 / 游戏客户端)
    │  TCP 或 Unix Socket（本地监听）
    ▼
┌──────────────────────────────────────────────┐
│ kcptun client (localaddr, 如 :12948)         │
│  1. 每接受一条本地连接，在 KCP 会话上打开一条  │
│     smux 逻辑流 (stream)                     │
│  2. smux 多路复用：N 条流共享 1 个 KCP 会话   │
│  3. 可选 Snappy 压缩 (nocomp=false 时)        │
│  4. 加密：PBKDF2 派生密钥 + 分组加密          │
│     (可选 QPP 量子置换垫层)                   │
│  5. KCP 层：ARQ 自动重传 + 快速重传 +         │
│     Reed-Solomon 前向纠错 (datashard/parity) │
└──────────────────────────────────────────────┘
    │  UDP（tcp=true 时用 tcpraw 伪装为 TCP 报文）
    ▼
┌──────────────────────────────────────────────┐
│ kcptun server (listen, 如 :29900)            │
│  1. 接受 KCP 会话，按客户端同样参数初始化      │
│  2. smux 服务端逐流解复用                     │
│  3. 每条流拨号到 target（TCP 或 Unix Socket） │
│  4. 双向管道转发 (std.Pipe)                   │
└──────────────────────────────────────────────┘
    │
    ▼
目标服务 (target, 如 127.0.0.1:22)
```

### 分层说明

| 层 | 实现 | 说明 |
| --- | --- | --- |
| 接入层 | `client/main.go`、`server/main.go` | 客户端监听本地 TCP 地址（不含端口号时视为 Unix Socket），服务端将流量转发至 `target`（TCP 地址或 Unix Socket 路径） |
| 多路复用层 | [xtaci/smux](https://github.com/xtaci/smux) v1/v2 | 单条 KCP 会话上复用多条逻辑流，减少握手开销；缓冲由 `smuxbuf`（会话级）与 `streambuf`（流级，v2+）控制，心跳由 `keepalive` 控制 |
| 可靠传输层 | [xtaci/kcp-go/v5](https://github.com/xtaci/kcp-go) | KCP 协议核心：ARQ 自动重传、快速重传、可关闭流控；`mode` 预设映射到 `nodelay/interval/resend/nc` 四参数组合；窗口 `sndwnd/rcvwnd`、分片 `mtu` 可调 |
| 前向纠错 (FEC) | klauspost/reedsolomon | Reed-Solomon 纠删码：每 `datashard` 个数据包附加 `parityshard` 个冗余包，丢包时可恢复而无需重传 |
| 加密层 | golang.org/x/crypto | 预共享密钥 `key` 经 PBKDF2（salt `kcp-go`，4096 次迭代，SHA-1）派生 32 字节密钥，再按 `crypt` 选择分组加密算法 |
| 可选增强 | [xtaci/qpp](https://github.com/xtaci/qpp)、[golang/snappy](https://github.com/golang/snappy)、[xtaci/tcpraw](https://github.com/xtaci/tcpraw) | QPP 量子置换垫层（`QPP`/`QPPCount`）；Snappy 流压缩（`nocomp=false`）；`tcp=true` 时模拟 TCP 报文收发，可绕过部分 UDP 限速/封锁 |
| 工具库 | `std/` | `Pipe` 双向管道转发、`MultiPort` 端口段解析、`SnmpLogger` 流量统计、QPP 端口包装、信号与退出处理 |

### 关键机制

- **模式预设（mode）**：`normal/fast/fast2/fast3` 是四组 `nodelay, interval, resend, nc` 的预设值，越靠后越激进（延迟更低、冗余更多）；`manual` 则手动指定这四个隐藏参数：

  | mode | nodelay | interval (ms) | resend | nc |
  | --- | --- | --- | --- | --- |
  | normal | 0 | 40 | 2 | 1 |
  | fast | 0 | 30 | 2 | 1 |
  | fast2 | 1 | 20 | 2 | 1 |
  | fast3 | 1 | 10 | 2 | 1 |

- **端口段支持（multiport）**：监听/远端地址支持 `IP:minport-maxport` 写法（如 `vps:29900-30100`）。服务端会对段内每个端口逐一建立监听；客户端每次拨号在段内随机选择一个端口，可分摊单端口 QoS 限速风险。
- **多连接（conn）**：客户端可建立多条 KCP 会话，本地连接按轮询（round-robin）分配到各会话。
- **连接过期与回收（autoexpire/scavengettl）**：设置 `autoexpire` 后，KCP 会话到期自动重建，过期的旧会话由 scavenger 协程按 `scavengettl` 延时回收。
- **TCP 双栈（tcp）**：服务端 `tcp=true` 时同时监听 `/tcp`（tcpraw）与 `/udp`，客户端 `tcp=true` 时通过 tcpraw 拨号，报文在外观上是 TCP，载荷仍是 KCP。
- **可观测性**：`snmplog`/`snmpperiod` 周期性输出收发包统计；`pprof` 在 `:6060` 开启 Go 性能剖析端点；`quiet` 可关闭逐流的 open/close 日志。

## 目录结构

```
kcptun/
├── client/                  # 客户端实现
│   ├── main.go              # 入口、命令行参数、加密初始化、连接管理
│   ├── config.go            # 客户端 JSON 配置解析
│   └── dial.go              # KCP/UDP 拨号（含端口段随机、tcpraw 伪装）
├── server/                  # 服务端实现
│   ├── main.go              # 入口、监听循环、smux 服务端、target 转发
│   └── config.go            # 服务端 JSON 配置解析
└── std/                     # 通用工具库
    ├── copy.go              # Pipe 双向管道转发
    ├── multiport.go         # IP:minport-maxport 端口段解析
    ├── comp.go              # Snappy 压缩流包装
    ├── qpp.go               # QPP 量子置换垫层包装
    ├── snmp.go              # SNMP 流量统计
    ├── signal.go            # 信号处理
    └── atexit*.go           # 退出钩子
```

## Building

```bash
# 构建客户端（注入版本号）
go build -ldflags "-X main.VERSION=$(git describe --tags)" -o client/client ./client/

# 构建服务端（注入版本号）
go build -ldflags "-X main.VERSION=$(git describe --tags)" -o server/server ./server/

# 交叉编译 Linux 平台
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.VERSION=$(git describe --tags)" -o client_linux_amd64 ./client/
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.VERSION=$(git describe --tags)" -o server_linux_amd64 ./server/
```

## Usage

### Server

```bash
./server -c server-config.json
```

### Client

```bash
./client -c client-config.json
```

参数也可直接通过命令行传入（如 `./server -l :29900 -t 127.0.0.1:22 --key mysecret`）；指定 `-c` 配置文件时，**配置文件中的值会覆盖命令行参数**。预共享密钥还可通过环境变量 `KCPTUN_KEY` 传入。

## Configuration

两端均使用 JSON 配置文件。**以下参数两端必须一致，否则无法建立连接**：

`key`、`crypt`、`datashard`、`parityshard`、`mode`（或 manual 模式下的 `nodelay/interval/resend/nc`）

另外注意窗口参数镜像对应：**客户端的 `sndwnd` 应等于服务端的 `rcvwnd`，客户端的 `rcvwnd` 应等于服务端的 `sndwnd`**。

### 服务端配置示例 (server-config.json)

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
  "snmplog": "",
  "snmpperiod": 60,
  "pprof": false
}
```

### 客户端配置示例 (client-config.json)

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
  "conn": 1,
  "autoexpire": 0,
  "scavengettl": 600,
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

以上示例为「SSH 加速」场景：客户端本地 `:12948` 收到的连接经隧道转发到服务器的 `22` 端口，使用 `ssh -p 12948 user@127.0.0.1` 即可接入。

### 服务端参数说明

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `listen` | string | `:29900` | 监听地址，`IP:29900` 为单端口，`IP:29900-30100` 为端口段 |
| `target` | string | `127.0.0.1:12948` | 加速目标，TCP 地址或 `path/to/unix_socket` |
| `key` | string | `it's a secrect` | 预共享密钥，两端一致（也可用环境变量 `KCPTUN_KEY`） |
| `crypt` | string | `aes` | 加密方式，见下方加密列表 |
| `mode` | string | `fast` | 加速模式预设：`fast3/fast2/fast/normal/manual` |
| `mtu` | int | `1350` | UDP 数据包最大传输单元 |
| `sndwnd` | int | `1024` | 发送窗口大小（数据包数量） |
| `rcvwnd` | int | `1024` | 接收窗口大小（数据包数量） |
| `datashard` | int | `10` | Reed-Solomon FEC 数据包分片，两端一致 |
| `parityshard` | int | `3` | Reed-Solomon FEC 冗余包分片，两端一致 |
| `dscp` | int | `0` | DSCP 差分服务代码点（6bit） |
| `nocomp` | bool | `false` | 是否关闭 Snappy 压缩（代理套壳流量建议关闭压缩） |
| `nodelay` | int | `0` | 隐藏参数：是否启用 nodelay 模式 |
| `interval` | int | `50` | 隐藏参数：协议内部工作间隔（ms） |
| `resend` | int | `0` | 隐藏参数：快速重传，0 关闭 / 1 开启 / 2 跨越 2 次 ACK 直接重传 |
| `nc` | int | `0` | 隐藏参数：是否关闭流控 |
| `acknodelay` | bool | `false` | 隐藏参数：收到包后立即flush ACK |
| `sockbuf` | int | `4194304` | 单 socket 收发缓冲区（字节） |
| `smuxver` | int | `1` | smux 版本，可选 `1`/`2` |
| `smuxbuf` | int | `4194304` | smux 会话级解复用缓冲区（字节） |
| `streambuf` | int | `2097152` | smux v2+ 单流接收缓冲区（字节） |
| `keepalive` | int | `10` | 心跳间隔（秒） |
| `closewait` | int | `30` | 连接拆除前的等待时间（秒） |
| `snmplog` | string | `""` | SNMP 统计输出文件，支持 Go 时间格式如 `./snmp-20060102.log` |
| `snmpperiod` | int | `60` | SNMP 统计周期（秒） |
| `pprof` | bool | `false` | 是否在 `:6060` 开启性能剖析服务 |
| `log` | string | `""` | 日志输出文件，默认输出到 stderr |
| `quiet` | bool | `false` | 是否屏蔽 stream open/close 日志 |
| `tcp` | bool | `false` | 是否同时监听 TCP 伪装栈（仅 Linux） |
| `qpp` | bool | `false` | 是否启用 QPP 量子置换垫层 |
| `qpp-count` | int | `61` | QPP 垫层数量（建议质数，越多越安全，每个垫层 256 字节） |

### 客户端参数说明

客户端包含服务端的所有传输层参数（`key/crypt/mode/mtu/sndwnd/rcvwnd/datashard/parityshard/dscp/nocomp` 及隐藏参数、缓冲区、snmp、pprof、quiet、tcp 等，含义相同），另有以下客户端专属参数：

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `localaddr` | string | `:12948` | 本地监听地址；不含 `:` 端口时视为 Unix Socket 路径 |
| `remoteaddr` | string | `vps:29900` | 服务端地址，`IP:29900` 单端口，`IP:29900-30100` 端口段（每次拨号随机选端口） |
| `conn` | int | `1` | 建立到服务端的 UDP 连接数，本地连接按轮询分配 |
| `autoexpire` | int | `0` | 单条 UDP 连接的自动过期重建时间（秒），0 表示关闭 |
| `scavengettl` | int | `600` | 过期连接的回收存活时间（秒），应小于 `autoexpire` |
| `closewait` | int | `0` | 连接拆除前的等待时间（秒） |

### 加密方式（crypt）

`aes`（默认）、`aes-128`、`aes-192`、`salsa20`、`blowfish`、`twofish`、`cast5`、`3des`、`tea`、`xtea`、`xor`、`sm4`、`none`（占位加密）、`null`（完全不加密）

强加密对 CPU 要求较高；在路由器等性能受限设备上运行客户端时，建议选择弱加密或不加密。

### 调优建议

- **带宽换延迟**：`datashard/parityshard` 冗余比例与 `mode` 越激进，延迟越低但流量消耗越大，按线路质量权衡
- **流量整形规避**：`tcp=true` 模拟 TCP 报文，或使用端口段 `listen/remoteaddr` 随机化端口
- **高吞吐场景**：适当调大 `sockbuf/smuxbuf`（如 64MB）、使用 `smuxver: 2` 并调大 `streambuf`（如 16MB）
- **压缩取舍**：传输文本类数据可开启压缩（`nocomp: false`）；流量本身已加密/压缩（如代理套壳）时保持 `nocomp: true`
- **大窗口生效**：Linux 下若 `sockbuf` 设置无效，检查并调大内核参数 `net.core.rmem_max` 与 `net.core.wmem_max`

## License

MIT License

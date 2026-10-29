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

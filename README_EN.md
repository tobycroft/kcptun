English | [简体中文](README.md)

# kcptun

A TCP-over-KCP tunnel: the client multiplexes local TCP connections onto KCP (UDP) toward the server, and the server converts them back to TCP and forwards them to the target service. It works well on lossy, high-latency links such as cross-border routes, weak mobile networks, and congested Wi-Fi.

- **Loss tolerance**: KCP ARQ + fast retransmit + Reed-Solomon FEC
- **Multiplexing**: smux carries many TCP streams over a single KCP session
- **Security**: pre-shared key plus optional encryption, compression, and QPP
- **Stability**: periodic port rotation without dropping established connections

## One-Click Server Setup (Recommended)

The server can be installed with the [kcptun-script](https://github.com/TuuzNet/kcptun-script) installer, which handles downloads, configuration, process supervision, and firewall rules:

```sh
wget --no-check-certificate -O kcptun.sh https://github.com/TuuzNet/kcptun-script/raw/main/kcptun.sh
sh kcptun.sh
```

The script will:

- Download the server binary matching your CPU architecture from this repository's Releases
- Walk you through port, target, encryption, and mode settings (every prompt has a default, so you can just press Enter)
- Install Supervisor, enable start-on-boot, and restart the process automatically if it crashes
- Open the UDP port in firewalld / iptables automatically
- Print the matching **client JSON configuration** and a parameter string for mobile clients
- Support multiple instances, online updates, and installing a specific version

Common commands:

| Command | Description |
| --- | --- |
| `sh kcptun.sh` | Run the interactive installer |
| `sh kcptun.sh add` | Add another instance (multi-port) |
| `sh kcptun.sh reconfig 1` | Reconfigure instance 1 |
| `sh kcptun.sh show 1` | Show the configuration of instance 1 |
| `sh kcptun.sh log 1` | Show the log of instance 1 |
| `sh kcptun.sh del 1` | Delete instance 1 |
| `sh kcptun.sh update` | Check for script and kcptun updates |
| `sh kcptun.sh uninstall` | Uninstall |

> The script requires root, supports Debian/Ubuntu, CentOS/Fedora/RedHat and similar distributions, and only runs on x86 / x86_64.

## Manual Setup

### 1. Download

Grab the files for your platform from [Releases](https://github.com/tobycroft/kcptun/releases):

| Platform | Client | Server |
| --- | --- | --- |
| Linux x86_64 | `client_linux_amd64` | `server_linux_amd64` |
| Linux x86 | `client_linux_386` | `server_linux_386` |
| Linux arm64 | `client_linux_arm64` | `server_linux_arm64` |
| Linux arm | `client_linux_arm` | `server_linux_arm` |
| macOS Intel / Apple Silicon | `client_darwin_amd64` / `client_darwin_arm64` | same as client |
| Windows x86_64 / x86 / arm64 | `client_windows_*.exe` | same as client |
| FreeBSD x86_64 | `client_freebsd_amd64` | `server_freebsd_amd64` |

Linux x86 / x86_64 also ship `kcptun-linux-*.tar.gz` archives (used by the installer, containing both client and server).

### 2. Start the Server

```sh
chmod +x server_linux_amd64
./server_linux_amd64 -c server-config.json
```

### 3. Start the Client

```sh
chmod +x client_linux_amd64
./client_linux_amd64 -c client-config.json
```

### 4. Use It

The client listens on `localaddr` (`:12948` by default). Point your application at that address, for example to accelerate SSH:

```sh
ssh -p 12948 user@127.0.0.1
```

Traffic path: local app → client `:12948` → KCP → server `:29900` → target `127.0.0.1:22`.

### 5. Run in the Background

For a quick test, run it in the foreground. For long-term use, prefer the **one-click script** above (Supervisor included). With a manual setup you can use systemd:

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

## Configuration Examples

### Server: server-config.json

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

### Client: client-config.json

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

Notes:

- These values must match on both sides: `key`, `crypt`, `datashard`, `parityshard`, `mode` (or `nodelay/interval/resend/nc` in `manual` mode)
- Windows are mirrored: client `sndwnd` = server `rcvwnd`, and client `rcvwnd` = server `sndwnd`
- `autoexpire: 300` + `autoexpiremax: 360` rotates the local port every 300–360 seconds at random; set both to `0` to keep a fixed port

## Parameters

### Server

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `listen` | string | `:29900` | Listen address; `IP:29900` for a single port, `IP:29900-30100` for a port range |
| `target` | string | `127.0.0.1:12948` | Acceleration target, a TCP address or a Unix Socket path |
| `key` | string | `it's a secrect` | Pre-shared key, must match on both sides (also `KCPTUN_KEY` env var) |
| `crypt` | string | `aes` | Cipher: `aes`, `aes-128`, `aes-192`, `salsa20`, `blowfish`, `twofish`, `cast5`, `3des`, `tea`, `xtea`, `xor`, `sm4`, `none`, `null` |
| `mode` | string | `fast` | Mode preset: `fast3` / `fast2` / `fast` / `normal` / `manual` |
| `mtu` | int | `1350` | Maximum transmission unit for UDP packets |
| `sndwnd` / `rcvwnd` | int | `1024` | Send / receive window size in packets |
| `datashard` / `parityshard` | int | `10` / `3` | Reed-Solomon FEC data and parity shards, must match on both sides |
| `dscp` | int | `0` | DSCP code point (6 bits) |
| `nocomp` | bool | `false` | Disable Snappy compression |
| `sockbuf` | int | `4194304` | Per-socket send/receive buffer in bytes |
| `smuxver` | int | `1` | smux version, `1` or `2` |
| `smuxbuf` | int | `4194304` | smux session-level buffer in bytes |
| `streambuf` | int | `2097152` | Per-stream receive buffer in bytes (smux v2+) |
| `keepalive` | int | `10` | Heartbeat interval in seconds |
| `closewait` | int | `30` | Seconds to wait before tearing down a connection |
| `snmplog` / `snmpperiod` | string / int | `""` / `60` | Traffic statistics file and collection period |
| `pprof` | bool | `false` | Enable profiling server on `:6060` |
| `log` | string | `""` | Log file, stderr by default |
| `quiet` | bool | `false` | Suppress stream open/close logs |
| `tcp` | bool | `false` | Also listen on a TCP-emulated stack (Linux only) |
| `qpp` / `qpp-count` | bool / int | `false` / `61` | QPP quantum permutation pad toggle and pad count |

### Client

The client accepts every transport parameter above (same meaning) plus:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `localaddr` | string | `:12948` | Local listen address; treated as a Unix Socket path when it has no `:` port |
| `remoteaddr` | string | `vps:29900` | Server address, `IP:29900` or a port range `IP:29900-30100` (a port is picked at random per dial) |
| `conn` | int | `1` | Number of KCP connections to the server; local connections are assigned round-robin |
| `autoexpire` | int | `0` | Port rotation interval in seconds, `0` disables it; also the lower bound for `autoexpiremax` |
| `autoexpiremax` | int | `0` | Upper bound of the random rotation interval in seconds, `0` means fixed; must be greater than `autoexpire` |
| `scavengettl` | int | `0` | Force-close deadline in seconds for retired connections, `0` means never force close (recommended) |
| `closewait` | int | `0` | Seconds to wait before tearing down a connection |

## Differences from Upstream kcptun

1. **New `autoexpiremax`**: the rotation time is randomized within `[autoexpire, autoexpiremax]` seconds, so connections do not all switch at once.
2. **Rotation no longer drops connections**: after expiry, new connections use the new port while existing ones keep running on the old session until they finish naturally.
3. **`scavengettl` defaults to 0**: sessions with live connections are never force-closed unless you explicitly set a deadline.
4. **Bounded resources and cleaner logs**: at most `conn` retired sessions are kept (total ≤ `2 × conn`), and normal teardown no longer prints `use of closed network connection`.

## Build from Source

```sh
go build -o client/client ./client
go build -o server/server ./server
```

Requires Go 1.27+.

Verify the multi-platform artifacts locally (identical matrix and naming to the published
Release assets):

```sh
VERSION=v20261030.1 bash .github/scripts/build-dist.sh
```

## Automation Pipeline

Third-party dependency updates (Dependabot) → CI (`gofmt` / `go vet` / build / tests) →
auto-merge (squash) → multi-platform build + SHA-256 checksums → automatic tag and GitHub
Release are fully handled by GitHub Actions. Once a dependency update lands on `master`,
the new binaries are published with no manual step, using the same file names as the table
above.

See [`.github/AUTOMATION.md`](.github/AUTOMATION.md) for the full flow, required repository
settings, first-run verification steps and troubleshooting.

## Acknowledgements

This project is derived from [xtaci/kcptun](https://github.com/xtaci/kcptun). Thanks to the original author **xtaci** for the excellent work, and to the underlying libraries [kcp-go](https://github.com/xtaci/kcp-go), [smux](https://github.com/xtaci/smux), [qpp](https://github.com/xtaci/qpp) and [tcpraw](https://github.com/xtaci/tcpraw).

## License

MIT License

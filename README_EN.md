English | [简体中文](README.md)

# kcptun

![Special Version](assets/Specialversion.png)

A TCP-over-KCP tunnel: the client multiplexes local TCP connections onto KCP (UDP) toward the server, and the server converts them back to TCP and forwards them to the target service. It works well on lossy, high-latency links such as cross-border routes, weak mobile networks, and congested Wi-Fi.

- **Loss tolerance**: KCP ARQ + fast retransmit + Reed-Solomon FEC
- **Multiplexing**: smux carries many TCP streams over a single KCP session
- **Security**: pre-shared key plus optional encryption, compression, and QPP
- **Stability**: periodic port rotation without dropping established connections

![Architecture](assets/architecturenew.jpeg)

> Details such as manual setup, configuration examples, parameters, and differences from upstream are collapsed by default — click a section title to expand it.

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

<details>
<summary><strong>Manual Setup</strong> (download · start · background)</summary>

### Manual Setup

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

</details>

<details>
<summary><strong>Configuration Examples</strong> (server-config.json / client-config.json)</summary>

### Configuration Examples

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
  "keepalivetolerance": 2,
  "logsize": 1024,
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
- `logsize: 1024` caps the client running log at 1 MB (the unit is KB): once full, the oldest whole lines are dropped so the file only keeps the most recent tail, still in chronological order and readable with `cat`/`tail`. It does not apply to `snmplog`, and the server has no such option
- `autoexpire: -1` is the fully automatic mode: ports never rotate by clock, they rotate together only once a port is confirmed blocked. A slot is judged blocked when nothing at all has arrived from the peer for `keepalive × keepalivetolerance` seconds **and** an out-of-band liveness probe stays unanswered. The probe is what tells a blocked path apart from an idle but healthy one, so an idle connection is never torn down. In this mode `autoexpiremax` and `scavengettl` do not apply, and the server has to run the same build (answering probes needs FEC, so `datashard`/`parityshard` must be above 0)

</details>

<details>
<summary><strong>Parameters</strong> (full server / client parameter tables)</summary>

### Parameters

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
| `autoexpire` | int | `0` | Port rotation interval in seconds, `0` disables rotation, `-1` switches to the fully automatic mode driven by the blockage detector; also the lower bound for `autoexpiremax` |
| `autoexpiremax` | int | `0` | Upper bound of the random rotation interval in seconds, `0` means fixed; must be greater than `autoexpire`, unused in `-1` mode |
| `scavengettl` | int | `0` | Force-close deadline in seconds for retired connections, `0` means never force close (recommended), unused in `-1` mode |
| `keepalivetolerance` | int | `2` | How many consecutive unanswered heartbeats a port may tolerate before it is judged blocked, only used with `autoexpire: -1`; `keepalive × keepalivetolerance` has to stay below the 30 second session timeout built into smux |
| `logsize` | int | `0` | Size cap for the running log file in KB; once full the oldest whole lines are dropped and only the recent tail is kept. `0` means grow forever (legacy behaviour). Only applies when logging to `log`, console output is never capped, and the server has no such option |
| `closewait` | int | `0` | Seconds to wait before tearing down a connection |

</details>

## Loss Test

The client has a built-in loss test mode based on KCP OOB probes, answering the question "does this link actually drop packets, and how stable is the latency?"

<details>
<summary><strong>Loss Test</strong> · Principle</summary>

### Loss Test · Principle

KCP's reliable transport retransmits lost packets, so you never see loss in the application stream; `ping`/`mtr` test ICMP, but many ISPs treat ICMP and UDP differently. The loss test uses kcp-go's OOB (out-of-band) channel:

- OOB packets are **not retransmitted, not reordered, not ACKed** — behaving exactly like raw UDP probes;
- Probe packets reuse the KCP session's encryption, port, MTU, and FEC parameters, traversing the same UDP path as proxy traffic;
- The test runs in two sequential phases: **downlink first** (server sends probes, client counts received sequence numbers), **then uplink** (client sends probes, server counts received packets and reports via a control packet); loss rates for the two directions are independent;
- Uplink probe packets are echoed back by the server so the client can calculate RTT and jitter.

This means the test measures "the true performance of the UDP path that the KCP session uses" without being masked by retransmissions.

</details>

<details>
<summary><strong>Loss Test</strong> · Prerequisites</summary>

### Loss Test · Prerequisites

| Condition | Description |
| --- | --- |
| Version | Both server and client must be `losstest`-capable builds of the same version |
| FEC | Must be enabled; both `datashard` and `parityshard` greater than 0 |
| Client | CLI client required; mobile apps do not support this yet |
| Port | Same as the server listen port; no additional firewall rules needed |

The server requires no extra configuration — newer versions automatically register the loss test handler on every KCP session and only respond to marked OOB packets.

</details>

<details>
<summary><strong>Loss Test</strong> · Usage and Output Example</summary>

### Loss Test · Usage and Output Example

Client command (transport parameters must match the server):

```sh
./client -losstest -losstest-duration 30 -losstest-rate mid \
  -remoteaddr SERVER:29900 -key 'your key' -crypt aes \
  -datashard 10 -parityshard 3 -mtu 1350 -nocomp
```

You can also use the command generated by the one-click script (recommended, auto-aligns instance parameters):

```sh
sh kcptun.sh losstest [id]
```

Output after the test:

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

</details>

<details>
<summary><strong>Loss Test</strong> · Parameters</summary>

### Loss Test · Parameters

| Parameter | Default | Description |
| --- | --- | --- |
| `losstest` | `false` | Enter one-shot test mode; no local port is opened |
| `losstest-duration` | `30` | Test duration in seconds (1–300) |
| `losstest-rate` | `mid` | `low` = 100 pps, `mid` = 1000 pps, `full` ≈ 20000 pps |
| `losstest-size` | `64` | OOB probe packet size in bytes (32–1024) |
| `losstest-pps` | `20000` | Rate cap for `full`; lower it to reduce impact on live traffic |

</details>

<details>
<summary><strong>Loss Test</strong> · Rate Tiers and Environment Applicability</summary>

### Loss Test · Rate Tiers and Environment Applicability

| Tier | Rate | Extra Traffic | Suitable Environment | Suggested Duration |
| --- | --- | --- | --- | --- |
| Low `low` | 100 pps | ≈0.1 Mbps | Home broadband, mobile networks, high-latency cross-border routes; can run alongside live traffic | 60 s |
| Mid `mid` | 1000 pps | ≈1 Mbps | Typical VPS / home broadband; assess everyday link quality | 15–30 s |
| Full `full` | 20000 pps | ≈20 Mbps | High-bandwidth VPS, dedicated lines, gigabit broadband; diagnose "fine when idle, lossy under load" | 15 s, avoid peak hours |

Duration guidelines:

- **15 s**: quick connectivity and rough loss check;
- **30 s**: standard test, suitable for `mid`;
- **60 s**: observe stability and jitter; recommended for `low` to improve loss-rate confidence.

Use cases:

- `ping`/`mtr` look fine but the proxy still stalls with visible retransmissions — ICMP and UDP may take different paths; use OOB to measure the real UDP link;
- Suspect ISP UDP throttling / QoS — run all three tiers in the same time window: low is fine but full shows increasing loss means bandwidth or QoS limits; all three lose packets means the link itself is faulty;
- Cross-border route with time-dependent jitter — run 60 s on `low` during the problematic window, focus on RTT p95 and jitter;
- Before/after FEC tuning — use the same tier and duration as a baseline comparison.

</details>

<details>
<summary><strong>Loss Test</strong> · Interpreting Results</summary>

### Loss Test · Interpreting Results

| Metric | Guidance |
| --- | --- |
| Loss < 0.1% | Excellent |
| 0.1%–1% | Usable; KCP retransmissions can absorb this, FEC 1–3 shards sufficient |
| 1%–3% | Perceptible; consider raising `parityshard` (4–6) and investigate the route |
| > 3% | Clearly impacts experience; prioritize checking the route, UDP QoS, or congestion |
| RTT p95 far above min | Queuing/jitter present; watch for bufferbloat or link congestion |
| Jitter persistently high | Unstable link (mobile network, cross-border congestion) |
| Near 100% loss | Server not updated, FEC not enabled, port unreachable, or UDP blocked |

</details>

<details>
<summary><strong>Loss Test</strong> · Limitations</summary>

### Loss Test · Limitations

- Uplink and downlink are measured separately: downlink is counted at the client, uplink is reported by the server; total test time is roughly twice the per-direction duration;
- OOB probes are intentionally **not** FEC-protected — only by bypassing correction and retransmission can the true path loss be observed;
- `full` tier caps at 20000 pps by default to avoid sender queue silent drops polluting results; lower `-losstest-pps` on weak hardware or limited bandwidth;
- Mobile client does not support this test yet.

</details>

<details>
<summary><strong>Differences from Upstream kcptun</strong></summary>

### Differences from Upstream kcptun

1. **New `autoexpiremax`**: the rotation time is randomized within `[autoexpire, autoexpiremax]` seconds, so connections do not all switch at once.
2. **Rotation no longer drops connections**: after expiry, new connections use the new port while existing ones keep running on the old session until they finish naturally.
3. **`scavengettl` defaults to 0**: sessions with live connections are never force-closed unless you explicitly set a deadline.
4. **Bounded resources and cleaner logs**: at most `conn` retired sessions are kept (total ≤ `2 × conn`), and normal teardown no longer prints `use of closed network connection`.
5. **New `autoexpire: -1` mode and `keepalivetolerance`**: rotation is no longer clock driven, port blockage is detected from heartbeat silence plus an out-of-band liveness probe, and once confirmed every port switches at once. This also sidesteps a known side effect of timed rotation: the server (kcp-go `Listener`) maps sessions by client address rather than by `conv`, so an old session and its replacement fight over the same address slot, which shows up as the first connection after a rotation failing only after a 30 second timeout while the second one succeeds. The automatic mode never leaves two sessions running side by side, so the race cannot happen.
6. **New `logsize` (client only)**: a KB-sized cap on the file given to `-log`. Past the cap the oldest whole lines are dropped and only the recent tail is kept, so the file never exceeds the limit while staying in order. `0` keeps the legacy append-forever behaviour, and the server has no such option.

</details>

<details>
<summary><strong>Build from Source</strong></summary>

### Build from Source

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

</details>

<details>
<summary><strong>Automation Pipeline</strong></summary>

### Automation Pipeline

Third-party dependency updates (Dependabot) → CI (`gofmt` / `go vet` / build / tests) →
auto-merge (squash) → multi-platform build + SHA-256 checksums → automatic tag and GitHub
Release are fully handled by GitHub Actions. Once a dependency update lands on `master`,
the new binaries are published with no manual step, using the same file names as the table
above.

See [`.github/AUTOMATION.md`](.github/AUTOMATION.md) for the full flow, required repository
settings, first-run verification steps and troubleshooting.

</details>

## Acknowledgements

This project is derived from [xtaci/kcptun](https://github.com/xtaci/kcptun). Thanks to the original author **xtaci** for the excellent work, and to the underlying libraries [kcp-go](https://github.com/xtaci/kcp-go), [smux](https://github.com/xtaci/smux), [qpp](https://github.com/xtaci/qpp) and [tcpraw](https://github.com/xtaci/tcpraw).

## License

MIT License

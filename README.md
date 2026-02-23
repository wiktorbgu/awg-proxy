# awg-proxy -- AmneziaWG UDP Proxy for MikroTik

🇷🇺 [Русская версия](docs/README.ru.md)

Lightweight Docker container that transforms standard WireGuard traffic into AmneziaWG-compatible format, allowing MikroTik routers to connect to AmneziaWG servers with traffic obfuscation support.

## Table of Contents

- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Verification](#verification)
- [Configuration Reference](#configuration-reference)
- [Getting AWG Parameters](#getting-awg-parameters)
- [Uninstallation](#uninstallation)
- [Building from Source](#building-from-source)
- [Troubleshooting](#troubleshooting)

## How It Works

```
MikroTik WG client ──UDP──► [awg-proxy container] ──UDP──► AmneziaWG server
  (native crypto)           (packet transformation)        (sees valid AWG)
```

MikroTik handles all WireGuard cryptography natively using its built-in WG client. The proxy sits between the router and the AmneziaWG server, performing only packet framing transformations:

- **Outbound (WG to AWG):** replaces standard WireGuard message type headers with AmneziaWG values (H1--H4), prepends random padding to handshake packets (S1/S2 bytes), sends junk packets before handshake initiation (Jc packets of Jmin--Jmax bytes), and recomputes MAC1 using the server's public key so the AWG server accepts the packet.
- **Inbound (AWG to WG):** reverses type replacement, strips padding from handshake packets, recomputes MAC1 using the client's public key so MikroTik accepts the response, and silently drops junk packets.

No tunnel data or session keys are modified. The proxy is completely transparent to the WireGuard protocol layer.

## Quick Start

1. Export your AmneziaWG `.conf` file (see [Getting AWG Parameters](#getting-awg-parameters))
2. Open the **[Offline Configurator](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html)**
3. Paste the `.conf` contents and copy the generated commands
4. Execute the commands on your MikroTik router via terminal

## Requirements

- **AmneziaWG server** -- a running server with known obfuscation parameters
- **Configuration file** (`.conf`) -- exported from AmneziaVPN (see [Getting AWG Parameters](#getting-awg-parameters))
- **MikroTik RouterOS 7.4+** with the **container** package installed
- **Supported architectures**: ARM64, ARM (v7), or x86\_64
  ([check your device](https://help.mikrotik.com/docs/spaces/ROS/pages/47579139/Container))
- Device mode enabled: `/system/device-mode/update container=yes`
- At least 5 MB free disk space, 16+ MB free RAM recommended

## Installation

### Step 1: Enable container package and reboot

Install the container package from `/system/package`, then enable container mode and reboot:

```routeros
/system/device-mode/update container=yes
```

The router will reboot. After it comes back up, proceed to the next steps.

### Choose your setup method

**Option A: [Offline Configurator](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html) (recommended)**

Paste your AmneziaWG `.conf` file and get ready-to-use MikroTik commands. Copy the output and execute on the router, then skip to [Verification](#verification).

**Option B: Manual setup**

Follow Steps 2--7 below to configure everything manually.

### Step 2: Upload image to router

Download `awg-proxy-{arch}.tar.gz` from [GitHub Releases](https://github.com/amneziawg-mikrotik/awg-proxy/releases/latest) (choose arm64, arm, or amd64 to match your router) and upload it to the router via Winbox or SCP.

Alternatively, download directly from RouterOS (replace the URL with the actual release version):

```routeros
# /tool/fetch url="https://github.com/amneziawg-mikrotik/awg-proxy/releases/download/vX.X.X/awg-proxy-arm64.tar.gz" dst-path=awg-proxy-arm64.tar.gz
```

### Step 3: Create network (veth, IP, NAT)

```routeros
# Create virtual Ethernet interface for the container
/interface/veth/add name=veth-awg-proxy address=172.18.0.2/30 gateway=172.18.0.1

# Assign IP address to the host side of the veth pair
/ip/address/add address=172.18.0.1/30 interface=veth-awg-proxy

# NAT rule so the container can reach the internet
/ip/firewall/nat/add chain=srcnat action=masquerade src-address=172.18.0.0/30
```

### Step 4: Create WireGuard interface and peer

```routeros
# Create the WireGuard interface
/interface/wireguard/add name=wg-awg-proxy private-key="YOUR_PRIVATE_KEY" listen-port=12429

# Add the peer, pointing endpoint at the proxy container
/interface/wireguard/peers/add interface=wg-awg-proxy public-key="SERVER_PUBLIC_KEY" preshared-key="YOUR_PRESHARED_KEY" endpoint-address=172.18.0.2 endpoint-port=51820 allowed-address=0.0.0.0/0 persistent-keepalive=25

# Assign the tunnel IP address
/ip/address/add address=YOUR_TUNNEL_IP interface=wg-awg-proxy
```

Replace `YOUR_PRIVATE_KEY` with your WireGuard private key (from `[Interface]` PrivateKey), `SERVER_PUBLIC_KEY` with the AWG server public key (from `[Peer]` PublicKey), `YOUR_PRESHARED_KEY` with the preshared key (if any), and `YOUR_TUNNEL_IP` with the tunnel IP (from `[Interface]` Address, e.g. `10.8.0.2/32`). Add routing rules as needed for your setup.

### Step 5: Set environment variables

`AWG_CLIENT_PUB` is automatically read from the WireGuard interface created in the previous step -- no need to compute it manually.

```routeros
# Container environment variables (AWG obfuscation parameters)
/container/envs/add list=awg-proxy-env key=AWG_LISTEN value=":51820"
/container/envs/add list=awg-proxy-env key=AWG_REMOTE value="YOUR_SERVER:PORT"
/container/envs/add list=awg-proxy-env key=AWG_JC value="5"
/container/envs/add list=awg-proxy-env key=AWG_JMIN value="30"
/container/envs/add list=awg-proxy-env key=AWG_JMAX value="500"
/container/envs/add list=awg-proxy-env key=AWG_S1 value="20"
/container/envs/add list=awg-proxy-env key=AWG_S2 value="20"
/container/envs/add list=awg-proxy-env key=AWG_H1 value="1234567890"
/container/envs/add list=awg-proxy-env key=AWG_H2 value="1234567891"
/container/envs/add list=awg-proxy-env key=AWG_H3 value="1234567892"
/container/envs/add list=awg-proxy-env key=AWG_H4 value="1234567893"
/container/envs/add list=awg-proxy-env key=AWG_SERVER_PUB value="SERVER_PUBLIC_KEY"
/container/envs/add list=awg-proxy-env key=AWG_CLIENT_PUB value=[/interface/wireguard/get [find name=wg-awg-proxy] public-key]
```

Replace `YOUR_SERVER:PORT` with your AmneziaWG server address and port. Replace all H1--H4, S1, S2, Jc, Jmin, Jmax values with the actual parameters from your AmneziaWG configuration. `AWG_SERVER_PUB` is the AWG server public key (from `[Peer]` PublicKey in your `.conf` file).

### Step 6: Create container

```routeros
/container/add file=awg-proxy-arm64.tar.gz interface=veth-awg-proxy envlist=awg-proxy-env hostname=awg-proxy root-dir=disk1/awg-proxy logging=yes shm-size=4M start-on-boot=yes
```

### Step 7: Start container

```routeros
/container/start [find where tag~"awg-proxy"]
```

## Verification

After starting the container, confirm that everything is running correctly:

```routeros
/container/print
/interface/wireguard/print
/interface/wireguard/peers/print
/ping 172.18.0.2
```

The container status should show `running`. The WireGuard peer should show a recent handshake time once traffic flows. The ping to `172.18.0.2` confirms the veth link to the container is up.

## Configuration Reference

All configuration is done through environment variables passed to the container.

| Variable | Required | Default | Description |
|---|---|---|---|
| `AWG_LISTEN` | Yes | -- | Listen address, e.g. `:51820` |
| `AWG_REMOTE` | Yes | -- | AmneziaWG server address (`host:port`) |
| `AWG_JC` | Yes | -- | Junk packet count sent before handshake initiation |
| `AWG_JMIN` | Yes | -- | Minimum junk packet size in bytes |
| `AWG_JMAX` | Yes | -- | Maximum junk packet size in bytes |
| `AWG_S1` | Yes | -- | Random padding prepended to handshake init (bytes) |
| `AWG_S2` | Yes | -- | Random padding prepended to handshake response (bytes) |
| `AWG_H1` | Yes | -- | Replacement message type for handshake init |
| `AWG_H2` | Yes | -- | Replacement message type for handshake response |
| `AWG_H3` | Yes | -- | Replacement message type for cookie reply |
| `AWG_H4` | Yes | -- | Replacement message type for transport data |
| `AWG_SERVER_PUB` | Yes | -- | AWG server public key (base64), used for MAC1 recomputation on outbound handshake packets |
| `AWG_CLIENT_PUB` | Yes | -- | WG client public key (base64), auto-derived from WG interface (see Step 5) |
| `AWG_TIMEOUT` | No | `180` | Inactivity timeout in seconds before reconnecting |
| `AWG_LOG_LEVEL` | No | `info` | Log verbosity: `none`, `error`, or `info` |

## Getting AWG Parameters

The Jc, Jmin, Jmax, S1, S2, H1--H4 values must match your AmneziaWG server configuration exactly. To obtain them:

### Export from AmneziaVPN

1. Open the **AmneziaVPN** application
2. Select the desired connection
3. Tap **Share**
4. Choose: **Protocol**: AmneziaWG, **Format**: AmneziaWG Format
5. Save the resulting `.conf` file

### Reading the parameters

1. Open the exported `.conf` file in a text editor.
2. The obfuscation parameters are in the `[Interface]` section:
   ```ini
   [Interface]
   Jc = 5
   Jmin = 30
   Jmax = 500
   S1 = 20
   S2 = 20
   H1 = 1234567890
   H2 = 1234567891
   H3 = 1234567892
   H4 = 1234567893
   ```
3. The `Endpoint` value from the `[Peer]` section becomes `AWG_REMOTE`.
4. The `PublicKey` value from the `[Peer]` section becomes `AWG_SERVER_PUB`.
5. `AWG_CLIENT_PUB` is derived automatically from the WireGuard interface (see Step 5).

Alternatively, use the [offline configurator](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html) to paste your `.conf` file and generate all MikroTik commands automatically.

## Uninstallation

The uninstall script is created automatically during installation via the configurator.
To remove awg-proxy, run:

```routeros
/system/script/run awg-proxy-uninstall
```

The script removes the container, WireGuard interface, NAT rules, routes,
environment variables, restores previous DNS settings, and deletes itself.

## Building from Source

Requires Go 1.25+ and Docker with buildx support.

```bash
make build          # Build local binary
make test           # Run tests with race detector
make docker-arm64   # Build Docker image for ARM64 (MikroTik ARM64 devices)
make docker-arm     # Build Docker image for ARM v7
make docker-amd64   # Build Docker image for x86_64
make docker-all     # Build for all architectures
```

The Docker build produces a minimal scratch-based image containing a single statically linked binary.

## Troubleshooting

**Container does not start**
- Verify that the container package is installed: `/system/package/print`
- Confirm device mode is enabled: `/system/device-mode/print`
- Check available disk space: `/system/resource/print`

**Handshake timeout (no connection established)**
- Ensure all AWG parameters (Jc, Jmin, Jmax, S1, S2, H1--H4) match the server configuration exactly. Even a single mismatched value will prevent the handshake.
- Verify that `AWG_REMOTE` points to the correct server address and port.
- Verify that `AWG_SERVER_PUB` and `AWG_CLIENT_PUB` are set correctly. Incorrect public keys cause MAC1 verification failures and silently dropped packets.
- Check that the container can reach the server: the NAT masquerade rule must be in place.

**No traffic after successful handshake**
- Confirm the NAT rule exists: `/ip/firewall/nat/print`
- Check routing on the MikroTik -- traffic to the WireGuard peer must be routed through the proxy.
- Verify the WireGuard peer `endpoint-address` is set to the container IP (`172.18.0.2`).

**Container crash loop**
- Inspect container status: `/container/print`
- Set `AWG_LOG_LEVEL` to `info` to see detailed proxy logs.
- Common cause: missing or invalid environment variables. All required variables must be set.

## License

MIT -- see [LICENSE](LICENSE) for details.

# awg-proxy -- AmneziaWG for MikroTik

[![Tests](https://github.com/amneziawg-mikrotik/awg-proxy/actions/workflows/build.yml/badge.svg)](https://github.com/amneziawg-mikrotik/awg-proxy/actions/workflows/build.yml)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.25-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Русская версия](README.md)

Lightweight Docker container that allows MikroTik routers to connect to AmneziaWG servers. All traffic is encrypted by the router's native WireGuard client; the container only transforms the packet format.

## Table of Contents

- [How It Works](#how-it-works)
- [Quick Start (Configurator)](#quick-start-configurator)
- [Requirements](#requirements)
- [Manual Installation](#manual-installation)
- [Getting AWG Parameters](#getting-awg-parameters)
- [Additional Settings](#additional-settings)
- [Uninstallation](#uninstallation)
- [Troubleshooting](#troubleshooting)
  - [Insufficient disk space](#insufficient-disk-space)
  - [not allowed by device-mode](#not-allowed-by-device-mode)
- [Building from Source](#building-from-source)
- [License](#license)

## How It Works

```
MikroTik WG client ──UDP──> [awg-proxy] ──UDP──> AmneziaWG server
   (encryption)          (transformation)          (obfuscation)
```

The proxy replaces packet headers, adds padding and junk packets so the AmneziaWG server accepts the traffic. Keys and data are not modified.

Compatible with AWG v1 and v2 -- the version is detected automatically based on the environment variables.

## Quick Start (Configurator)

1. Export a `.conf` file from AmneziaVPN (see [Getting AWG Parameters](#getting-awg-parameters))
2. Open the [configurator](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html)
3. Paste the `.conf` file contents
4. Copy the generated commands and run them in MikroTik terminal

Done. The configurator works offline; no data is sent to any server.

## Requirements

- An AmneziaWG server with known obfuscation parameters
- Configuration file `.conf` exported from AmneziaVPN
- MikroTik RouterOS 7.4+ with the **container** package
  - **RouterOS 7.21+**: standard images `awg-proxy-{arch}.tar.gz` (OCI format)
  - **RouterOS 7.20 and below**: images `awg-proxy-{arch}-7.20-Docker.tar.gz` (Docker format)
  - The configurator detects the version automatically
- Architecture: ARM64, ARM (v7), or x86_64 ([check your device](https://help.mikrotik.com/docs/spaces/ROS/pages/84901929/Container))
- At least 5 MB disk space, 16+ MB RAM recommended

## Manual Installation

### 1. Enable Containers

Install the container package from [mikrotik.com](https://mikrotik.com/download), upload it to the router, and reboot. Then:

```routeros
/system/device-mode/update container=yes
```

The router will ask for confirmation (button press or reboot, depending on the model).

### 2. Upload Image

Download `awg-proxy-{arch}.tar.gz` from [Releases](https://github.com/amneziawg-mikrotik/awg-proxy/releases/latest) and upload it to the router via Winbox or SCP. For RouterOS 7.20 and below, use files with the `-7.20-Docker` suffix (Docker format).

Or download directly on the router (replace URL with the actual one):

```routeros
/tool/fetch url="https://github.com/amneziawg-mikrotik/awg-proxy/releases/download/vX.X.X/awg-proxy-arm64.tar.gz" dst-path=awg-proxy-arm64.tar.gz
```

### 3. Network Setup

```routeros
/interface/veth/add name=veth-awg-proxy address=172.18.0.2/30 gateway=172.18.0.1
/ip/address/add address=172.18.0.1/30 interface=veth-awg-proxy
/ip/firewall/nat/add chain=srcnat action=masquerade src-address=172.18.0.0/30
```

### 4. WireGuard

```routeros
/interface/wireguard/add name=wg-awg-proxy private-key="YOUR_PRIVATE_KEY" listen-port=12429
/interface/wireguard/peers/add interface=wg-awg-proxy public-key="SERVER_PUBLIC_KEY" \
    preshared-key="YOUR_PRESHARED_KEY" endpoint-address=172.18.0.2 endpoint-port=51820 \
    allowed-address=0.0.0.0/0 persistent-keepalive=25
/ip/address/add address=YOUR_TUNNEL_IP interface=wg-awg-proxy
```

Replace:
- `YOUR_PRIVATE_KEY` -- PrivateKey from `[Interface]`
- `SERVER_PUBLIC_KEY` -- PublicKey from `[Peer]`
- `YOUR_PRESHARED_KEY` -- PresharedKey from `[Peer]` (if any)
- `YOUR_TUNNEL_IP` -- Address from `[Interface]` (e.g., `10.8.0.2/32`)

### 5. Environment Variables

```routeros
/container/envs/add list=awg-proxy-env key=AWG_LISTEN value=":51820"
/container/envs/add list=awg-proxy-env key=AWG_REMOTE value="SERVER_IP:PORT"
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

Replace all values with parameters from your `.conf` file. `AWG_CLIENT_PUB` is read automatically from the WireGuard interface.

### 6. Create and Start Container

```routeros
/container/add file=awg-proxy-arm64.tar.gz interface=veth-awg-proxy envlist=awg-proxy-env \
    hostname=awg-proxy root-dir=disk1/awg-proxy logging=yes shm-size=4M start-on-boot=yes
/container/start [find where tag~"awg-proxy"]
```

Verify it works:

```routeros
/container/print
/interface/wireguard/peers/print
```

The container should show `running` status, and the peer should have a `last-handshake` value.

## Getting AWG Parameters

1. Open the **AmneziaVPN** application
2. Select the desired connection
3. Tap **Share**
4. Choose: **Protocol**: AmneziaWG, **Format**: AmneziaWG Format
5. Save the `.conf` file

The obfuscation parameters (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`--`H4`) are in the `[Interface]` section, while `Endpoint` and `PublicKey` are in the `[Peer]` section.

## Additional Settings

### Routing Traffic Through the Tunnel

Specific host:

```routeros
/ip/route/add dst-address=8.8.8.8/32 gateway=wg-awg-proxy
```

Subnet:

```routeros
/ip/route/add dst-address=10.0.0.0/8 gateway=wg-awg-proxy
```

View routes:

```routeros
/ip/route/print where gateway=wg-awg-proxy
```

Remove a route:

```routeros
/ip/route/remove [find where dst-address="8.8.8.8/32" gateway="wg-awg-proxy"]
```

### DNS Through the Tunnel

To route DNS queries through the tunnel, set DNS servers and add routes to them:

```routeros
/ip/dns/set servers=8.8.8.8,8.8.4.4
/ip/route/add dst-address=8.8.8.8/32 gateway=wg-awg-proxy
/ip/route/add dst-address=8.8.4.4/32 gateway=wg-awg-proxy
```

### Address-List Based Routing (Advanced)

For selective traffic routing through the tunnel, use routing tables and mangle rules.

Create a routing table:

```routeros
/routing/table/add disabled=no fib name=r_to_vpn
```

Default route through the tunnel for this table:

```routeros
/ip/route/add dst-address=0.0.0.0/0 gateway=wg-awg-proxy routing-table=r_to_vpn
```

Address list with destinations to route through the tunnel:

```routeros
/ip/firewall/address-list/add address=8.8.8.8 list=to_vpn
/ip/firewall/address-list/add address=1.1.1.1 list=to_vpn
```

Mangle rules for traffic marking:

```routeros
# Skip local traffic
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=10.0.0.0/8
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=172.16.0.0/12
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=192.168.0.0/16

# Mark connections to addresses in the list
/ip/firewall/mangle/add chain=prerouting action=mark-connection \
    dst-address-list=to_vpn connection-mark=no-mark \
    new-connection-mark=to-vpn-conn passthrough=yes

# Mark routing for tagged connections
/ip/firewall/mangle/add chain=prerouting action=mark-routing \
    connection-mark=to-vpn-conn new-routing-mark=r_to_vpn passthrough=yes
```

NAT for marked traffic:

```routeros
/ip/firewall/nat/add chain=srcnat action=masquerade routing-mark=r_to_vpn
```

Now all traffic to addresses in the `to_vpn` list will go through the tunnel. Add addresses to the list as needed.

## Uninstallation

If installed via the configurator:

```routeros
/system/script/run awg-proxy-uninstall
```

The script removes the container, WireGuard interface, NAT rules, routes, environment variables, restores DNS settings, and deletes itself.

## Troubleshooting

**Container does not start** -- check the container package is installed (`/system/package/print`), device mode is enabled (`/system/device-mode/print`), and there is enough disk space (`/system/resource/print`).

**No handshake** -- make sure all AWG parameters (Jc, Jmin, Jmax, S1, S2, H1--H4) exactly match the server. Verify `AWG_REMOTE`, `AWG_SERVER_PUB`, and `AWG_CLIENT_PUB`.

**No traffic after handshake** -- check the NAT rule (`/ip/firewall/nat/print`), routing, and the peer's `endpoint-address` (should be `172.18.0.2`).

**Container keeps restarting** -- set `AWG_LOG_LEVEL=info` and check the logs. Common cause: missing environment variables.

### Insufficient disk space

If you get `Insufficient disk space` error during container installation and you have free space on an external drive (USB, SD, NVMe) -- reconfigure the image download directory:

```routeros
/container/config set tmpdir=usb1/pull ram-high=200M
```

Replace `usb1` with your drive name (`usb1`, `usb2`, `sd1`, `nvme1`, `sata1`). After the container is installed, you can revert:

```routeros
/container/config set tmpdir="" ram-high=0
```

If using the configurator -- select the appropriate drive in the "Container storage" field, and tmpdir will be configured automatically.

### not allowed by device-mode

If you get `not allowed by device-mode` error when downloading an image or creating a container, containers are not enabled. Run:

```routeros
/system/device-mode/update container=yes
```

The router will ask for confirmation -- press the Reset or Mode button on the device (depends on model) within a few minutes, or wait for automatic reboot. After reboot, retry the installation.

## Building from Source

Requires Go 1.25+, Docker (for container images), and make.

```bash
# Tests
make test

# Local binary build
make build

# Docker images (OCI, for RouterOS 7.21+)
make docker-arm64    # ARM64
make docker-arm      # ARM v7
make docker-armv5    # ARM v5
make docker-amd64    # x86_64
make docker-all      # All architectures

# Docker images (classic format, for RouterOS 7.20 and below)
make docker-arm64-7.20-docker
make docker-arm-7.20-docker
make docker-armv5-7.20-docker
make docker-amd64-7.20-docker
make docker-all-7.20-docker
```

Artifacts are created in the `builds/` directory.

## License

MIT -- see [LICENSE](LICENSE).

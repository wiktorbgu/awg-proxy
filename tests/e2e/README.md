# E2E Tests for AWG Proxy Configurator

## Requirements

- Docker + Docker Compose (for RouterOS CHR)
- Node.js 18+ (for Playwright)
- SSH client

## Quick start

```bash
# Install Playwright
npm install
npx playwright install chromium

# Start RouterOS instances
docker compose up -d
./helpers/wait-for-ros.sh 2220 2221

# Run configurator tests (no RouterOS needed)
npx playwright test configurator.test.js

# Run RouterOS command tests (requires running CHR)
TEST_CONF_PATH=/path/to/test.conf ./routeros-test.sh

# Cleanup
docker compose down
```

## Test config

Copy `test.conf.example` to `test.conf` and fill in real AmneziaWG values.
`test.conf` is gitignored and must NOT be committed.

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TEST_CONF_PATH` | Path to .conf file | `./test.conf` |
| `ROS720_SSH_PORT` | SSH port for RouterOS 7.20 | `2220` |
| `ROS721_SSH_PORT` | SSH port for RouterOS 7.21 | `2221` |
| `ROS_SSH_USER` | SSH username | `admin` |
| `ROS_SSH_HOST` | SSH host | `localhost` |
| `ROS_BOOT_TIMEOUT` | Boot wait timeout (seconds) | `120` |

## Architecture

```
[Playwright] -> configurator.html -> Generates RouterOS script
     |
[SSH] -> RouterOS 7.20 CHR (QEMU in Docker) -> Executes commands
[SSH] -> RouterOS 7.21 CHR (QEMU in Docker) -> Executes commands
```

RouterOS CHR runs via `evilfreelancer/docker-routeros` (QEMU inside Docker).
Second disk (`sata1`, 64MB) emulates USB storage.
Container feature requires device-mode -- tests cover commands up to that point.

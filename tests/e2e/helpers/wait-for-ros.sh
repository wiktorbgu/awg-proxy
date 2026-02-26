#!/usr/bin/env bash
# Wait for RouterOS instances to become reachable via SSH.
# Usage: ./wait-for-ros.sh <port1> [port2] ...
# Exits 0 when all instances respond, 1 on timeout.

set -euo pipefail

TIMEOUT=${ROS_BOOT_TIMEOUT:-120}
HOST=${ROS_SSH_HOST:-localhost}
USER=${ROS_SSH_USER:-admin}

SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3 -o LogLevel=ERROR"

wait_for_port() {
    local port=$1
    local elapsed=0
    echo "Waiting for RouterOS on port $port (timeout: ${TIMEOUT}s)..."
    while [ $elapsed -lt "$TIMEOUT" ]; do
        if ssh $SSH_OPTS -p "$port" "${USER}@${HOST}" "/system/identity/print" 2>/dev/null | grep -q "name:"; then
            echo "RouterOS on port $port is ready (${elapsed}s)"
            return 0
        fi
        sleep 3
        elapsed=$((elapsed + 3))
    done
    echo "TIMEOUT: RouterOS on port $port not ready after ${TIMEOUT}s"
    return 1
}

if [ $# -eq 0 ]; then
    echo "Usage: $0 <port1> [port2] ..."
    exit 1
fi

FAILED=0
for port in "$@"; do
    wait_for_port "$port" || FAILED=$((FAILED + 1))
done

if [ $FAILED -gt 0 ]; then
    echo "FAIL: $FAILED instance(s) did not start"
    exit 1
fi

echo "All RouterOS instances ready"

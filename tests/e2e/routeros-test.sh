#!/usr/bin/env bash
set -euo pipefail

HOST=${ROS_SSH_HOST:-localhost}
ROS_USER=${ROS_SSH_USER:-admin}
PORT_720=${ROS720_SSH_PORT:-2220}
PORT_721=${ROS721_SSH_PORT:-2221}
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o LogLevel=ERROR -o PubkeyAuthentication=no"

PASS=0
FAIL=0
SKIP=0

ros_exec() {
    local port="$1"
    local cmd="$2"
    sshpass -p "" ssh $SSH_OPTS -p "$port" "${ROS_USER}@${HOST}" "$cmd" 2>/dev/null || \
        ssh $SSH_OPTS -p "$port" "${ROS_USER}@${HOST}" "$cmd" 2>/dev/null
}

verify_cmd() {
    local port="$1"
    local cmd="$2"
    local expected="$3"
    local out
    out=$(ros_exec "$port" "$cmd" 2>/dev/null || true)
    if echo "$out" | grep -qF "$expected"; then
        PASS=$((PASS + 1))
        echo "  PASS: $cmd -> contains '$expected'"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: $cmd -> expected '$expected', got: $out"
    fi
}

run_script() {
    local port="$1"
    local script="$2"
    local full_script="${script}
:put \"AWG_TEST_OK\""
    local out
    out=$(ros_exec "$port" "$full_script" 2>/dev/null || true)
    echo "$out"
}

check_script_ok() {
    local port="$1"
    local script="$2"
    local desc="$3"
    local out
    out=$(run_script "$port" "$script")
    if echo "$out" | grep -qF "AWG_TEST_OK"; then
        PASS=$((PASS + 1))
        echo "  PASS: $desc"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: $desc -> sentinel not found in: $out"
    fi
}

check_script_fail() {
    local port="$1"
    local script="$2"
    local expected_str="$3"
    local desc="$4"
    local full_script="${script}
:put \"AWG_TEST_OK\""
    local out
    out=$(ros_exec "$port" "$full_script" 2>/dev/null || true)
    local has_expected=0
    local has_sentinel=0
    echo "$out" | grep -qF "$expected_str" && has_expected=1 || true
    echo "$out" | grep -qF "AWG_TEST_OK" && has_sentinel=1 || true
    if [ "$has_expected" -eq 1 ] && [ "$has_sentinel" -eq 0 ]; then
        PASS=$((PASS + 1))
        echo "  PASS: $desc"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: $desc -> expected '$expected_str' without sentinel. got: $out"
    fi
}

test_b1_wg_interface() {
    local port="$1"
    echo "  [B1] WG interface create/verify/remove"
    ros_exec "$port" '/interface/wireguard/add name=wg-test listen-port=12429' >/dev/null 2>&1 || true
    verify_cmd "$port" '/interface/wireguard/print where name=wg-test' 'wg-test'
    ros_exec "$port" '/interface/wireguard/remove [find where name=wg-test]' >/dev/null 2>&1 || true
}

test_b2_veth_ip() {
    local port="$1"
    echo "  [B2] VETH + IP address"
    ros_exec "$port" '/interface/veth/add name=veth-test address=172.18.0.2/30 gateway=172.18.0.1' >/dev/null 2>&1 || true
    ros_exec "$port" '/ip/address/add address=172.18.0.1/30 interface=veth-test' >/dev/null 2>&1 || true
    verify_cmd "$port" '/interface/veth/print where name=veth-test' 'veth-test'
    verify_cmd "$port" '/ip/address/print where interface=veth-test' '172.18.0.1'
    ros_exec "$port" '/ip/address/remove [find where interface=veth-test]' >/dev/null 2>&1 || true
    ros_exec "$port" '/interface/veth/remove [find where name=veth-test]' >/dev/null 2>&1 || true
}

test_b3_nat() {
    local port="$1"
    echo "  [B3] NAT masquerade rule"
    ros_exec "$port" '/ip/firewall/nat/add chain=srcnat action=masquerade src-address=172.18.0.0/30 comment=test-nat' >/dev/null 2>&1 || true
    verify_cmd "$port" '/ip/firewall/nat/print where comment=test-nat' 'masquerade'
    ros_exec "$port" '/ip/firewall/nat/remove [find where comment=test-nat]' >/dev/null 2>&1 || true
}

test_b4_env_vars() {
    local port="$1"
    echo "  [B4] Container env vars"
    ros_exec "$port" '/container/envs/add list=test-env key=AWG_LISTEN value=":51820"' >/dev/null 2>&1 || true
    ros_exec "$port" '/container/envs/add list=test-env key=AWG_REMOTE value="1.2.3.4:443"' >/dev/null 2>&1 || true
    verify_cmd "$port" '/container/envs/print where list=test-env' 'AWG_LISTEN'
    verify_cmd "$port" '/container/envs/print where list=test-env' 'AWG_REMOTE'
    ros_exec "$port" '/container/envs/remove [find where list=test-env]' >/dev/null 2>&1 || true
}

test_b5_disk_format() {
    local port="$1"
    echo "  [B5] Disk format (may skip if sata1 absent)"
    local disk_out
    disk_out=$(ros_exec "$port" '/disk/print' 2>/dev/null || true)
    if ! echo "$disk_out" | grep -qF 'sata1'; then
        SKIP=$((SKIP + 1))
        echo "  SKIP: [B5] sata1 disk not found"
        return
    fi
    ros_exec "$port" '/disk/format-drive sata1 file-system=ext4 label=sata1' >/dev/null 2>&1 || true
    sleep 3
    verify_cmd "$port" '/disk/print' 'sata1'
}

test_b6_free_space() {
    local port="$1"
    echo "  [B6] Free space check (depends on B5)"
    local disk_out
    disk_out=$(ros_exec "$port" '/disk/print' 2>/dev/null || true)
    if ! echo "$disk_out" | grep -qF 'sata1-part1'; then
        SKIP=$((SKIP + 1))
        echo "  SKIP: [B6] sata1-part1 not found (B5 likely skipped)"
        return
    fi
    local free_out
    free_out=$(ros_exec "$port" ':put [/disk/get [find where mount-point=sata1-part1] free]' 2>/dev/null || true)
    if [ -n "$free_out" ] && [ "$free_out" -gt 0 ] 2>/dev/null; then
        PASS=$((PASS + 1))
        echo "  PASS: [B6] free space = $free_out"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: [B6] expected number > 0, got: $free_out"
    fi
}

test_b7_mkdir() {
    local port="$1"
    echo "  [B7] Make directory (depends on B5)"
    local disk_out
    disk_out=$(ros_exec "$port" '/disk/print' 2>/dev/null || true)
    if ! echo "$disk_out" | grep -qF 'sata1-part1'; then
        SKIP=$((SKIP + 1))
        echo "  SKIP: [B7] sata1-part1 not found"
        return
    fi
    ros_exec "$port" '/file/make-directory sata1-part1/pull' >/dev/null 2>&1 || true
    verify_cmd "$port" '/file/print where name=sata1-part1/pull' 'sata1-part1/pull'
    ros_exec "$port" '/file/remove sata1-part1/pull' >/dev/null 2>&1 || true
}

test_b8_low_disk_space() {
    local port="$1"
    echo "  [B8] Low disk space negative test"
    local script='{
  :local freeStorage 1000
  :if ($freeStorage < 5242880) do={
    :put "Insufficient disk space"
    :error "Insufficient disk space"
  }
}'
    check_script_fail "$port" "$script" "Insufficient disk space" "[B8] low disk space error path"
}

test_b9_uninstall() {
    local port="$1"
    echo "  [B9] Uninstall cleanup"
    ros_exec "$port" '/interface/wireguard/add name=wg-awg-proxy listen-port=12429' >/dev/null 2>&1 || true
    ros_exec "$port" '/interface/veth/add name=veth-awg-proxy address=172.18.0.2/30 gateway=172.18.0.1' >/dev/null 2>&1 || true
    ros_exec "$port" '/container/envs/add list=awg-proxy-env key=AWG_LISTEN value=":51820"' >/dev/null 2>&1 || true

    ros_exec "$port" '/container/envs/remove [find where list="awg-proxy-env"]' >/dev/null 2>&1 || true
    ros_exec "$port" '/interface/veth/remove [find where name="veth-awg-proxy"]' >/dev/null 2>&1 || true
    ros_exec "$port" '/interface/wireguard/remove [find where name="wg-awg-proxy"]' >/dev/null 2>&1 || true

    local env_out wg_out veth_out
    env_out=$(ros_exec "$port" '/container/envs/print where list=awg-proxy-env' 2>/dev/null || true)
    wg_out=$(ros_exec "$port" '/interface/wireguard/print where name=wg-awg-proxy' 2>/dev/null || true)
    veth_out=$(ros_exec "$port" '/interface/veth/print where name=veth-awg-proxy' 2>/dev/null || true)

    local ok=1
    echo "$env_out" | grep -qF 'AWG_LISTEN' && ok=0 || true
    echo "$wg_out" | grep -qF 'wg-awg-proxy' && ok=0 || true
    echo "$veth_out" | grep -qF 'veth-awg-proxy' && ok=0 || true

    if [ "$ok" -eq 1 ]; then
        PASS=$((PASS + 1))
        echo "  PASS: [B9] all resources removed"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: [B9] some resources still present"
    fi
}

test_version_detection() {
    local port="$1"
    local expected_minor="$2"
    local expected_suffix="$3"
    echo "  [C] Version detection (minor=$expected_minor suffix='$expected_suffix')"
    local script='{
  :local ver [/system/resource/get version]
  :local dotPos [:find $ver "."]
  :local rest [:pick $ver ($dotPos + 1) [:len $ver]]
  :local endPos [:find $rest "."]
  :if ([:typeof $endPos] = "nil") do={ :set endPos [:find $rest " "] }
  :local minor [:tonum [:pick $rest 0 $endPos]]
  :local suffix ""
  :if ($minor <= 20) do={ :set suffix "-7.20-Docker" }
  :put ("minor=" . $minor . " suffix=" . $suffix)
}'
    local out
    out=$(ros_exec "$port" "$script" 2>/dev/null || true)
    local want="minor=${expected_minor} suffix=${expected_suffix}"
    if echo "$out" | grep -qF "$want"; then
        PASS=$((PASS + 1))
        echo "  PASS: got '$want'"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: expected '$want', got: $out"
    fi
}

run_tests() {
    local port="$1"
    local label="$2"
    echo "  Checking SSH connectivity on port $port..."
    if ! ros_exec "$port" ':put ok' >/dev/null 2>&1; then
        echo "  SKIP: cannot connect to RouterOS $label on port $port"
        SKIP=$((SKIP + 7))
        return
    fi

    test_b1_wg_interface "$port"
    test_b2_veth_ip "$port"
    test_b3_nat "$port"
    test_b4_env_vars "$port"
    test_b5_disk_format "$port"
    test_b6_free_space "$port"
    test_b7_mkdir "$port"
    test_b8_low_disk_space "$port"
    test_b9_uninstall "$port"
}

echo "=== RouterOS 7.20 (port $PORT_720) ==="
run_tests "$PORT_720" "7.20"

echo ""
echo "=== RouterOS 7.21 (port $PORT_721) ==="
run_tests "$PORT_721" "7.21"

echo ""
echo "=== Version comparison tests ==="
if ros_exec "$PORT_720" ':put ok' >/dev/null 2>&1; then
    test_version_detection "$PORT_720" "20" "-7.20-Docker"
else
    SKIP=$((SKIP + 1))
    echo "  SKIP: [C] 7.20 not reachable"
fi
if ros_exec "$PORT_721" ':put ok' >/dev/null 2>&1; then
    test_version_detection "$PORT_721" "21" ""
else
    SKIP=$((SKIP + 1))
    echo "  SKIP: [C] 7.21 not reachable"
fi

echo ""
echo "=== Results ==="
echo "PASS: $PASS"
echo "FAIL: $FAIL"
echo "SKIP: $SKIP"
[ "$FAIL" -eq 0 ] || exit 1

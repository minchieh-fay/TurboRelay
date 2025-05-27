#!/bin/bash

echo "=== TurboRelay ARP Test ==="
echo "This script will generate ARP traffic to test packet forwarding"
echo ""

if [ "$EUID" -ne 0 ]; then
    echo "Warning: This script should be run as root for best results"
fi

# 获取当前网络配置
echo "Current network configuration:"
ip addr show | grep -E "(inet |^[0-9]+:)" | grep -v "127.0.0.1"
echo ""

# 获取本地IP和网段
LOCAL_IP=$(ip addr show | grep "inet " | grep -v "127.0.0.1" | head -1 | awk '{print $2}' | cut -d'/' -f1)
if [ -z "$LOCAL_IP" ]; then
    echo "No local IP found, using default test IPs"
    NETWORK_PREFIX="10.35.146"
else
    NETWORK_PREFIX=$(echo $LOCAL_IP | cut -d'.' -f1-3)
    echo "Detected network: ${NETWORK_PREFIX}.x"
fi

echo ""
echo "=== Method 1: Clear ARP cache and ping ==="
echo "This will force ARP requests"

# 清除ARP缓存
echo "Clearing ARP cache..."
if command -v ip >/dev/null 2>&1; then
    ip neigh flush all 2>/dev/null || echo "Could not flush ARP cache (need root)"
else
    arp -d -a 2>/dev/null || echo "Could not clear ARP cache (need root)"
fi

# 生成一些ARP请求
echo "Generating ARP requests by pinging nearby IPs..."
for i in {1..10}; do
    TARGET_IP="${NETWORK_PREFIX}.$((RANDOM % 254 + 1))"
    echo "Pinging $TARGET_IP (will generate ARP request)..."
    ping -c 1 -W 1 $TARGET_IP > /dev/null 2>&1 &
    sleep 0.5
done

echo ""
echo "=== Method 2: Manual ARP requests ==="
if command -v arping >/dev/null 2>&1; then
    echo "Using arping to generate ARP requests..."
    for i in {1..5}; do
        TARGET_IP="${NETWORK_PREFIX}.$((RANDOM % 254 + 1))"
        echo "ARP request to $TARGET_IP"
        arping -c 1 -w 1 $TARGET_IP 2>/dev/null &
        sleep 0.5
    done
else
    echo "arping not available, using ping method only"
fi

echo ""
echo "=== Method 3: Network interface manipulation ==="
echo "You can also trigger ARP by:"
echo "1. Unplugging and replugging network cable"
echo "2. Bringing interface down and up:"
echo "   sudo ip link set <interface> down"
echo "   sudo ip link set <interface> up"
echo "3. Adding/removing IP addresses"

echo ""
echo "=== ARP traffic generation completed ==="
echo "Check TurboRelay logs for ARP packet forwarding activity"
echo "You should see ARP Request/Reply packets in the logs"

wait 
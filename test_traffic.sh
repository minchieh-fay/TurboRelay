#!/bin/bash

echo "=== TurboRelay Traffic Test Script ==="
echo "This script will generate test traffic to verify packet forwarding"
echo ""

# 检查是否为root用户
if [ "$EUID" -ne 0 ]; then
    echo "Error: This script must be run as root"
    exit 1
fi

# 获取网络接口信息
echo "Available network interfaces:"
ip link show | grep -E "^[0-9]+:" | awk '{print $2}' | sed 's/://'
echo ""

# 获取IP地址信息
echo "Network configuration:"
ip addr show | grep -E "(inet |^[0-9]+:)" | grep -v "127.0.0.1"
echo ""

# 测试1: ping测试
echo "=== Test 1: Ping Test ==="
echo "Testing connectivity to common addresses..."

# 测试本地网段
LOCAL_NETWORK=$(ip route | grep -E "eth[0-9]" | head -1 | awk '{print $1}' | head -1)
if [ ! -z "$LOCAL_NETWORK" ]; then
    echo "Local network: $LOCAL_NETWORK"
    # 提取网关地址
    GATEWAY=$(ip route | grep default | awk '{print $3}' | head -1)
    if [ ! -z "$GATEWAY" ]; then
        echo "Testing ping to gateway: $GATEWAY"
        ping -c 3 $GATEWAY
    fi
fi

echo ""

# 测试2: 生成UDP流量
echo "=== Test 2: UDP Traffic Generation ==="
echo "Generating UDP test traffic..."

# 使用nc生成UDP流量
for i in {1..5}; do
    echo "Sending UDP packet $i..."
    echo "Test packet $i from TurboRelay" | nc -u -w1 8.8.8.8 53 2>/dev/null &
    sleep 1
done

echo ""

# 测试3: 生成TCP流量
echo "=== Test 3: TCP Traffic Generation ==="
echo "Generating TCP test traffic..."

# 测试HTTP连接
for i in {1..3}; do
    echo "Testing HTTP connection $i..."
    curl -m 5 -s http://www.baidu.com > /dev/null 2>&1 &
    sleep 2
done

echo ""

# 测试4: ARP流量
echo "=== Test 4: ARP Traffic ==="
echo "Generating ARP requests..."

# 获取本地网段的一些IP进行ARP请求
LOCAL_IP=$(ip addr show | grep "inet " | grep -v "127.0.0.1" | head -1 | awk '{print $2}' | cut -d'/' -f1)
if [ ! -z "$LOCAL_IP" ]; then
    # 获取网段
    NETWORK_PREFIX=$(echo $LOCAL_IP | cut -d'.' -f1-3)
    echo "Testing ARP requests in network ${NETWORK_PREFIX}.x"
    
    for i in {1..5}; do
        TARGET_IP="${NETWORK_PREFIX}.$((RANDOM % 254 + 1))"
        echo "ARP request to $TARGET_IP"
        ping -c 1 -W 1 $TARGET_IP > /dev/null 2>&1 &
    done
fi

echo ""
echo "=== Traffic generation completed ==="
echo "Check TurboRelay logs for packet forwarding activity"
echo "You should see packet forwarding logs if the forwarder is working correctly" 
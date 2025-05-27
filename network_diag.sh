#!/bin/bash

echo "=== TurboRelay Network Diagnostics ==="
echo "Analyzing network configuration for packet forwarding setup"
echo ""

# 检查是否为root用户
if [ "$EUID" -ne 0 ]; then
    echo "Warning: Running as non-root user, some information may be limited"
fi

echo "=== System Information ==="
echo "Hostname: $(hostname)"
echo "Kernel: $(uname -r)"
echo "Date: $(date)"
echo ""

echo "=== Network Interfaces ==="
echo "Available interfaces:"
ip link show
echo ""

echo "=== IP Configuration ==="
ip addr show
echo ""

echo "=== Routing Table ==="
ip route show
echo ""

echo "=== ARP Table ==="
arp -a 2>/dev/null || ip neigh show
echo ""

echo "=== Network Statistics ==="
echo "Interface statistics:"
cat /proc/net/dev | head -2
cat /proc/net/dev | grep -E "(eth|ens|enp)"
echo ""

echo "=== Packet Forwarding Status ==="
echo "IP forwarding: $(cat /proc/sys/net/ipv4/ip_forward)"
echo "IPv6 forwarding: $(cat /proc/sys/net/ipv6/conf/all/forwarding)"
echo ""

echo "=== Bridge Information ==="
if command -v brctl >/dev/null 2>&1; then
    echo "Bridge configuration:"
    brctl show 2>/dev/null
else
    echo "brctl not available, checking with ip:"
    ip link show type bridge 2>/dev/null
fi
echo ""

echo "=== Firewall Status ==="
if command -v iptables >/dev/null 2>&1; then
    echo "iptables rules (first 10):"
    iptables -L -n | head -10 2>/dev/null || echo "Cannot access iptables (need root)"
else
    echo "iptables not available"
fi
echo ""

echo "=== Network Connectivity Tests ==="
echo "Testing basic connectivity..."

# 测试本地回环
echo -n "Localhost ping: "
ping -c 1 -W 1 127.0.0.1 >/dev/null 2>&1 && echo "OK" || echo "FAILED"

# 测试网关
GATEWAY=$(ip route | grep default | awk '{print $3}' | head -1)
if [ ! -z "$GATEWAY" ]; then
    echo -n "Gateway ($GATEWAY) ping: "
    ping -c 1 -W 2 $GATEWAY >/dev/null 2>&1 && echo "OK" || echo "FAILED"
fi

# 测试DNS
echo -n "DNS (8.8.8.8) ping: "
ping -c 1 -W 2 8.8.8.8 >/dev/null 2>&1 && echo "OK" || echo "FAILED"

echo ""

echo "=== Interface Details ==="
for iface in $(ip link show | grep -E "^[0-9]+:" | awk '{print $2}' | sed 's/:$//' | grep -E "(eth|ens|enp)"); do
    echo "Interface: $iface"
    echo "  Status: $(ip link show $iface | grep -o "state [A-Z]*" | awk '{print $2}')"
    echo "  MAC: $(ip link show $iface | grep -o "link/ether [a-f0-9:]*" | awk '{print $2}')"
    
    # 获取IP地址
    IP=$(ip addr show $iface | grep "inet " | awk '{print $2}' | head -1)
    if [ ! -z "$IP" ]; then
        echo "  IP: $IP"
    else
        echo "  IP: No IP assigned"
    fi
    
    # 检查接口流量
    RX_BYTES=$(cat /sys/class/net/$iface/statistics/rx_bytes 2>/dev/null || echo "0")
    TX_BYTES=$(cat /sys/class/net/$iface/statistics/tx_bytes 2>/dev/null || echo "0")
    echo "  RX bytes: $RX_BYTES"
    echo "  TX bytes: $TX_BYTES"
    echo ""
done

echo "=== Recommendations ==="
echo "For TurboRelay to work properly:"
echo "1. Both interfaces should be UP"
echo "2. Interfaces should not have IP addresses (acting as bridge)"
echo "3. IP forwarding should be enabled if routing is needed"
echo "4. No conflicting firewall rules"
echo "5. Physical connections should be established"
echo ""

echo "=== Diagnostics completed ===" 
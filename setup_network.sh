#!/bin/bash
# 设置虚拟网络环境用于测试

set -e

echo "=== 设置虚拟网络环境 ==="

# 检查是否有root权限
if [ "$EUID" -ne 0 ]; then
    echo "错误: 需要root权限运行此脚本"
    echo "请使用: sudo $0"
    exit 1
fi

# 清理现有的veth设备
echo "清理现有的veth设备..."
ip link delete veth1 2>/dev/null || true
ip link delete veth2 2>/dev/null || true

# 创建veth对
echo "创建veth设备对..."
ip link add veth1 type veth peer name veth2

# 启用接口
echo "启用网络接口..."
ip link set veth1 up
ip link set veth2 up

# 配置IP地址
echo "配置IP地址..."
ip addr add 192.168.100.1/24 dev veth1
ip addr add 192.168.100.2/24 dev veth2

echo "✓ 网络环境设置完成"
echo ""
echo "网络配置:"
echo "  veth1: 192.168.100.1/24"
echo "  veth2: 192.168.100.2/24"
echo ""
echo "测试连通性:"
echo "  ping -c 1 192.168.100.2"
echo ""
echo "启动转发器:"
echo "  sudo ./turbo_relay -if1 veth1 -if2 veth2 -p 8000 -debug" 
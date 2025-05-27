#!/bin/bash

echo "=== TurboRelay 运行示例 ==="
echo ""

echo "使用方法："
echo "1. 基本运行："
echo "   sudo ./turbo_relay -if1=eth1 -if2=eth0"
echo ""

echo "2. 启用调试模式："
echo "   sudo ./turbo_relay -if1=eth1 -if2=eth0 -debug"
echo ""

echo "参数说明："
echo "  -if1: 近端网络接口（连接3方设备，直连，不丢包）"
echo "  -if2: 远端网络接口（连接MCU，可能丢包）"
echo "  -debug: 启用调试模式"
echo ""

echo "网络配置："
echo "  if1: 固定为近端接口，连接3方设备（直连，稳定）"
echo "  if2: 固定为远端接口，连接MCU（可能通过网络）"
echo ""

echo "转发方向："
echo "  if1 -> if2: 3方设备 → MCU（近端到远端）"
echo "  if2 -> if1: MCU → 3方设备（远端到近端）"
echo ""

if [ $# -eq 0 ]; then
    echo "请提供运行参数，例如："
    echo "sudo ./turbo_relay -if1=eth1 -if2=eth0"
    exit 0
fi

echo "正在启动 TurboRelay..."
sudo ./turbo_relay "$@" 
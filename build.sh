#!/bin/bash
# 构建TurboRelay IP流量转发器

set -e

echo "=== TurboRelay IP流量转发器构建 ==="

# 检查Go环境
if ! command -v go &> /dev/null; then
    echo "错误: 未找到Go编译器"
    exit 1
fi

echo "Go版本: $(go version)"

# 构建程序
echo "构建IP流量转发器..."
go build -o turbo_relay main.go
echo "✓ 构建完成: turbo_relay"

echo ""
echo "使用方法:"
echo "  sudo ./turbo_relay -if1 <接口1> -if2 <接口2> [-debug]"
echo ""
echo "例如:"
echo "  sudo ./turbo_relay -if1 veth1 -if2 veth2 -debug"
echo ""
echo "功能: 转发所有IP流量（TCP、UDP、ICMP等）"
echo "注意: 需要root权限来访问网络接口" 
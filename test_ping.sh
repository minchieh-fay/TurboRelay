#!/bin/bash

echo "=== TurboRelay Ping Test ==="
echo "This script will generate ping traffic to test packet forwarding"
echo ""

if [ $# -eq 0 ]; then
    echo "Usage: $0 <target_ip>"
    echo "Example: $0 10.35.146.109"
    echo "Example: $0 8.8.8.8"
    exit 1
fi

TARGET_IP=$1

echo "Target IP: $TARGET_IP"
echo "Starting continuous ping test..."
echo "Press Ctrl+C to stop"
echo ""

# 连续ping，每秒一次
ping -i 1 $TARGET_IP 
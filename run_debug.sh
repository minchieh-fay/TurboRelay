#!/bin/sh

# Simple debug version - for troubleshooting

echo "=== TurboRelay Debug Start ==="

# Check file
if [ ! -f "/opt/turbo_relay_arm64" ]; then
    echo "ERROR: Program file not found"
    exit 1
fi

# Set permissions
chmod +x /opt/turbo_relay_arm64

# Start network
echo "Setting up network interfaces..."
ifconfig eth1 up
ifconfig eth2 up
sleep 2

# Show network status
echo "Network status:"
ifconfig eth1 | grep UP
ifconfig eth2 | grep UP

# Change directory
cd /opt

# Start program - show all output for debugging
echo "Starting program..."
echo "Command: ./turbo_relay_arm64 eth2 eth1"

./turbo_relay_arm64 eth2 eth1 
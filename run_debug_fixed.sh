#!/bin/sh

# Simple debug version - for troubleshooting
# Fixed to avoid shell command confusion

echo "=== TurboRelay Debug Start ==="

# Check file
if [ ! -f "/opt/turbo_relay_arm64" ]; then
    echo "ERROR: Program file not found"
    exit 1
fi

echo "Program file found: /opt/turbo_relay_arm64"

# Set permissions
chmod +x /opt/turbo_relay_arm64
echo "Permissions set"

# Check file info
ls -la /opt/turbo_relay_arm64

# Start network
echo "Setting up network interfaces..."
ifconfig eth1 up 2>/dev/null && echo "eth1 up OK" || echo "eth1 up FAILED"
ifconfig eth2 up 2>/dev/null && echo "eth2 up OK" || echo "eth2 up FAILED"
sleep 2

# Show network status
echo "Network status:"
ifconfig eth1 2>/dev/null | grep -E "(UP|inet)" || echo "eth1 not found"
ifconfig eth2 2>/dev/null | grep -E "(UP|inet)" || echo "eth2 not found"

# Show all network interfaces
echo "All network interfaces:"
ifconfig -a | grep "^[a-z]" | head -10

# Change directory
cd /opt || exit 1
echo "Changed to directory: $(pwd)"

# Try to run program with correct parameters
echo "Starting program with correct parameters..."
echo "Command: ./turbo_relay_arm64 -if1 eth2 -if2 eth1"
echo "--- Program Output Start ---"

# Run program in foreground with correct parameters (eth2=near, eth1=far)
./turbo_relay_arm64 -if1 eth2 -if2 eth1

echo "--- Program Output End ---"
echo "Program exited with code: $?" 
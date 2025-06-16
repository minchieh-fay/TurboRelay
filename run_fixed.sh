#!/bin/sh

# TurboRelay Fixed Startup Script - Compatible with /bin/sh
# For ARM accelerator hardware

SCRIPT_DIR="/opt"
BINARY_NAME="turbo_relay_arm64"
BINARY_PATH="$SCRIPT_DIR/$BINARY_NAME"
PID_FILE="/var/run/turborelay.pid"

# Simple log function
log() {
    echo "[$(date)] $1"
}

# Check binary file
check_binary() {
    if [ ! -f "$BINARY_PATH" ]; then
        log "ERROR: Binary not found $BINARY_PATH"
        exit 1
    fi
    
    if [ ! -x "$BINARY_PATH" ]; then
        chmod +x "$BINARY_PATH"
    fi
}

# Setup network interfaces
setup_network() {
    log "Setting up network interfaces..."
    
    ifconfig eth1 up 2>/dev/null || log "eth1 setup failed"
    ifconfig eth2 up 2>/dev/null || log "eth2 setup failed"
    
    sleep 2
    log "Network interfaces ready"
}

# Check if process is running
is_running() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
            return 0
        else
            rm -f "$PID_FILE"
        fi
    fi
    return 1
}

# Start program - fixed version with correct parameters
start_program() {
    log "Starting TurboRelay..."
    
    cd "$SCRIPT_DIR" || exit 1
    
    # Start program with correct parameters (eth2=near, eth1=far)
    "$BINARY_PATH" -if1 eth2 -if2 eth1 >/dev/null 2>&1 &
    
    # Get PID immediately
    PID=$!
    
    # Save PID to file
    echo "$PID" > "$PID_FILE"
    
    log "Program started, PID: $PID"
    
    # Wait a bit for program to initialize
    sleep 3
    
    # Check if still running
    if kill -0 "$PID" 2>/dev/null; then
        log "Start successful"
        return 0
    else
        log "Start failed - program exited"
        rm -f "$PID_FILE"
        return 1
    fi
}

# Stop program
stop_program() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        log "Stopping program PID: $PID"
        
        kill "$PID" 2>/dev/null
        
        # Wait for program to exit
        COUNT=0
        while [ $COUNT -lt 10 ]; do
            if ! kill -0 "$PID" 2>/dev/null; then
                break
            fi
            sleep 1
            COUNT=$((COUNT + 1))
        done
        
        # Force kill if still running
        if kill -0 "$PID" 2>/dev/null; then
            kill -9 "$PID" 2>/dev/null
        fi
        
        rm -f "$PID_FILE"
        log "Program stopped"
    fi
}

# Monitor loop
monitor() {
    log "Starting monitor..."
    
    while true; do
        if ! is_running; then
            log "Program not running, restarting..."
            
            # Clean up any remaining processes
            killall "$BINARY_NAME" 2>/dev/null
            sleep 2
            
            if start_program; then
                log "Restart successful"
            else
                log "Restart failed, retry in 30s"
                sleep 30
            fi
        fi
        
        sleep 5
    done
}

# Signal handler
cleanup() {
    log "Received exit signal"
    stop_program
    exit 0
}

# Set signal handler
trap cleanup TERM INT

# Main function
main() {
    log "=== TurboRelay Starting ==="
    
    check_binary
    setup_network
    
    if is_running; then
        log "Program already running, stopping first"
        stop_program
    fi
    
    if start_program; then
        monitor
    else
        log "Start failed"
        exit 1
    fi
}

# Handle parameters
case "${1:-start}" in
    start)
        main
        ;;
    stop)
        log "Stopping TurboRelay"
        stop_program
        ;;
    restart)
        log "Restarting TurboRelay"
        stop_program
        sleep 2
        setup_network
        start_program
        ;;
    status)
        if is_running; then
            PID=$(cat "$PID_FILE")
            echo "TurboRelay running (PID: $PID)"
        else
            echo "TurboRelay not running"
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac 
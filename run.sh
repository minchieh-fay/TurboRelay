#!/bin/bash

# TurboRelay 加速器启动脚本
# 用于在加速器硬件上自动启动和监控 turbo_relay_arm64 程序
# eth2: 近端网卡, eth1: 远端网卡

set -e

# 配置变量
SCRIPT_DIR="/opt"
BINARY_NAME="turbo_relay_arm64"
BINARY_PATH="$SCRIPT_DIR/$BINARY_NAME"
LOG_FILE="/var/log/turborelay.log"
PID_FILE="/var/run/turborelay.pid"
MAX_LOG_SIZE=10485760  # 10MB (10 * 1024 * 1024)

# 日志管理函数
manage_log_size() {
    # 管理主日志文件
    if [ -f "$LOG_FILE" ]; then
        local log_size=$(stat -f%z "$LOG_FILE" 2>/dev/null || stat -c%s "$LOG_FILE" 2>/dev/null || echo 0)
        if [ "$log_size" -gt "$MAX_LOG_SIZE" ]; then
            # 保留后一半日志，删除前一半
            local total_lines=$(wc -l < "$LOG_FILE")
            local keep_lines=$((total_lines / 2))
            tail -n "$keep_lines" "$LOG_FILE" > "$LOG_FILE.tmp"
            mv "$LOG_FILE.tmp" "$LOG_FILE"
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] 主日志文件过大，已清理前半部分 (保留 $keep_lines 行)" >> "$LOG_FILE"
        fi
    fi
}

# 日志函数
log() {
    manage_log_size
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

# 检查程序是否存在
check_binary() {
    if [ ! -f "$BINARY_PATH" ]; then
        log "错误: 找不到程序文件 $BINARY_PATH"
        exit 1
    fi
    
    if [ ! -x "$BINARY_PATH" ]; then
        log "设置程序执行权限..."
        chmod +x "$BINARY_PATH"
    fi
}

# 启动网络接口
setup_network() {
    log "启动网络接口..."
    
    # 启动 eth1 (远端网卡)
    if ifconfig eth1 up 2>/dev/null; then
        log "eth1 (远端) 接口启动成功"
    else
        log "警告: eth1 (远端) 接口启动失败"
    fi
    
    # 启动 eth2 (近端网卡)
    if ifconfig eth2 up 2>/dev/null; then
        log "eth2 (近端) 接口启动成功"
    else
        log "警告: eth2 (近端) 接口启动失败"
    fi
    
    # 等待接口稳定
    sleep 2
    
    # 显示接口状态
    log "网络接口状态:"
    local eth1_status=$(ifconfig eth1 2>/dev/null | grep -E "(inet|UP|RUNNING)" | tr '\n' ' ' || echo "无法获取状态")
    local eth2_status=$(ifconfig eth2 2>/dev/null | grep -E "(inet|UP|RUNNING)" | tr '\n' ' ' || echo "无法获取状态")
    log "eth1 (远端): $eth1_status"
    log "eth2 (近端): $eth2_status"
}

# 启动 TurboRelay 程序
start_turborelay() {
    log "启动 TurboRelay 程序 (eth2=近端, eth1=远端)..."
    
    cd "$SCRIPT_DIR"
    
    # 启动程序并记录PID (生产环境，无debug，无日志输出)
    # 参数顺序: eth2(近端) eth1(远端)
    nohup "$BINARY_PATH" eth2 eth1 >/dev/null 2>&1 &
    local pid=$!
    
    echo $pid > "$PID_FILE"
    log "TurboRelay 已启动, PID: $pid"
    
    # 等待一下确认程序正常启动
    sleep 3
    
    if kill -0 $pid 2>/dev/null; then
        log "TurboRelay 启动成功"
        return 0
    else
        log "错误: TurboRelay 启动失败"
        return 1
    fi
}

# 检查程序是否运行
is_running() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if kill -0 $pid 2>/dev/null; then
            return 0
        else
            rm -f "$PID_FILE"
            return 1
        fi
    fi
    return 1
}

# 停止程序
stop_turborelay() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        log "停止 TurboRelay 程序 (PID: $pid)..."
        
        if kill $pid 2>/dev/null; then
            # 等待程序正常退出
            local count=0
            while kill -0 $pid 2>/dev/null && [ $count -lt 10 ]; do
                sleep 1
                count=$((count + 1))
            done
            
            # 如果还没退出，强制杀死
            if kill -0 $pid 2>/dev/null; then
                log "强制停止程序..."
                kill -9 $pid 2>/dev/null || true
            fi
        fi
        
        rm -f "$PID_FILE"
        log "TurboRelay 已停止"
    fi
}

# 监控循环
monitor_loop() {
    log "开始监控 TurboRelay 程序..."
    local check_count=0
    
    while true; do
        if ! is_running; then
            log "检测到程序未运行，正在重启..."
            
            # 清理可能的残留进程
            pkill -f "$BINARY_NAME" 2>/dev/null || true
            sleep 2
            
            # 重新启动程序
            if start_turborelay; then
                log "程序重启成功"
            else
                log "程序重启失败，30秒后重试..."
                sleep 30
                continue
            fi
        fi
        
        # 每5分钟检查一次日志文件大小 (100次循环 * 3秒 = 5分钟)
        check_count=$((check_count + 1))
        if [ $check_count -ge 100 ]; then
            manage_log_size
            check_count=0
        fi
        
        # 每1秒检查一次
        sleep 3
    done
}

# 信号处理
cleanup() {
    log "收到退出信号，正在清理..."
    stop_turborelay
    exit 0
}

# 设置信号处理
trap cleanup SIGTERM SIGINT

# 主函数
main() {
    log "========================================="
    log "TurboRelay 加速器启动脚本开始运行"
    log "网卡配置: eth2=近端, eth1=远端"
    log "========================================="
    
    # 检查程序文件
    check_binary
    
    # 设置网络接口
    setup_network
    
    # 如果程序已经在运行，先停止
    if is_running; then
        log "检测到程序已在运行，先停止..."
        stop_turborelay
    fi
    
    # 启动程序
    if start_turborelay; then
        # 进入监控循环
        monitor_loop
    else
        log "程序启动失败，退出"
        exit 1
    fi
}

# 处理命令行参数
case "${1:-start}" in
    start)
        main
        ;;
    stop)
        log "停止 TurboRelay..."
        stop_turborelay
        ;;
    restart)
        log "重启 TurboRelay..."
        stop_turborelay
        sleep 2
        setup_network
        start_turborelay
        ;;
    status)
        if is_running; then
            local pid=$(cat "$PID_FILE")
            log "TurboRelay 正在运行 (PID: $pid)"
            echo "TurboRelay 正在运行 (PID: $pid)"
        else
            log "TurboRelay 未运行"
            echo "TurboRelay 未运行"
        fi
        ;;
    logs)
        echo "=== TurboRelay 日志 (最后50行) ==="
        tail -n 50 "$LOG_FILE" 2>/dev/null || echo "无日志文件"
        ;;
    *)
        echo "用法: $0 {start|stop|restart|status|logs}"
        echo "  start   - 启动并监控程序 (默认)"
        echo "  stop    - 停止程序"
        echo "  restart - 重启程序"
        echo "  status  - 查看程序状态"
        echo "  logs    - 查看最近日志"
        echo ""
        echo "网卡配置: eth2=近端, eth1=远端"
        echo "日志管理: 主日志文件超过10MB时自动清理前半部分"
        exit 1
        ;;
esac 
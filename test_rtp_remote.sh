#!/bin/bash

# 远程RTP测试脚本
# 在两台机器之间测试RTP序列号窗口逻辑

echo "=== 远程RTP序列号窗口测试 ==="

# 测试环境配置
NEAR_END_IP="10.35.146.109"
NEAR_END_PORT="10810"
FAR_END_IP="10.35.146.7"
FAR_END_PORT="22"

# 测试用的RTP参数
SSRC_HEX="12345678"
BASE_PORT=5004
RTP_PORT=$BASE_PORT

echo "近端设备: $NEAR_END_IP:$NEAR_END_PORT"
echo "远端设备: $FAR_END_IP:$FAR_END_PORT"
echo "RTP测试端口: $RTP_PORT"
echo ""

# 函数：生成RTP包的十六进制数据
generate_rtp_hex() {
    local seq=$1
    local timestamp=$2
    local payload_size=${3:-160}
    
    # RTP头部 (12字节)
    local version_flags="80"  # V=2, P=0, X=0, CC=0
    local marker_pt="08"      # M=0, PT=8 (PCMA)
    local seq_hex=$(printf "%04x" $seq)
    local ts_hex=$(printf "%08x" $timestamp)
    local ssrc_hex="$SSRC_HEX"
    
    # 构建RTP头
    local rtp_header="${version_flags}${marker_pt}${seq_hex}${ts_hex}${ssrc_hex}"
    
    # 生成payload (简单重复模式)
    local payload=""
    for ((i=0; i<payload_size; i++)); do
        payload="${payload}$(printf "%02x" $((i % 256)))"
    done
    
    echo "${rtp_header}${payload}"
}

# 函数：通过SSH发送UDP包
send_udp_via_ssh() {
    local source_host=$1
    local source_port=$2
    local target_ip=$3
    local target_port=$4
    local hex_data=$5
    local description=$6
    
    echo "[$description] $source_host -> $target_ip:$target_port"
    
    # 将十六进制转换为二进制并通过SSH发送
    local ssh_cmd="echo '$hex_data' | xxd -r -p | nc -u -w1 $target_ip $target_port"
    
    if [ "$source_host" = "local" ]; then
        # 本地发送
        eval "$ssh_cmd"
    else
        # 远程发送
        ssh -p $source_port root@$source_host "$ssh_cmd"
    fi
}

# 函数：测试正常序列
test_normal_sequence() {
    echo "=== 测试1: 正常序列 (1,2,3,4,5) ==="
    echo "从远端($FAR_END_IP)发送到近端，测试远端包处理逻辑"
    
    for seq in 1 2 3 4 5; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$hex_data" "远端->近端"
        sleep 0.5
    done
    
    echo ""
}

# 函数：测试乱序序列
test_out_of_order() {
    echo "=== 测试2: 乱序序列 (10,12,11,13,15,14) ==="
    echo "测试序列号窗口的乱序处理能力"
    
    for seq in 10 12 11 13 15 14; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$hex_data" "远端->近端"
        sleep 0.8
    done
    
    echo ""
}

# 函数：测试丢包场景
test_packet_loss() {
    echo "=== 测试3: 丢包场景 (20,21,22,25,26) - 缺失23,24 ==="
    echo "测试NACK机制"
    
    for seq in 20 21 22 25 26; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$hex_data" "远端->近端"
        sleep 1.0
    done
    
    echo "等待NACK超时处理..."
    sleep 3
    
    # 补发丢失的包
    echo "补发丢失的包:"
    for seq in 23 24; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "补发序列号: $seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$hex_data" "远端->近端"
        sleep 0.5
    done
    
    echo ""
}

# 函数：测试序列号回绕
test_sequence_wrap() {
    echo "=== 测试4: 序列号回绕 (65533,65534,65535,0,1,2) ==="
    echo "测试16位序列号回绕处理"
    
    for seq in 65533 65534 65535 0 1 2; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$hex_data" "远端->近端"
        sleep 0.5
    done
    
    echo ""
}

# 函数：测试近端包
test_near_end_packets() {
    echo "=== 测试5: 近端包测试 (100,101,102,103,104) ==="
    echo "从近端发送，测试直接转发逻辑"
    
    for seq in 100 101 102 103 104; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "发送序列号: $seq (近端)"
        send_udp_via_ssh "$NEAR_END_IP" "$NEAR_END_PORT" "$FAR_END_IP" "$RTP_PORT" "$hex_data" "近端->远端"
        sleep 0.5
    done
    
    echo ""
}

# 函数：测试双向流量
test_bidirectional() {
    echo "=== 测试6: 双向流量测试 ==="
    echo "同时从两端发送，测试SSRC冲突处理"
    
    # 近端发送序列号 200-204
    for seq in 200 201 202 203 204; do
        timestamp=$((seq * 160))
        hex_data=$(generate_rtp_hex $seq $timestamp)
        
        echo "近端发送序列号: $seq"
        send_udp_via_ssh "$NEAR_END_IP" "$NEAR_END_PORT" "$FAR_END_IP" "$RTP_PORT" "$hex_data" "近端->远端" &
        
        # 同时远端发送序列号 300+
        far_seq=$((seq + 100))
        far_timestamp=$((far_seq * 160))
        far_hex_data=$(generate_rtp_hex $far_seq $far_timestamp)
        
        echo "远端发送序列号: $far_seq"
        send_udp_via_ssh "$FAR_END_IP" "$FAR_END_PORT" "$NEAR_END_IP" "$RTP_PORT" "$far_hex_data" "远端->近端" &
        
        sleep 1.0
        wait  # 等待并发发送完成
    done
    
    echo ""
}

# 函数：显示统计信息
show_statistics() {
    echo "=== 获取统计信息 ==="
    echo "请在turbo_relay程序中查看统计信息，或者发送SIGUSR1信号获取统计报告"
    echo ""
}

# 主测试流程
main() {
    echo "开始远程RTP序列号窗口测试..."
    echo "请确保:"
    echo "1. turbo_relay程序在目标设备上运行"
    echo "2. 网络连接正常"
    echo "3. SSH密钥已配置"
    echo ""
    
    read -p "按Enter键开始测试..."
    
    test_normal_sequence
    sleep 3
    
    test_out_of_order
    sleep 3
    
    test_packet_loss
    sleep 3
    
    test_sequence_wrap
    sleep 3
    
    test_near_end_packets
    sleep 3
    
    test_bidirectional
    sleep 3
    
    show_statistics
    
    echo "=== 测试完成 ==="
    echo ""
    echo "验证要点:"
    echo "1. 正常序列应该按顺序转发"
    echo "2. 乱序包应该被缓存并重新排序"
    echo "3. 丢包应该触发NACK请求"
    echo "4. 序列号回绕应该正确处理"
    echo "5. 近端包应该直接转发并缓存"
    echo "6. 双向流量应该正确区分方向"
    echo ""
    echo "请检查turbo_relay的调试日志以验证上述行为"
}

# 检查依赖
check_dependencies() {
    local missing_deps=()
    
    if ! command -v ssh &> /dev/null; then
        missing_deps+=("ssh")
    fi
    
    if ! command -v nc &> /dev/null; then
        missing_deps+=("netcat")
    fi
    
    if ! command -v xxd &> /dev/null; then
        missing_deps+=("xxd")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        echo "错误: 缺少以下依赖:"
        printf '%s\n' "${missing_deps[@]}"
        exit 1
    fi
}

# 测试连接
test_connections() {
    echo "测试SSH连接..."
    
    if ! ssh -p $FAR_END_PORT -o ConnectTimeout=5 root@$FAR_END_IP "echo 'Far-end connection OK'"; then
        echo "警告: 无法连接到远端设备 $FAR_END_IP:$FAR_END_PORT"
        echo "将跳过需要远端发送的测试"
        return 1
    fi
    
    if ! ssh -p $NEAR_END_PORT -o ConnectTimeout=5 root@$NEAR_END_IP "echo 'Near-end connection OK'"; then
        echo "警告: 无法连接到近端设备 $NEAR_END_IP:$NEAR_END_PORT"
        echo "将跳过需要近端发送的测试"
        return 1
    fi
    
    echo "连接测试通过"
    return 0
}

# 运行前检查
check_dependencies
test_connections

# 运行测试
main "$@" 
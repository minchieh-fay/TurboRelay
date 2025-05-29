#!/bin/bash

# RTP序列号窗口测试脚本
# 测试新的滑动窗口逻辑

echo "=== RTP序列号窗口测试 ==="

# 测试环境配置
NEAR_END="10.35.146.9:10810"  # 近端（终端）
FAR_END="10.35.146.7:22"      # 远端（MCU）

# 测试用的RTP参数
SSRC="0x12345678"
BASE_PORT=5004
RTP_PORT=$BASE_PORT
RTCP_PORT=$((BASE_PORT + 1))

echo "近端设备: $NEAR_END"
echo "远端设备: $FAR_END"
echo "RTP端口: $RTP_PORT"
echo "RTCP端口: $RTCP_PORT"
echo ""

# 函数：生成RTP包
generate_rtp_packet() {
    local seq=$1
    local timestamp=$2
    local payload_size=${3:-160}  # 默认160字节payload
    
    # RTP头部 (12字节)
    # V=2, P=0, X=0, CC=0, M=0, PT=8 (PCMA), Seq=$seq, TS=$timestamp, SSRC=$SSRC
    printf "\x80\x08"                           # V=2, P=0, X=0, CC=0, M=0, PT=8
    printf "\\x%02x\\x%02x" $((seq >> 8)) $((seq & 0xFF))  # 序列号
    printf "\\x%02x\\x%02x\\x%02x\\x%02x" \
        $((timestamp >> 24)) $(((timestamp >> 16) & 0xFF)) \
        $(((timestamp >> 8) & 0xFF)) $((timestamp & 0xFF))  # 时间戳
    printf "\\x12\\x34\\x56\\x78"               # SSRC
    
    # Payload (模拟音频数据)
    for ((i=0; i<payload_size; i++)); do
        printf "\\x%02x" $((i % 256))
    done
}

# 函数：发送UDP包
send_udp_packet() {
    local target_ip=$1
    local target_port=$2
    local data=$3
    local source_desc=$4
    
    echo "[$source_desc] 发送到 $target_ip:$target_port"
    echo -n "$data" | nc -u -w1 $target_ip $target_port
}

# 函数：测试正常序列
test_normal_sequence() {
    echo "=== 测试1: 正常序列 (1,2,3,4,5) ==="
    
    for seq in 1 2 3 4 5; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        # 从远端发送到近端（测试远端包处理逻辑）
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.1
    done
    
    echo ""
}

# 函数：测试乱序序列
test_out_of_order() {
    echo "=== 测试2: 乱序序列 (10,12,11,13,15,14) ==="
    
    for seq in 10 12 11 13 15 14; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.2  # 稍长间隔观察缓存行为
    done
    
    echo ""
}

# 函数：测试丢包场景
test_packet_loss() {
    echo "=== 测试3: 丢包场景 (20,21,22,25,26) - 缺失23,24 ==="
    
    for seq in 20 21 22 25 26; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.3  # 更长间隔触发NACK
    done
    
    echo "等待NACK超时..."
    sleep 2
    
    # 发送丢失的包
    echo "补发丢失的包:"
    for seq in 23 24; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "补发序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.1
    done
    
    echo ""
}

# 函数：测试序列号回绕
test_sequence_wrap() {
    echo "=== 测试4: 序列号回绕 (65533,65534,65535,0,1,2) ==="
    
    for seq in 65533 65534 65535 0 1 2; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.1
    done
    
    echo ""
}

# 函数：测试重复包
test_duplicate_packets() {
    echo "=== 测试5: 重复包 (30,31,31,32,32,33) ==="
    
    for seq in 30 31 31 32 32 33; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.1
    done
    
    echo ""
}

# 函数：测试大跳跃
test_large_jump() {
    echo "=== 测试6: 大跳跃重置 (40,41,42,1000,1001) ==="
    
    for seq in 40 41 42 1000 1001; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq"
        send_udp_packet "127.0.0.1" $RTP_PORT "$packet" "远端->近端"
        sleep 0.1
    done
    
    echo ""
}

# 函数：测试近端包（应该直接转发）
test_near_end_packets() {
    echo "=== 测试7: 近端包测试 (直接转发) ==="
    
    for seq in 100 101 102 103 104; do
        timestamp=$((seq * 160))
        packet=$(generate_rtp_packet $seq $timestamp)
        
        echo "发送序列号: $seq (近端)"
        # 这里应该发送到turbo_relay的近端接口
        # 实际测试时需要根据具体网络配置调整
        send_udp_packet "127.0.0.1" $((RTP_PORT + 100)) "$packet" "近端->远端"
        sleep 0.1
    done
    
    echo ""
}

# 主测试流程
main() {
    echo "开始RTP序列号窗口测试..."
    echo "请确保turbo_relay程序正在运行并监听相应端口"
    echo ""
    
    read -p "按Enter键开始测试..."
    
    test_normal_sequence
    sleep 2
    
    test_out_of_order
    sleep 2
    
    test_packet_loss
    sleep 2
    
    test_sequence_wrap
    sleep 2
    
    test_duplicate_packets
    sleep 2
    
    test_large_jump
    sleep 2
    
    test_near_end_packets
    
    echo "=== 测试完成 ==="
    echo "请检查turbo_relay的日志输出，验证:"
    echo "1. 正常序列是否按顺序转发"
    echo "2. 乱序包是否正确缓存和重排"
    echo "3. 丢包是否触发NACK"
    echo "4. 序列号回绕是否正确处理"
    echo "5. 重复包是否被丢弃"
    echo "6. 大跳跃是否触发窗口重置"
    echo "7. 近端包是否直接转发"
}

# 检查依赖
if ! command -v nc &> /dev/null; then
    echo "错误: 需要安装netcat (nc)"
    exit 1
fi

# 运行测试
main "$@" 
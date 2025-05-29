#!/bin/bash

# 双向联动RTP测试脚本
# 验证turbo_relay的双向转发功能

echo "=== TurboRelay双向联动测试 ==="

NEAR_END_IP="10.35.146.109"
NEAR_END_PORT="10810"
FAR_END_IP="10.35.146.7"
FAR_END_PORT="22"
TEST_PORT="5004"

echo "测试环境:"
echo "  近端设备: $NEAR_END_IP:$NEAR_END_PORT"
echo "  远端设备: $FAR_END_IP:$FAR_END_PORT"
echo "  测试端口: $TEST_PORT"
echo ""

# 传输接收器脚本到两台机器
echo "传输测试脚本到两台机器..."
scp -P $NEAR_END_PORT rtp_receiver.py root@$NEAR_END_IP:/root/
scp -P $FAR_END_PORT rtp_receiver.py root@$FAR_END_IP:/root/

echo ""
echo "请选择测试场景:"
echo "1. 测试1: 近端发送 -> 远端接收 (验证近端包直接转发)"
echo "2. 测试2: 远端发送乱序包 -> 近端接收有序包 (验证远端包重排)"
echo "3. 测试3: 双向同时测试 (验证SSRC冲突处理)"
echo "4. 测试4: 丢包重传测试"
echo "0. 退出"
echo ""

read -p "请选择测试 (0-4): " choice

case $choice in
    1)
        echo ""
        echo "=== 测试1: 近端发送 -> 远端接收 ==="
        echo "这个测试验证近端包是否能正确转发到远端"
        echo ""
        
        # 在远端启动接收器
        echo "在远端($FAR_END_IP)启动接收器..."
        ssh -p $FAR_END_PORT root@$FAR_END_IP "python3 /root/rtp_receiver.py $TEST_PORT 20" &
        RECEIVER_PID=$!
        
        sleep 2
        
        # 在近端发送包
        echo "在近端($NEAR_END_IP)发送RTP包..."
        ssh -p $NEAR_END_PORT root@$NEAR_END_IP "python3 -c \"
import socket, struct, time
def create_rtp_packet(seq, ts, ssrc=0x11111111):
    return struct.pack('!BBHII', 0x80, 8, seq, ts, ssrc) + bytes(160)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
print('从近端发送正常序列包...')
for seq in range(1, 6):
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$FAR_END_IP', $TEST_PORT))
    print(f'发送序列号: {seq}')
    time.sleep(1)
sock.close()
\""
        
        # 等待接收器完成
        wait $RECEIVER_PID
        echo "测试1完成"
        ;;
        
    2)
        echo ""
        echo "=== 测试2: 远端发送乱序包 -> 近端接收有序包 ==="
        echo "这个测试验证远端乱序包是否能被重排后转发到近端"
        echo ""
        
        # 在近端启动接收器
        echo "在近端($NEAR_END_IP)启动接收器..."
        ssh -p $NEAR_END_PORT root@$NEAR_END_IP "python3 /root/rtp_receiver.py $TEST_PORT 25" &
        RECEIVER_PID=$!
        
        sleep 2
        
        # 在远端发送乱序包
        echo "在远端($FAR_END_IP)发送乱序RTP包..."
        ssh -p $FAR_END_PORT root@$FAR_END_IP "python3 -c \"
import socket, struct, time
def create_rtp_packet(seq, ts, ssrc=0x22222222):
    return struct.pack('!BBHII', 0x80, 8, seq, ts, ssrc) + bytes(160)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
print('从远端发送乱序包...')
sequence = [10, 12, 11, 13, 15, 14]  # 乱序
for seq in sequence:
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$NEAR_END_IP', $TEST_PORT))
    print(f'发送序列号: {seq}')
    time.sleep(2)
sock.close()
\""
        
        # 等待接收器完成
        wait $RECEIVER_PID
        echo "测试2完成"
        ;;
        
    3)
        echo ""
        echo "=== 测试3: 双向同时测试 ==="
        echo "这个测试验证双向流量的SSRC冲突处理"
        echo ""
        
        # 在两端都启动接收器
        echo "在两端启动接收器..."
        ssh -p $NEAR_END_PORT root@$NEAR_END_IP "python3 /root/rtp_receiver.py $TEST_PORT 30" &
        NEAR_RECEIVER_PID=$!
        
        ssh -p $FAR_END_PORT root@$FAR_END_IP "python3 /root/rtp_receiver.py $TEST_PORT 30" &
        FAR_RECEIVER_PID=$!
        
        sleep 2
        
        # 近端发送
        echo "近端开始发送..."
        ssh -p $NEAR_END_PORT root@$NEAR_END_IP "python3 -c \"
import socket, struct, time
def create_rtp_packet(seq, ts, ssrc=0x33333333):
    return struct.pack('!BBHII', 0x80, 8, seq, ts, ssrc) + bytes(160)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
for seq in range(100, 105):
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$FAR_END_IP', $TEST_PORT))
    print(f'近端发送序列号: {seq}')
    time.sleep(2)
sock.close()
\"" &
        NEAR_SENDER_PID=$!
        
        sleep 1
        
        # 远端发送
        echo "远端开始发送..."
        ssh -p $FAR_END_PORT root@$FAR_END_IP "python3 -c \"
import socket, struct, time
def create_rtp_packet(seq, ts, ssrc=0x44444444):
    return struct.pack('!BBHII', 0x80, 8, seq, ts, ssrc) + bytes(160)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
for seq in range(200, 205):
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$NEAR_END_IP', $TEST_PORT))
    print(f'远端发送序列号: {seq}')
    time.sleep(2)
sock.close()
\"" &
        FAR_SENDER_PID=$!
        
        # 等待所有进程完成
        wait $NEAR_SENDER_PID
        wait $FAR_SENDER_PID
        wait $NEAR_RECEIVER_PID
        wait $FAR_RECEIVER_PID
        
        echo "测试3完成"
        ;;
        
    4)
        echo ""
        echo "=== 测试4: 丢包重传测试 ==="
        echo "这个测试验证丢包检测和NACK机制"
        echo ""
        
        # 在近端启动接收器
        echo "在近端($NEAR_END_IP)启动接收器..."
        ssh -p $NEAR_END_PORT root@$NEAR_END_IP "python3 /root/rtp_receiver.py $TEST_PORT 40" &
        RECEIVER_PID=$!
        
        sleep 2
        
        # 在远端发送丢包序列
        echo "在远端($FAR_END_IP)发送丢包序列..."
        ssh -p $FAR_END_PORT root@$FAR_END_IP "python3 -c \"
import socket, struct, time
def create_rtp_packet(seq, ts, ssrc=0x55555555):
    return struct.pack('!BBHII', 0x80, 8, seq, ts, ssrc) + bytes(160)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
print('发送丢包序列: 20,21,22,25,26 (缺失23,24)')
for seq in [20, 21, 22, 25, 26]:
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$NEAR_END_IP', $TEST_PORT))
    print(f'发送序列号: {seq}')
    time.sleep(3)
print('等待NACK超时...')
time.sleep(5)
print('补发丢失的包: 23, 24')
for seq in [23, 24]:
    packet = create_rtp_packet(seq, seq*160)
    sock.sendto(packet, ('$NEAR_END_IP', $TEST_PORT))
    print(f'补发序列号: {seq}')
    time.sleep(1)
sock.close()
\""
        
        # 等待接收器完成
        wait $RECEIVER_PID
        echo "测试4完成"
        ;;
        
    0)
        echo "退出测试"
        exit 0
        ;;
        
    *)
        echo "无效选择"
        exit 1
        ;;
esac

echo ""
echo "=== 测试完成 ==="
echo ""
echo "验证要点:"
echo "1. 检查接收端的统计信息"
echo "2. 验证序列号是否连续"
echo "3. 验证接收顺序是否正确"
echo "4. 检查turbo_relay的日志(需要U盘拷贝)"
echo ""
echo "如果需要查看turbo_relay的详细日志，请从双网口机器拷贝日志文件" 
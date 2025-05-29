#!/usr/bin/env python3
import socket
import struct
import time

def create_rtp_packet(seq_num, timestamp, ssrc=0x12345678, payload_type=8, payload_size=160):
    version_flags = 0x80
    marker_pt = payload_type & 0x7F
    rtp_header = struct.pack('!BBHII', version_flags, marker_pt, seq_num & 0xFFFF, timestamp & 0xFFFFFFFF, ssrc & 0xFFFFFFFF)
    payload = bytes([(i % 256) for i in range(payload_size)])
    return rtp_header + payload

def send_packet(sock, seq, timestamp, target_ip, target_port, desc):
    packet = create_rtp_packet(seq, timestamp)
    sock.sendto(packet, (target_ip, target_port))
    print(f'[{desc}] 发送序列号 {seq} -> {target_ip}:{target_port}')

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
target_ip = '10.35.146.7'
target_port = 5004

print('=== 近端综合RTP测试 ===')

# 测试1: 正常序列
print('\n--- 测试1: 正常序列 (200-204) ---')
for seq in range(200, 205):
    timestamp = seq * 160
    send_packet(sock, seq, timestamp, target_ip, target_port, '近端终端')
    time.sleep(0.8)

time.sleep(2)

# 测试2: 乱序序列
print('\n--- 测试2: 乱序序列 (210,212,211,213,215,214) ---')
sequence = [210, 212, 211, 213, 215, 214]
for seq in sequence:
    timestamp = seq * 160
    send_packet(sock, seq, timestamp, target_ip, target_port, '近端终端')
    time.sleep(1.0)

time.sleep(2)

# 测试3: 大跳跃
print('\n--- 测试3: 大跳跃 (220,221,222,500,501) ---')
sequence = [220, 221, 222, 500, 501]
for seq in sequence:
    timestamp = seq * 160
    send_packet(sock, seq, timestamp, target_ip, target_port, '近端终端')
    time.sleep(0.8)

time.sleep(2)

# 测试4: 重复包
print('\n--- 测试4: 重复包 (300,301,301,302,302,303) ---')
sequence = [300, 301, 301, 302, 302, 303]
for seq in sequence:
    timestamp = seq * 160
    send_packet(sock, seq, timestamp, target_ip, target_port, '近端终端')
    time.sleep(0.5)

sock.close()
print('\n=== 近端综合测试完成 ===') 
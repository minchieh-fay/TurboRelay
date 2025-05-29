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

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
print('=== 从近端发送RTP测试包 ===')
for seq in range(100, 105):
    timestamp = seq * 160
    packet = create_rtp_packet(seq, timestamp)
    sock.sendto(packet, ('10.35.146.7', 5004))
    print(f'[近端终端] 发送序列号 {seq} -> 10.35.146.7:5004')
    time.sleep(1.0)
sock.close()
print('=== 近端测试完成 ===') 
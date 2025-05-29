#!/usr/bin/env python3
"""
从测试机发送乱序RTP包到目标机
测试加速器是否进行包重排序
"""

import socket
import struct
import time
import random

class RTPPacket:
    def __init__(self, ssrc: int, seq: int, timestamp: int, payload: bytes = b''):
        self.ssrc = ssrc
        self.seq = seq
        self.timestamp = timestamp
        self.payload = payload or b'A' * 100
    
    def to_bytes(self) -> bytes:
        """构造RTP包"""
        header = struct.pack('!BBHII',
            0x80,  # V=2, P=0, X=0, CC=0
            96,    # M=0, PT=96
            self.seq,
            self.timestamp,
            self.ssrc
        )
        return header + self.payload

def send_reordered_rtp_packets():
    """发送乱序RTP包"""
    TARGET_IP = "10.35.146.109"
    TARGET_PORT = 5004
    SSRC = 12345678
    
    print(f"从测试机发送乱序RTP包到 {TARGET_IP}:{TARGET_PORT}")
    print(f"SSRC: {SSRC}")
    print("=" * 50)
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    base_timestamp = int(time.time()) % (2**31)
    
    # 创建乱序序列：100, 101, 102, 103, 104, 105, 106, 107, 108
    # 发送顺序：100, 102, 101, 104, 103, 106, 105, 108, 107
    sequences = [100, 102, 101, 104, 103, 106, 105, 108, 107]
    expected_order = [100, 101, 102, 103, 104, 105, 106, 107, 108]
    
    print(f"发送顺序: {sequences}")
    print(f"期望接收顺序: {expected_order}")
    print()
    
    for i, seq in enumerate(sequences):
        timestamp = (base_timestamp + seq * 3600) % (2**32)
        packet = RTPPacket(SSRC, seq, timestamp)
        
        sock.sendto(packet.to_bytes(), (TARGET_IP, TARGET_PORT))
        print(f"发送: Seq={seq}, TS={timestamp}")
        time.sleep(0.05)  # 50ms间隔
    
    sock.close()
    print("\n乱序RTP包发送完成")
    print("请在109上检查接收顺序是否被重排序")

def send_wrap_around_test():
    """发送序列号回绕测试"""
    TARGET_IP = "10.35.146.109"
    TARGET_PORT = 5004
    SSRC = 87654321
    
    print(f"\n序列号回绕测试")
    print(f"发送到 {TARGET_IP}:{TARGET_PORT}")
    print(f"SSRC: {SSRC}")
    print("=" * 30)
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    base_timestamp = int(time.time()) % (2**31)
    
    # 回绕测试：65534, 65535, 0, 1, 2
    # 乱序发送：65535, 1, 65534, 2, 0
    sequences = [65535, 1, 65534, 2, 0]
    expected_order = [65534, 65535, 0, 1, 2]
    
    print(f"发送顺序: {sequences}")
    print(f"期望接收顺序: {expected_order}")
    print()
    
    for i, seq in enumerate(sequences):
        timestamp = (base_timestamp + i * 3600) % (2**32)
        packet = RTPPacket(SSRC, seq, timestamp)
        
        sock.sendto(packet.to_bytes(), (TARGET_IP, TARGET_PORT))
        print(f"发送: Seq={seq}")
        time.sleep(0.05)
    
    sock.close()
    print("\n回绕测试包发送完成")

def main():
    print("RTP乱序包测试 - 发送端")
    print("测试加速器是否进行包重排序")
    print("=" * 50)
    
    try:
        # 测试1：基本乱序
        send_reordered_rtp_packets()
        
        time.sleep(3)
        
        # 测试2：序列号回绕
        send_wrap_around_test()
        
    except KeyboardInterrupt:
        print("\n测试被中断")
    except Exception as e:
        print(f"错误: {e}")

if __name__ == "__main__":
    main() 
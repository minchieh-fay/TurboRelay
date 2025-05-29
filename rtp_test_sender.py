#!/usr/bin/env python3
"""
RTP测试包发送器
用于测试TurboRelay的RTP序列号窗口逻辑
"""

import socket
import struct
import time
import argparse
import sys

class RTPTestSender:
    def __init__(self, target_ip, target_port, source_desc="Test"):
        self.target_ip = target_ip
        self.target_port = target_port
        self.source_desc = source_desc
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
    def create_rtp_packet(self, seq_num, timestamp, ssrc=0x12345678, payload_type=8, payload_size=160):
        """创建RTP包"""
        # RTP头部 (12字节)
        # V=2, P=0, X=0, CC=0, M=0, PT=payload_type
        version_flags = 0x80  # V=2, P=0, X=0, CC=0
        marker_pt = payload_type & 0x7F  # M=0, PT=payload_type
        
        # 构建RTP头
        rtp_header = struct.pack('!BBHII',
                                version_flags,
                                marker_pt,
                                seq_num & 0xFFFF,
                                timestamp & 0xFFFFFFFF,
                                ssrc & 0xFFFFFFFF)
        
        # 创建payload (简单模式数据)
        payload = bytes([(i % 256) for i in range(payload_size)])
        
        return rtp_header + payload
    
    def send_packet(self, seq_num, timestamp, ssrc=0x12345678):
        """发送单个RTP包"""
        packet = self.create_rtp_packet(seq_num, timestamp, ssrc)
        
        try:
            self.sock.sendto(packet, (self.target_ip, self.target_port))
            print(f"[{self.source_desc}] 发送序列号 {seq_num} -> {self.target_ip}:{self.target_port}")
            return True
        except Exception as e:
            print(f"[{self.source_desc}] 发送失败: {e}")
            return False
    
    def test_normal_sequence(self, start_seq=1, count=5, interval=0.1):
        """测试正常序列"""
        print(f"\n=== 测试正常序列 ({start_seq}-{start_seq+count-1}) ===")
        
        for i in range(count):
            seq = start_seq + i
            timestamp = seq * 160  # 假设8kHz采样率，20ms帧
            self.send_packet(seq, timestamp)
            time.sleep(interval)
    
    def test_out_of_order(self, base_seq=10, interval=0.2):
        """测试乱序序列"""
        print(f"\n=== 测试乱序序列 ===")
        
        # 发送顺序: 10, 12, 11, 13, 15, 14
        sequence = [base_seq, base_seq+2, base_seq+1, base_seq+3, base_seq+5, base_seq+4]
        
        for seq in sequence:
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(interval)
    
    def test_packet_loss(self, base_seq=20, interval=0.3):
        """测试丢包场景"""
        print(f"\n=== 测试丢包场景 ===")
        
        # 发送: 20, 21, 22, 25, 26 (缺失 23, 24)
        sequence = [base_seq, base_seq+1, base_seq+2, base_seq+5, base_seq+6]
        
        for seq in sequence:
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(interval)
        
        print("等待NACK超时...")
        time.sleep(2)
        
        # 补发丢失的包
        print("补发丢失的包:")
        for seq in [base_seq+3, base_seq+4]:  # 23, 24
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(0.1)
    
    def test_sequence_wrap(self, interval=0.1):
        """测试序列号回绕"""
        print(f"\n=== 测试序列号回绕 ===")
        
        # 测试回绕: 65533, 65534, 65535, 0, 1, 2
        sequence = [65533, 65534, 65535, 0, 1, 2]
        
        for seq in sequence:
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(interval)
    
    def test_duplicate_packets(self, base_seq=30, interval=0.1):
        """测试重复包"""
        print(f"\n=== 测试重复包 ===")
        
        # 发送: 30, 31, 31, 32, 32, 33
        sequence = [base_seq, base_seq+1, base_seq+1, base_seq+2, base_seq+2, base_seq+3]
        
        for seq in sequence:
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(interval)
    
    def test_large_jump(self, interval=0.1):
        """测试大跳跃"""
        print(f"\n=== 测试大跳跃重置 ===")
        
        # 发送: 40, 41, 42, 1000, 1001
        sequence = [40, 41, 42, 1000, 1001]
        
        for seq in sequence:
            timestamp = seq * 160
            self.send_packet(seq, timestamp)
            time.sleep(interval)
    
    def close(self):
        """关闭socket"""
        self.sock.close()

def main():
    parser = argparse.ArgumentParser(description='RTP测试包发送器')
    parser.add_argument('target_ip', help='目标IP地址')
    parser.add_argument('target_port', type=int, help='目标端口')
    parser.add_argument('--source', default='TestSender', help='源描述')
    parser.add_argument('--test', choices=['all', 'normal', 'ooo', 'loss', 'wrap', 'dup', 'jump'], 
                       default='all', help='测试类型')
    parser.add_argument('--interval', type=float, default=0.5, help='包间隔(秒)')
    
    args = parser.parse_args()
    
    print(f"RTP测试发送器")
    print(f"目标: {args.target_ip}:{args.target_port}")
    print(f"源: {args.source}")
    print(f"测试类型: {args.test}")
    print()
    
    sender = RTPTestSender(args.target_ip, args.target_port, args.source)
    
    try:
        if args.test == 'all':
            sender.test_normal_sequence(interval=args.interval)
            time.sleep(2)
            
            sender.test_out_of_order(interval=args.interval)
            time.sleep(2)
            
            sender.test_packet_loss(interval=args.interval)
            time.sleep(2)
            
            sender.test_sequence_wrap(interval=args.interval)
            time.sleep(2)
            
            sender.test_duplicate_packets(interval=args.interval)
            time.sleep(2)
            
            sender.test_large_jump(interval=args.interval)
            
        elif args.test == 'normal':
            sender.test_normal_sequence(interval=args.interval)
        elif args.test == 'ooo':
            sender.test_out_of_order(interval=args.interval)
        elif args.test == 'loss':
            sender.test_packet_loss(interval=args.interval)
        elif args.test == 'wrap':
            sender.test_sequence_wrap(interval=args.interval)
        elif args.test == 'dup':
            sender.test_duplicate_packets(interval=args.interval)
        elif args.test == 'jump':
            sender.test_large_jump(interval=args.interval)
        
        print("\n=== 测试完成 ===")
        
    except KeyboardInterrupt:
        print("\n测试被中断")
    except Exception as e:
        print(f"测试出错: {e}")
    finally:
        sender.close()

if __name__ == '__main__':
    main() 
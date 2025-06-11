#!/usr/bin/env python3
import socket
import struct
import time
import threading
import random
import argparse
import sys
from collections import deque

# 默认端口
RTP_PORT = 5544
RTCP_PORT = 5545

class RTPPacket:
    def __init__(self, seq, timestamp, ssrc=12345):
        self.version = 2
        self.padding = 0
        self.extension = 0
        self.cc = 0
        self.marker = 0
        self.payload_type = 96  # Dynamic payload type
        self.seq = seq
        self.timestamp = timestamp
        self.ssrc = ssrc
        self.payload = b'A' * 160  # 假数据
    
    def to_bytes(self):
        # RTP头部（12字节）
        header = struct.pack('!BBHII',
            (self.version << 6) | (self.padding << 5) | (self.extension << 4) | self.cc,
            (self.marker << 7) | self.payload_type,
            self.seq,
            self.timestamp,
            self.ssrc)
        return header + self.payload
    
    @classmethod
    def from_bytes(cls, data):
        if len(data) < 12:
            return None
        
        header = struct.unpack('!BBHII', data[:12])
        seq = header[2]
        timestamp = header[3]
        ssrc = header[4]
        
        return cls(seq, timestamp, ssrc)

class RTCPNACKPacket:
    def __init__(self, ssrc, missing_seq):
        self.version = 2
        self.padding = 0
        self.fmt = 1  # Generic NACK
        self.pt = 205  # RTCP Feedback
        self.length = 3
        self.sender_ssrc = ssrc
        self.media_ssrc = ssrc
        self.pid = missing_seq
        self.blp = 0
    
    def to_bytes(self):
        # 标准RTCP NACK包格式 (RFC 4585)
        header = struct.pack('!BBHII',
            (self.version << 6) | (self.padding << 5) | self.fmt,  # V+P+FMT
            self.pt,                                                # PT
            self.length,                                            # Length
            self.sender_ssrc,                                       # Sender SSRC
            self.media_ssrc)                                        # Media SSRC
        fci = struct.pack('!HH', self.pid, self.blp)              # FCI
        return header + fci
    
    @classmethod
    def from_bytes(cls, data):
        if len(data) < 16:
            return None
        
        header = struct.unpack('!BBHII', data[:12])
        fci = struct.unpack('!HH', data[12:16])
        
        ssrc = header[3]  # Sender SSRC现在是第4个字段(index 3)
        missing_seq = fci[0]
        
        return cls(ssrc, missing_seq)

class RTPSender:
    def __init__(self, target_ip, seq_start=0, loss_rate=0, interval=20, disorder_rate=0):
        self.target_ip = target_ip
        self.seq = seq_start
        self.loss_rate = loss_rate
        self.interval = interval / 1000.0  # 转换为秒
        self.disorder_rate = disorder_rate
        self.timestamp = 0
        self.running = False
        
        # 创建套接字
        self.rtp_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.rtcp_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 发送端也使用固定端口
        try:
            self.rtp_sock.bind(('0.0.0.0', RTP_PORT))
            self.rtcp_sock.bind(('0.0.0.0', RTCP_PORT))
        except OSError as e:
            # 如果端口被占用，尝试使用其他端口（测试时可能发送端和接收端在同一台机器）
            print(f"Warning: Cannot bind to standard ports ({RTP_PORT}, {RTCP_PORT}): {e}")
            print("Using alternative ports for sender...")
            self.rtp_sock.bind(('0.0.0.0', RTP_PORT + 2))  # 5546
            self.rtcp_sock.bind(('0.0.0.0', RTCP_PORT + 2))  # 5547
        
    def start(self):
        self.running = True
        
        # 启动NACK监听线程
        nack_thread = threading.Thread(target=self._nack_listener)
        nack_thread.daemon = True
        nack_thread.start()
        
        # 开始发送RTP包
        self._send_loop()
    
    def stop(self):
        self.running = False
        
    def _send_loop(self):
        while self.running:
            try:
                # 检查是否模拟丢包
                if random.randint(1, 100) <= self.loss_rate:
                    print(f"--------lost-------seq={self.seq}")
                    self._next_seq()
                    time.sleep(self.interval)
                    continue
                
                # 检查是否延迟发送（模拟乱序）
                if random.randint(1, 100) <= self.disorder_rate:
                    # 延迟发送
                    delay_thread = threading.Thread(target=self._delayed_send, args=(self.seq,))
                    delay_thread.daemon = True
                    delay_thread.start()
                    print(f"+++delay send {self.target_ip} seq = {self.seq}")
                else:
                    # 正常发送
                    self._send_rtp_packet(self.seq)
                    print(f"send {self.target_ip} seq = {self.seq}")
                
                self._next_seq()
                time.sleep(self.interval)
                
            except KeyboardInterrupt:
                break
    
    def _delayed_send(self, seq):
        time.sleep(self.interval * 2)  # 延迟2个间隔时间
        self._send_rtp_packet(seq)
    
    def _send_rtp_packet(self, seq):
        packet = RTPPacket(seq, self.timestamp)
        data = packet.to_bytes()
        self.rtp_sock.sendto(data, (self.target_ip, RTP_PORT))
        self.timestamp += 160  # 假设8kHz采样率，20ms间隔
    
    def _next_seq(self):
        self.seq = (self.seq + 1) % 65536  # 处理回环
    
    def _nack_listener(self):
        while self.running:
            try:
                data, addr = self.rtcp_sock.recvfrom(1024)
                nack = RTCPNACKPacket.from_bytes(data)
                if nack:
                    # 重传请求的包
                    self._send_rtp_packet(nack.pid)
                    if hasattr(self, 'nack_response') and self.nack_response:
                        print(f"NACK response: resend seq={nack.pid} to {addr[0]}")
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"NACK listener error: {e}")

class RTPReceiver:
    def __init__(self, nack_rate=0, nack_response=False):
        self.nack_rate = nack_rate
        self.nack_response = nack_response
        self.received_seqs = set()
        self.expected_seq = 0
        self.missing_seqs = set()
        self.running = False
        self.session_started = False
        self.first_seq = None
        self.max_received_seq = None
        
        # 创建套接字
        self.rtp_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.rtcp_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 绑定接收端口
        self.rtp_sock.bind(('0.0.0.0', RTP_PORT))
        self.rtp_sock.settimeout(1.0)
    
    def start(self):
        self.running = True
        self._receive_loop()
    
    def stop(self):
        self.running = False
    
    def _receive_loop(self):
        while self.running:
            try:
                data, addr = self.rtp_sock.recvfrom(1024)
                packet = RTPPacket.from_bytes(data)
                if packet:
                    self._process_packet(packet, addr[0])
            except socket.timeout:
                continue
            except KeyboardInterrupt:
                break
            except Exception as e:
                if self.running:
                    print(f"Receive error: {e}")
    
    def _process_packet(self, packet, sender_ip):
        seq = packet.seq
        
        # 第一次接收包，初始化会话
        if not self.session_started:
            self.session_started = True
            self.first_seq = seq
            self.max_received_seq = seq
            self.received_seqs.add(seq)
            print(f"recv {sender_ip} seq = {seq} []")
            
            # 检查是否对第一个包发送NACK（故意请求重传）
            if self.nack_rate > 0:
                if random.randint(1, 100) <= self.nack_rate:
                    self._send_nack(sender_ip, seq, packet.ssrc)
            return
        
        # 添加到已接收集合
        self.received_seqs.add(seq)
        
        # 从丢失集合中移除（如果存在）
        self.missing_seqs.discard(seq)
        
        # 更新最大序列号
        if self._is_seq_newer(seq, self.max_received_seq):
            # 检测从max_received_seq+1到seq-1之间的丢失包
            self._detect_missing_packets(self.max_received_seq, seq)
            self.max_received_seq = seq
        
        # 打印接收信息
        missing_list = sorted(list(self.missing_seqs))
        if missing_list:
            missing_str = ' '.join(map(str, missing_list))
            print(f"recv {sender_ip} seq = {seq} [{missing_str}]")
        else:
            print(f"recv {sender_ip} seq = {seq} []")
        
        # 检查是否发送NACK（对丢失的包）
        if self.nack_rate > 0 and missing_list:
            for missing_seq in missing_list:
                if random.randint(1, 100) <= self.nack_rate:
                    self._send_nack(sender_ip, missing_seq, packet.ssrc)
        
        # 检查是否对当前收到的包发送NACK（故意请求重传）
        if self.nack_rate > 0:
            if random.randint(1, 100) <= self.nack_rate:
                self._send_nack(sender_ip, seq, packet.ssrc)
    
    def _detect_missing_packets(self, last_seq, current_seq):
        """检测从last_seq+1到current_seq-1之间的丢失包"""
        if last_seq == current_seq:
            return
            
        # 计算序列号差值（考虑回环）
        diff = (current_seq - last_seq) % 65536
        
        # 如果差值太大，可能是序列号回环，不处理
        if diff > 1000:
            return
            
        # 添加中间缺失的序列号
        for i in range(1, diff):
            missing_seq = (last_seq + i) % 65536
            if missing_seq not in self.received_seqs:
                self.missing_seqs.add(missing_seq)
    
    def _is_seq_newer(self, seq1, seq2):
        """检查seq1是否比seq2新（考虑回环）"""
        diff = (seq1 - seq2) % 65536
        return diff < 32768
    
    def _send_nack(self, target_ip, missing_seq, ssrc):
        nack = RTCPNACKPacket(ssrc, missing_seq)
        data = nack.to_bytes()
        self.rtcp_sock.sendto(data, (target_ip, RTCP_PORT))
        print(f"NACK request: seq={missing_seq} to {target_ip}")

def print_help():
    print("RTP/RTCP 测试脚本帮助")
    print("=" * 50)
    print()
    print("发送端（s）参数：")
    print("  -ip <IP>     : （必须）对端IP地址")
    print("  --seq <N>    : 发送的开始seq（默认：0）")
    print("  --loss <N>   : 丢包概率百分比（默认：0，范围：0-100）")
    print("  -t <N>       : 发送RTP包的时间间隔，单位ms（默认：20）")
    print("  -k <N>       : 乱序概率百分比（默认：0，延迟2个间隔发送）")
    print("  -x           : 开启NACK响应（收到NACK后重传包）")
    print()
    print("接收端（r）参数：")
    print("  -n <N>       : NACK发送概率百分比（默认：0）")
    print()
    print("通用参数：")
    print("  默认RTP端口：5544")
    print("  默认RTCP端口：5545")
    print()
    print("使用示例：")
    print("  发送端：python3 rtp_test.py s -ip 192.168.1.100 --loss 5 -k 3 -x")
    print("  接收端：python3 rtp_test.py r -n 10")

def main():
    parser = argparse.ArgumentParser(description='RTP/RTCP 测试脚本', add_help=False)
    parser.add_argument('mode', nargs='?', choices=['s', 'r', 'h'], help='模式：s=发送端, r=接收端, h=帮助')
    
    # 发送端参数
    parser.add_argument('-ip', '--ip', help='对端IP地址')
    parser.add_argument('--seq', type=int, default=0, help='开始序列号')
    parser.add_argument('--loss', type=int, default=0, help='丢包概率百分比')
    parser.add_argument('-t', type=int, default=20, help='发送间隔（ms）')
    parser.add_argument('-k', type=int, default=0, help='乱序概率百分比')
    
    # 接收端参数
    parser.add_argument('-n', type=int, default=0, help='NACK发送概率百分比')
    parser.add_argument('-x', action='store_true', help='开启NACK响应')
    
    args = parser.parse_args()
    
    if not args.mode or args.mode == 'h':
        print_help()
        return
    
    if args.mode == 's':
        # 发送端模式
        if not args.ip:
            print("错误：发送端必须指定 -ip 参数")
            return
        
        print(f"启动发送端模式")
        print(f"目标IP: {args.ip}")
        print(f"开始序列号: {args.seq}")
        print(f"丢包概率: {args.loss}%")
        print(f"发送间隔: {args.t}ms")
        print(f"乱序概率: {args.k}%")
        print(f"NACK响应: {'开启' if args.x else '关闭'}")
        print("-" * 40)
        
        sender = RTPSender(args.ip, args.seq, args.loss, args.t, args.k)
        if args.x:
            sender.nack_response = True
        
        # 显示实际使用的端口
        rtp_port = sender.rtp_sock.getsockname()[1]
        rtcp_port = sender.rtcp_sock.getsockname()[1]
        print(f"发送端端口: RTP={rtp_port}, RTCP={rtcp_port}")
        
        try:
            sender.start()
        except KeyboardInterrupt:
            print("\n停止发送")
        finally:
            sender.stop()
    
    elif args.mode == 'r':
        # 接收端模式
        print(f"启动接收端模式")
        print(f"NACK发送概率: {args.n}%")
        print("-" * 40)
        
        receiver = RTPReceiver(args.n, False)  # 接收端不需要nack_response参数
        
        # 显示接收端端口
        rtp_port = receiver.rtp_sock.getsockname()[1]
        print(f"接收端端口: RTP={rtp_port}, RTCP=监听中")
        
        try:
            receiver.start()
        except KeyboardInterrupt:
            print("\n停止接收")
        finally:
            receiver.stop()

if __name__ == '__main__':
    main() 
#!/usr/bin/env python3
"""
Far-end MCU RTP/RTCP Test Program
运行在远端MCU (10.35.146.7)
按照test.md要求实现：
1. 接收流，模拟10%丢包，发送NACK
2. 发送流，模拟乱序和丢包
"""

import socket
import struct
import time
import threading
import random
import sys
from collections import deque
from datetime import datetime

def get_timestamp():
    """获取精确到毫秒的时间戳"""
    return datetime.now().strftime("%H:%M:%S.%f")[:-3]

class FarEndTester:
    def __init__(self, near_end_ip="10.35.146.109", rtp_port=5533, rtcp_port=5534):
        self.near_end_ip = near_end_ip
        self.rtp_port = rtp_port
        self.rtcp_port = rtcp_port
        
        # RTP发送参数
        self.send_ssrc = 0x87654321
        self.send_seq = 0  # 从0开始发送
        self.send_timestamp = random.randint(100000, 200000)
        
        # 接收处理参数
        self.received_seqs = set()  # 已收到的序列号
        self.missing_seqs = set()   # 缺失的序列号
        self.last_continuous_seq = -1  # 最后连续的序列号
        
        # 发送队列（用于模拟乱序，最多延迟3个包）
        self.send_queue = deque()
        self.send_count = 0
        
        # 创建socket
        self.rtp_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.rtcp_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 绑定本地端口
        self.rtp_socket.bind(('0.0.0.0', rtp_port))
        self.rtcp_socket.bind(('0.0.0.0', rtcp_port))
        
        self.running = False
        
        # 近端端口配置
        self.near_end_rtp_port = 5533
        self.near_end_rtcp_port = 5534
        
    def create_rtp_packet(self, seq_num, payload_data=b"Far-end MCU RTP payload"):
        """创建RTP包"""
        version = 2
        padding = 0
        extension = 0
        cc = 0
        marker = 0
        payload_type = 96
        
        byte1 = (version << 6) | (padding << 5) | (extension << 4) | cc
        byte2 = (marker << 7) | payload_type
        
        rtp_header = struct.pack('!BBHII',
                                byte1, byte2, seq_num,
                                self.send_timestamp, self.send_ssrc)
        
        self.send_timestamp += 240  # 假设30ms间隔，8kHz采样率
        return rtp_header + payload_data
    
    def create_rtcp_nack_packet(self, missing_seq):
        """创建RTCP NACK包"""
        version = 2
        padding = 0
        fmt = 1  # Generic NACK
        packet_type = 205  # RTPFB
        length = 3
        
        byte1 = (version << 6) | (padding << 5) | fmt
        sender_ssrc = self.send_ssrc
        media_ssrc = 0x12345678  # 近端的SSRC
        
        rtcp_packet = struct.pack('!BBHIIHH',
                                 byte1, packet_type, length,
                                 sender_ssrc, media_ssrc,
                                 missing_seq, 0)  # PID, BLP
        
        return rtcp_packet
    
    def send_nack(self, missing_seq):
        """发送NACK包"""
        try:
            nack_packet = self.create_rtcp_nack_packet(missing_seq)
            self.rtcp_socket.sendto(nack_packet, (self.near_end_ip, self.near_end_rtcp_port))
            print(f"{get_timestamp()} [Far-end] Sent NACK for seq={missing_seq}")
        except Exception as e:
            print(f"{get_timestamp()} [Far-end] NACK send error: {e}")
    
    def receive_rtcp_packets(self):
        """接收RTCP包，处理NACK请求"""
        print(f"{get_timestamp()} [Far-end] Starting RTCP receiver...")
        self.rtcp_socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.rtcp_socket.recvfrom(1500)
                print(f"{get_timestamp()} [Far-end] Received RTCP packet from {addr}, length={len(data)}")
                self.process_rtcp_packet(data, addr)
                        
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Far-end] RTCP receive error: {e}")
                break
    
    def handle_nack_request(self, requested_seq):
        """处理NACK请求，重传包"""
        try:
            # 直接重新构造对应序列号的RTP包
            retransmit_packet = self.create_rtp_packet(requested_seq, b"Far-end MCU RTP payload (retransmitted)")
            self.rtp_socket.sendto(retransmit_packet, (self.near_end_ip, self.near_end_rtp_port))
            print(f"{get_timestamp()} [Far-end] Retransmitted seq={requested_seq} (reconstructed)")
        except Exception as e:
            print(f"{get_timestamp()} [Far-end] Retransmission error: {e}")
    
    def process_received_seq(self, seq):
        """处理接收到的序列号，检查连续性"""
        # 模拟10%丢包（假装没收到）
        if random.random() < 0.1:
            print(f"{get_timestamp()} [Far-end] Received ssrc 0x12345678 seq {seq} lost (simulated)")
            # 20ms后发送NACK
            threading.Timer(0.02, self.send_nack, args=(seq,)).start()
            return
        
        # 正常处理收到的包
        self.received_seqs.add(seq)
        print(f"{get_timestamp()} [Far-end] Received ssrc 0x12345678 seq {seq}")
        
        # 更新连续序列号
        while (self.last_continuous_seq + 1) in self.received_seqs:
            self.last_continuous_seq += 1
            self.received_seqs.discard(self.last_continuous_seq)
        
        # 检查缺失的包
        self.check_missing_packets()
    
    def check_missing_packets(self):
        """检查并记录缺失的包"""
        # 找出缺失的序列号
        current_missing = set()
        max_received = max(self.received_seqs) if self.received_seqs else self.last_continuous_seq
        
        for seq in range(self.last_continuous_seq + 1, max_received + 1):
            if seq not in self.received_seqs:
                current_missing.add(seq)
        
        self.missing_seqs = current_missing
    
    def print_missing_packets(self):
        """每秒打印缺失的包"""
        while self.running:
            if self.missing_seqs:
                missing_list = sorted(list(self.missing_seqs))
                print(f"{get_timestamp()} [Far-end] Received Lost: {missing_list}")
            time.sleep(1.0)
    
    def receive_packets(self):
        """接收RTP包"""
        print(f"{get_timestamp()} [Far-end] Starting RTP receiver...")
        self.rtp_socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.rtp_socket.recvfrom(1500)
                if len(data) >= 12:
                    # 检查是否是RTCP包（版本2，包类型>=128）
                    version = (data[0] >> 6) & 0x03
                    packet_type = data[1]
                    
                    if version == 2 and packet_type >= 128:
                        # 这是RTCP包，处理它
                        print(f"{get_timestamp()} [Far-end] Received RTCP packet on RTP port from {addr}, length={len(data)}")
                        self.process_rtcp_packet(data, addr)
                    else:
                        # 这是RTP包，正常处理
                        header = struct.unpack('!BBHII', data[:12])
                        seq = header[2]
                        ssrc = header[4]
                        
                        if ssrc == 0x12345678:  # 近端的SSRC
                            self.process_received_seq(seq)
                        
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Far-end] RTP receive error: {e}")
                break
    
    def process_rtcp_packet(self, data, addr):
        """处理RTCP包（可能来自RTP端口或RTCP端口）"""
        if len(data) >= 8:  # 最小RTCP包长度
            # 解析RTCP头
            header = struct.unpack('!BBHII', data[:12])
            version = (header[0] >> 6) & 0x03
            packet_type = header[1]
            length = header[2]
            
            print(f"{get_timestamp()} [Far-end] RTCP packet: version={version}, type={packet_type}, length={length}")
            
            if packet_type == 205:  # RTCP NACK
                if len(data) >= 16:
                    # 解析NACK包
                    nack_data = struct.unpack('!HH', data[12:16])
                    requested_seq = nack_data[0]  # PID
                    
                    print(f"{get_timestamp()} [Far-end] Received NACK request for seq={requested_seq}")
                    self.handle_nack_request(requested_seq)
                else:
                    print(f"{get_timestamp()} [Far-end] NACK packet too short: {len(data)} bytes")
            else:
                print(f"{get_timestamp()} [Far-end] Non-NACK RTCP packet type {packet_type}")
        else:
            print(f"{get_timestamp()} [Far-end] RTCP packet too short: {len(data)} bytes")
    
    def send_rtp_with_reorder(self, packet, seq):
        """发送RTP包，模拟乱序（不超过3个包的延迟）"""
        # 模拟发送丢包（每10个包丢1个）
        if (self.send_count + 1) % 10 == 0:
            print(f"{get_timestamp()} [Far-end] Sent packet seq={seq} dropped (simulated)")
            return
        
        # 模拟乱序：随机延迟0-3个包的时间
        delay_packets = random.randint(0, 3)
        
        # 将包加入发送队列
        self.send_queue.append((packet, seq, delay_packets))
        
        # 处理发送队列
        self.process_send_queue()
    
    def process_send_queue(self):
        """处理发送队列，实现乱序发送"""
        # 减少所有包的延迟计数
        for i in range(len(self.send_queue)):
            packet, seq, delay = self.send_queue[i]
            self.send_queue[i] = (packet, seq, max(0, delay - 1))
        
        # 发送延迟为0的包
        to_send = []
        remaining = deque()
        
        for packet, seq, delay in self.send_queue:
            if delay == 0:
                to_send.append((packet, seq))
            else:
                remaining.append((packet, seq, delay))
        
        self.send_queue = remaining
        
        # 实际发送包
        for packet, seq in to_send:
            try:
                self.rtp_socket.sendto(packet, (self.near_end_ip, self.near_end_rtp_port))
                print(f"{get_timestamp()} [Far-end] Sent RTP seq={seq}")
            except Exception as e:
                print(f"{get_timestamp()} [Far-end] RTP send error: {e}")
    
    def send_rtp_stream(self):
        """发送RTP流，每30ms一个包"""
        print(f"{get_timestamp()} [Far-end] Starting RTP sender...")
        
        while self.running:
            try:
                # 创建RTP包
                rtp_packet = self.create_rtp_packet(self.send_seq)
                
                # 发送包（带乱序和丢包模拟）
                self.send_rtp_with_reorder(rtp_packet, self.send_seq)
                
                self.send_count += 1
                self.send_seq = (self.send_seq + 1) % 65536  # 0-65535循环
                
                time.sleep(0.03)  # 30ms间隔
                
            except Exception as e:
                print(f"{get_timestamp()} [Far-end] RTP send error: {e}")
                break
    
    def start(self):
        """启动测试"""
        print(f"{get_timestamp()} === Far-end MCU RTP Tester ===")
        print(f"{get_timestamp()} Local: 0.0.0.0:{self.rtp_port} (RTP), 0.0.0.0:{self.rtcp_port} (RTCP)")
        print(f"{get_timestamp()} Remote: {self.near_end_ip}:{self.near_end_rtp_port} (RTP), {self.near_end_rtcp_port} (RTCP)")
        print(f"{get_timestamp()} Features:")
        print(f"{get_timestamp()} - Receive: 10% packet loss simulation + NACK")
        print(f"{get_timestamp()} - Send: Reorder (max 3 packets) + 10% drop + 30ms interval")
        print(f"{get_timestamp()} Press Ctrl+C to stop...")
        
        self.running = True
        
        try:
            # 启动线程
            receiver_thread = threading.Thread(target=self.receive_packets)
            sender_thread = threading.Thread(target=self.send_rtp_stream)
            missing_thread = threading.Thread(target=self.print_missing_packets)
            rtcp_thread = threading.Thread(target=self.receive_rtcp_packets)
            
            receiver_thread.start()
            sender_thread.start()
            missing_thread.start()
            rtcp_thread.start()
            
            # 等待中断
            while self.running:
                time.sleep(1)
                
        except KeyboardInterrupt:
            print(f"{get_timestamp()} \n[Far-end] Stopping test...")
            self.running = False
        
        finally:
            self.cleanup()
    
    def cleanup(self):
        """清理资源"""
        self.running = False
        try:
            self.rtp_socket.close()
            self.rtcp_socket.close()
        except:
            pass
        print(f"{get_timestamp()} [Far-end] Cleanup completed")

def main():
    if len(sys.argv) > 1:
        near_end_ip = sys.argv[1]
    else:
        near_end_ip = "10.35.146.109"
    
    tester = FarEndTester(near_end_ip=near_end_ip)
    tester.start()

if __name__ == "__main__":
    main() 
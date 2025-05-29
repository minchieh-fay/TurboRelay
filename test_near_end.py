#!/usr/bin/env python3
"""
Near-end Terminal RTP Test Program
运行在近端终端 (10.35.146.109)
按照test.md要求实现：
1. 接收流，判断是否连续、不乱序、不丢包，出现问题就停止
2. 发送流，正常发送，不模拟乱序和丢包
"""

import socket
import struct
import time
import threading
import sys
from collections import deque
from datetime import datetime

def get_timestamp():
    """获取精确到毫秒的时间戳"""
    return datetime.now().strftime("%H:%M:%S.%f")[:-3]

class NearEndTester:
    def __init__(self, far_end_ip="10.35.146.7", rtp_port=5533, rtcp_port=5534):
        self.far_end_ip = far_end_ip
        self.rtp_port = rtp_port
        self.rtcp_port = rtcp_port
        
        # RTP发送参数
        self.send_ssrc = 0x12345678
        self.send_seq = 0  # 从0开始发送
        self.send_timestamp = 1000
        
        # 接收检查参数
        self.expected_seq = -1  # 期望的下一个序列号
        self.received_count = 0
        self.error_detected = False
        
        # 发送缓存（用于重传）
        self.send_cache = deque(maxlen=1000)  # 缓存最近1000个包
        self.cache_lock = threading.Lock()
        
        # 创建socket
        self.rtp_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.rtcp_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 绑定本地端口
        self.rtp_socket.bind(('0.0.0.0', rtp_port))
        self.rtcp_socket.bind(('0.0.0.0', rtcp_port))
        
        self.running = False
        
    def create_rtp_packet(self, seq_num, payload_data=b"Near-end RTP payload"):
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
    
    def check_sequence_continuity(self, seq):
        """检查序列号连续性，严格不容忍乱序（TurboRelay应该已排序）"""
        print(f"{get_timestamp()} [Near-end] Processing seq={seq}, expected={self.expected_seq % 65536 if self.expected_seq != -1 else 'first'}")
        
        if self.expected_seq == -1:
            # 第一个包
            self.expected_seq = seq + 1
            print(f"{get_timestamp()} [Near-end] ✓ First packet received, seq={seq}, next expected={self.expected_seq % 65536}")
            return True
        
        expected_mod = self.expected_seq % 65536
        
        # 计算序列号差值（考虑回绕）
        diff = seq - expected_mod
        if diff > 32767:
            diff -= 65536
        elif diff < -32767:
            diff += 65536
        
        print(f"{get_timestamp()} [Near-end] Seq analysis: current={seq}, expected={expected_mod}, diff={diff}")
        
        if diff == 0:
            # 正好是期望的包
            self.expected_seq += 1
            print(f"{get_timestamp()} [Near-end] ✓ Perfect in-order packet, next expected={self.expected_seq % 65536}")
            return True
        elif diff > 0:
            # 跳跃了一些包（丢包）
            print(f"{get_timestamp()} [Near-end] ✗ ERROR: Packet loss detected!")
            print(f"{get_timestamp()} [Near-end] Expected seq={expected_mod}, but got seq={seq} (gap={diff})")
            print(f"{get_timestamp()} [Near-end] TurboRelay should have handled packet loss and reordering!")
            print(f"{get_timestamp()} [Near-end] Waiting 3 seconds to see if missing packets arrive...")
            
            # 启动3秒延迟退出
            threading.Timer(1.0, self.delayed_exit).start()
            return False
        else:
            # 乱序包（seq < expected）
            print(f"{get_timestamp()} [Near-end] ✗ ERROR: Out-of-order packet detected!")
            print(f"{get_timestamp()} [Near-end] Expected seq={expected_mod}, but got seq={seq} (delay={-diff})")
            print(f"{get_timestamp()} [Near-end] TurboRelay should have reordered packets before forwarding!")
            print(f"{get_timestamp()} [Near-end] Waiting 3 seconds to see if packets get reordered...")
            
            # 启动3秒延迟退出
            threading.Timer(3.0, self.delayed_exit).start()
            return False
    
    def delayed_exit(self):
        """1秒后退出程序"""
        print(f"{get_timestamp()} [Near-end] 1-second observation period ended, stopping test...")
        self.error_detected = True
        self.running = False
    
    def cache_packet(self, seq_num, packet):
        """缓存发送的包用于重传"""
        with self.cache_lock:
            self.send_cache.append((seq_num, packet))
    
    def find_cached_packet(self, seq_num):
        """从缓存中查找指定序列号的包"""
        with self.cache_lock:
            for cached_seq, cached_packet in self.send_cache:
                if cached_seq == seq_num:
                    return cached_packet
        return None
    
    def process_rtcp_nack(self, data):
        """处理RTCP NACK包"""
        if len(data) < 12:
            return
            
        try:
            # 解析RTCP头部
            header = struct.unpack('!BBHI', data[:8])
            packet_type = header[1]
            
            if packet_type == 205:  # RTPFB (包含NACK)
                # 解析NACK包
                if len(data) >= 16:
                    nack_data = struct.unpack('!IIHH', data[8:16])
                    sender_ssrc = nack_data[0]
                    media_ssrc = nack_data[1] 
                    pid = nack_data[2]  # 丢失的包序列号
                    
                    if media_ssrc == self.send_ssrc:  # 确认是我们的SSRC
                        print(f"{get_timestamp()} [Near-end] Received NACK for seq={pid}")
                        
                        # 查找并重传包
                        cached_packet = self.find_cached_packet(pid)
                        if cached_packet:
                            try:
                                self.rtp_socket.sendto(cached_packet, (self.far_end_ip, self.rtp_port))
                                print(f"{get_timestamp()} [Near-end] Retransmitted RTP seq={pid}")
                            except Exception as e:
                                print(f"{get_timestamp()} [Near-end] Retransmit error: {e}")
                        else:
                            print(f"{get_timestamp()} [Near-end] Cannot retransmit seq={pid}, not in cache")
                            
        except Exception as e:
            print(f"{get_timestamp()} [Near-end] RTCP parse error: {e}")
    
    def receive_rtcp(self):
        """接收RTCP包"""
        print(f"{get_timestamp()} [Near-end] Starting RTCP receiver...")
        self.rtcp_socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.rtcp_socket.recvfrom(1500)
                if len(data) >= 8:
                    self.process_rtcp_nack(data)
                    
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Near-end] RTCP receive error: {e}")
                break
        
        print(f"{get_timestamp()} [Near-end] RTCP receiver stopped")
    
    def receive_packets(self):
        """接收RTP包并检查连续性"""
        print(f"{get_timestamp()} [Near-end] Starting RTP receiver...")
        self.rtp_socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.rtp_socket.recvfrom(1500)
                if len(data) >= 12:
                    header = struct.unpack('!BBHII', data[:12])
                    seq = header[2]
                    ssrc = header[4]
                    
                    if ssrc == 0x87654321:  # 远端的SSRC
                        self.received_count += 1
                        print(f"{get_timestamp()} [Near-end] Received ssrc 0x{ssrc:08X} seq {seq}")
                        
                        # 检查连续性
                        if not self.check_sequence_continuity(seq):
                            # 检测到错误，但不立即退出，继续观察1秒
                            print(f"{get_timestamp()} [Near-end] ERROR DETECTED! Continuing to observe for 1 seconds...")
                            # 不设置 self.running = False，让delayed_exit来处理
                        
                        if self.received_count % 100 == 0:
                            print(f"{get_timestamp()} [Near-end] Received {self.received_count} packets, all in order")
                            
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Near-end] RTP receive error: {e}")
                break
        
        if self.error_detected:
            print(f"{get_timestamp()} [Near-end] Test FAILED! Error detected after {self.received_count} packets")
        else:
            print(f"{get_timestamp()} [Near-end] Receiver stopped. Total received: {self.received_count} packets")
    
    def send_rtp_stream(self):
        """发送RTP流，每30ms一个包，正常发送不模拟问题"""
        print(f"{get_timestamp()} [Near-end] Starting RTP sender...")
        
        sent_count = 0
        
        while self.running:
            try:
                # 创建RTP包
                rtp_packet = self.create_rtp_packet(self.send_seq)
                
                # 缓存包用于重传
                self.cache_packet(self.send_seq, rtp_packet)
                
                # 正常发送包（不模拟任何网络问题）
                self.rtp_socket.sendto(rtp_packet, (self.far_end_ip, self.rtp_port))
                
                sent_count += 1
                if sent_count % 100 == 0:
                    print(f"{get_timestamp()} [Near-end] Sent {sent_count} RTP packets, seq={self.send_seq}")
                
                self.send_seq = (self.send_seq + 1) % 65536  # 0-65535循环
                
                time.sleep(0.03)  # 30ms间隔
                
            except Exception as e:
                print(f"{get_timestamp()} [Near-end] RTP send error: {e}")
                break
        
        print(f"{get_timestamp()} [Near-end] Sender stopped. Total sent: {sent_count} packets")
    
    def start(self):
        """启动测试"""
        print(f"{get_timestamp()} === Near-end Terminal RTP Tester ===")
        print(f"{get_timestamp()} Local: 0.0.0.0:{self.rtp_port} (RTP), 0.0.0.0:{self.rtcp_port} (RTCP)")
        print(f"{get_timestamp()} Remote: {self.far_end_ip}:{self.rtp_port} (RTP), {self.far_end_ip}:{self.rtcp_port} (RTCP)")
        print(f"{get_timestamp()} Features:")
        print(f"{get_timestamp()} - Receive: Strict sequence check (stop on error)")
        print(f"{get_timestamp()} - Send: Normal transmission + NACK retransmission + 30ms interval")
        print(f"{get_timestamp()} Press Ctrl+C to stop...")
        
        self.running = True
        
        try:
            # 启动线程
            receiver_thread = threading.Thread(target=self.receive_packets)
            sender_thread = threading.Thread(target=self.send_rtp_stream)
            rtcp_thread = threading.Thread(target=self.receive_rtcp)
            
            receiver_thread.start()
            sender_thread.start()
            rtcp_thread.start()
            
            # 等待中断或错误
            while self.running:
                time.sleep(1)
                
        except KeyboardInterrupt:
            print(f"{get_timestamp()} \n[Near-end] Stopping test...")
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
        
        if self.error_detected:
            print(f"{get_timestamp()} [Near-end] Test completed with ERRORS")
            sys.exit(1)
        else:
            print(f"{get_timestamp()} [Near-end] Cleanup completed")

def main():
    if len(sys.argv) > 1:
        far_end_ip = sys.argv[1]
    else:
        far_end_ip = "10.35.146.7"
    
    tester = NearEndTester(far_end_ip=far_end_ip)
    tester.start()

if __name__ == "__main__":
    main() 
#!/usr/bin/env python3
"""
RTP发送端测试脚本
功能：
1. 发送RTP包，支持10%乱序和10%丢包
2. 接收NACK并响应重传（直接重新创建包）
3. RTP和RTCP使用相同端口5533
"""

import socket
import struct
import time
import threading
import sys
import random
from datetime import datetime

def get_timestamp():
    """获取精确到毫秒的时间戳"""
    return datetime.now().strftime("%H:%M:%S.%f")[:-3]

class RTPSender:
    def __init__(self, target_ip, enable_nack_response=True, port=5533):
        self.target_ip = target_ip
        self.port = port
        self.enable_nack_response = enable_nack_response
        
        # RTP参数
        self.ssrc = 0x87654321
        self.seq = 0
        self.timestamp = 1000
        
        # 统计
        self.sent_count = 0
        self.dropped_count = 0
        self.reordered_count = 0
        self.retransmit_count = 0
        
        # 创建socket
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.socket.bind(('0.0.0.0', port))
        
        self.running = False
        
    def create_rtp_packet(self, seq_num, is_retransmit=False):
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
                                self.timestamp, self.ssrc)
        
        # 重传包的payload稍大一些，方便抓包识别
        if is_retransmit:
            payload = f"RETRANSMIT seq={seq_num} " + "X" * 100
        else:
            payload = f"RTP seq={seq_num} data"
        
        return rtp_header + payload.encode()
    
    def send_packet_delayed(self, packet, seq_num):
        """延迟发送包（用于模拟乱序）"""
        delay = random.uniform(0.02, 0.08)  # 20-80ms随机延迟
        time.sleep(delay)
        
        if self.running:
            try:
                self.socket.sendto(packet, (self.target_ip, self.port))
                print(f"{get_timestamp()} [Sender] Sent reordered seq={seq_num} (delay={delay*1000:.1f}ms)")
            except Exception as e:
                print(f"{get_timestamp()} [Sender] Reorder send error: {e}")
    
    def process_nack(self, data):
        """处理NACK包"""
        if not self.enable_nack_response:
            return
            
        if len(data) < 16:
            print(f"{get_timestamp()} [Sender] Packet too short for NACK: {len(data)} bytes")
            return
            
        try:
            # 先打印收到的数据包信息用于调试
            print(f"{get_timestamp()} [Sender] Received packet: len={len(data)}, first_bytes={data[:16].hex()}")
            
            # 直接解析完整的16字节RTCP NACK包
            header = struct.unpack('!BBHIIHH', data[:16])
            version = (header[0] >> 6) & 0x03
            fmt = header[0] & 0x1F
            packet_type = header[1]
            length = header[2]
            sender_ssrc = header[3]
            media_ssrc = header[4]
            pid = header[5]  # 请求重传的序列号
            blp = header[6]  # 位掩码
            
            print(f"{get_timestamp()} [Sender] RTCP packet: version={version}, fmt={fmt}, type={packet_type}, length={length}")
            print(f"{get_timestamp()} [Sender] NACK details: sender_ssrc=0x{sender_ssrc:08X}, "
                  f"media_ssrc=0x{media_ssrc:08X}, pid={pid}, blp=0x{blp:04X}")
            
            # 检查是否是RTCP包
            if version != 2:
                print(f"{get_timestamp()} [Sender] Not RTCP packet (version={version})")
                return
                
            if packet_type == 205:  # RTCP NACK (RTPFB)
                if media_ssrc == self.ssrc:
                    print(f"{get_timestamp()} [Sender] Received NACK for seq={pid}")
                    
                    # 直接重新创建重传包（不需要缓存）
                    retransmit_packet = self.create_rtp_packet(pid, is_retransmit=True)
                    
                    try:
                        self.socket.sendto(retransmit_packet, (self.target_ip, self.port))
                        self.retransmit_count += 1
                        print(f"{get_timestamp()} [Sender] Retransmitted seq={pid} (recreated)")
                    except Exception as e:
                        print(f"{get_timestamp()} [Sender] Retransmit error: {e}")
                else:
                    print(f"{get_timestamp()} [Sender] NACK for different SSRC: 0x{media_ssrc:08X} != 0x{self.ssrc:08X}")
            else:
                print(f"{get_timestamp()} [Sender] Not NACK packet (type={packet_type})")
                        
        except Exception as e:
            print(f"{get_timestamp()} [Sender] NACK parse error: {e}")
            # 打印原始数据用于调试
            print(f"{get_timestamp()} [Sender] Raw data: {data[:16].hex()}")
    
    def receive_nack(self):
        """接收NACK包"""
        print(f"{get_timestamp()} [Sender] Starting NACK receiver (enabled: {self.enable_nack_response})...")
        self.socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.socket.recvfrom(1500)
                print(f"{get_timestamp()} [Sender] Received data from {addr}: {len(data)} bytes")
                
                if len(data) >= 8:
                    self.process_nack(data)
                    
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Sender] NACK receive error: {e}")
                break
        
        print(f"{get_timestamp()} [Sender] NACK receiver stopped")
    
    def send_rtp_stream(self):
        """发送RTP流"""
        print(f"{get_timestamp()} [Sender] Starting RTP sender...")
        print(f"{get_timestamp()} [Sender] Target: {self.target_ip}:{self.port}")
        print(f"{get_timestamp()} [Sender] NACK response: {self.enable_nack_response}")
        print(f"{get_timestamp()} [Sender] Features: 10% drop, 10% reorder (20-80ms delay), 20ms interval")
        
        while self.running:
            try:
                # 10%概率丢包
                if random.random() < 0.1:
                    print(f"{get_timestamp()} [Sender] Dropped seq={self.seq}")
                    self.dropped_count += 1
                    self.seq = (self.seq + 1) % 65536
                    self.timestamp += 160  # 20ms @ 8kHz
                    time.sleep(0.02)
                    continue
                
                # 创建RTP包
                rtp_packet = self.create_rtp_packet(self.seq)
                
                # 10%概率乱序（启动异步线程延迟发送）
                if random.random() < 0.1:
                    self.reordered_count += 1
                    print(f"{get_timestamp()} [Sender] Scheduling reorder seq={self.seq}")
                    
                    # 启动异步线程延迟发送
                    thread = threading.Thread(target=self.send_packet_delayed, 
                                            args=(rtp_packet, self.seq))
                    thread.daemon = True
                    thread.start()
                else:
                    # 正常发送
                    self.socket.sendto(rtp_packet, (self.target_ip, self.port))
                    self.sent_count += 1
                    
                    if self.sent_count % 100 == 0:
                        print(f"{get_timestamp()} [Sender] Sent {self.sent_count} packets, "
                              f"dropped {self.dropped_count}, reordered {self.reordered_count}, "
                              f"retransmitted {self.retransmit_count}")
                
                self.seq = (self.seq + 1) % 65536
                self.timestamp += 160  # 20ms @ 8kHz
                
                time.sleep(0.02)  # 20ms间隔
                
            except Exception as e:
                print(f"{get_timestamp()} [Sender] Send error: {e}")
                break
        
        print(f"{get_timestamp()} [Sender] Stopped. Total sent: {self.sent_count}, "
              f"dropped: {self.dropped_count}, reordered: {self.reordered_count}, "
              f"retransmitted: {self.retransmit_count}")
    
    def start(self):
        """启动发送端"""
        print(f"{get_timestamp()} === RTP Sender ===")
        
        self.running = True
        
        try:
            # 启动线程
            sender_thread = threading.Thread(target=self.send_rtp_stream)
            nack_thread = threading.Thread(target=self.receive_nack)
            
            sender_thread.start()
            nack_thread.start()
            
            # 等待中断
            while self.running:
                time.sleep(1)
                
        except KeyboardInterrupt:
            print(f"{get_timestamp()} \n[Sender] Stopping...")
            self.running = False
        
        finally:
            self.cleanup()
    
    def cleanup(self):
        """清理资源"""
        self.running = False
        try:
            self.socket.close()
        except:
            pass
        print(f"{get_timestamp()} [Sender] Cleanup completed")

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 send.py <target_ip> [enable_nack_response]")
        print("Example: python3 send.py 10.35.146.7")
        print("Example: python3 send.py 10.35.146.7 false")
        sys.exit(1)
    
    target_ip = sys.argv[1]
    enable_nack = True
    
    if len(sys.argv) > 2:
        enable_nack = sys.argv[2].lower() not in ['false', 'no', '0']
    
    sender = RTPSender(target_ip, enable_nack)
    sender.start()

if __name__ == "__main__":
    main() 
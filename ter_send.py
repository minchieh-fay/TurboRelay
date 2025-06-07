#!/usr/bin/env python3
"""
终端发送脚本 (Terminal Sender)
功能：
1. 正常发送RTP包，不模拟乱序和丢包
2. 接收NACK并响应重传
3. 使用端口5533
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

class TerminalSender:
    def __init__(self, target_ip, port=5533):
        self.target_ip = target_ip
        self.port = port
        
        # RTP参数
        self.ssrc = 0x12345678  # 终端SSRC
        self.seq = 0
        self.timestamp = 1000
        
        # 统计
        self.sent_count = 0
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
        
        # 重传包的payload稍大一些，方便识别
        if is_retransmit:
            payload = f"TERMINAL RETRANSMIT seq={seq_num} " + "R" * 80
        else:
            payload = f"TERMINAL RTP seq={seq_num} data"
        
        return rtp_header + payload.encode()
    
    def process_nack(self, data):
        """处理NACK包"""
        if len(data) < 16:
            return
            
        try:
            # 解析完整的16字节RTCP NACK包
            header = struct.unpack('!BBHIIHH', data[:16])
            version = (header[0] >> 6) & 0x03
            packet_type = header[1]
            sender_ssrc = header[3]
            media_ssrc = header[4]
            pid = header[5]  # 请求重传的序列号
            
            if version == 2 and packet_type == 205:  # RTCP NACK
                if media_ssrc == self.ssrc:
                    print(f"{get_timestamp()} [Terminal] Received NACK for seq={pid}")
                    
                    # 重新创建重传包
                    #retransmit_packet = self.create_rtp_packet(pid, is_retransmit=True)
                    
                    #try:
                    #    self.socket.sendto(retransmit_packet, (self.target_ip, self.port))
                    #    self.retransmit_count += 1
                    #    print(f"{get_timestamp()} [Terminal] Retransmitted seq={pid}")
                    #except Exception as e:
                    #    print(f"{get_timestamp()} [Terminal] Retransmit error: {e}")
                        
        except Exception as e:
            print(f"{get_timestamp()} [Terminal] NACK parse error: {e}")
    
    def receive_nack(self):
        """接收NACK包"""
        print(f"{get_timestamp()} [Terminal] Starting NACK receiver...")
        self.socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.socket.recvfrom(1500)
                if len(data) >= 16:
                    self.process_nack(data)
                    
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Terminal] NACK receive error: {e}")
                break
        
        print(f"{get_timestamp()} [Terminal] NACK receiver stopped")
    
    def send_rtp_stream(self):
        """发送RTP流"""
        print(f"{get_timestamp()} [Terminal] Starting RTP sender...")
        print(f"{get_timestamp()} [Terminal] Target: {self.target_ip}:{self.port}")
        print(f"{get_timestamp()} [Terminal] Features: Normal sending, 50ms interval")
        
        while self.running:
            try:
                # 正常发送，不模拟任何问题
                rtp_packet = self.create_rtp_packet(self.seq)
                
                self.socket.sendto(rtp_packet, (self.target_ip, self.port))
                self.sent_count += 1
                
                print(f"{get_timestamp()} [Terminal] Sent seq={self.seq}")
                
                if self.sent_count % 50 == 0:
                    print(f"{get_timestamp()} [Terminal] Stats: sent={self.sent_count}, retransmitted={self.retransmit_count}")
                
                self.seq = (self.seq + 1) % 65536
                self.timestamp += 400  # 50ms @ 8kHz
                
                time.sleep(0.05)  # 50ms间隔
                
            except Exception as e:
                print(f"{get_timestamp()} [Terminal] Send error: {e}")
                break
        
        print(f"{get_timestamp()} [Terminal] Stopped. Total sent: {self.sent_count}, retransmitted: {self.retransmit_count}")
    
    def start(self):
        """启动发送端"""
        print(f"{get_timestamp()} === Terminal Sender ===")
        
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
            print(f"{get_timestamp()} \n[Terminal] Stopping...")
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
        print(f"{get_timestamp()} [Terminal] Cleanup completed")

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 ter_send.py <target_ip>")
        print("Example: python3 ter_send.py 10.35.146.7")
        sys.exit(1)
    
    target_ip = sys.argv[1]
    
    sender = TerminalSender(target_ip)
    sender.start()

if __name__ == "__main__":
    main() 
#!/usr/bin/env python3
"""
MCU接收脚本 (MCU Receiver)
功能：
1. 接收RTP包并打印序列号
2. 每收到5个包，向终端发送NACK要求重传前面某个包（前1-5随机）
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

class MCUReceiver:
    def __init__(self, terminal_ip, port=5533):
        self.terminal_ip = terminal_ip
        self.port = port
        
        # MCU参数
        self.mcu_ssrc = 0x87654321  # MCU SSRC
        
        # 接收统计
        self.received_count = 0
        self.received_seqs = []  # 记录收到的序列号
        self.nack_count = 0
        
        # 创建socket
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.socket.bind(('0.0.0.0', port))
        
        self.running = False
        
    def create_nack_packet(self, seq_to_nack, terminal_ssrc):
        """创建NACK包"""
        version = 2
        padding = 0
        fmt = 1  # Generic NACK
        
        byte1 = (version << 6) | (padding << 5) | fmt
        packet_type = 205  # RTCP NACK
        length = 3  # 16字节包，长度为(16/4)-1=3
        
        sender_ssrc = self.mcu_ssrc
        media_ssrc = terminal_ssrc
        pid = seq_to_nack  # 请求重传的序列号
        blp = 0  # 位掩码，这里只请求一个包
        
        nack_packet = struct.pack('!BBHIIHH',
                                 byte1, packet_type, length,
                                 sender_ssrc, media_ssrc,
                                 pid, blp)
        
        return nack_packet
    
    def send_nack(self, seq_to_nack, terminal_ssrc):
        """发送NACK包"""
        try:
            nack_packet = self.create_nack_packet(seq_to_nack, terminal_ssrc)
            self.socket.sendto(nack_packet, (self.terminal_ip, self.port))
            self.nack_count += 1
            print(f"{get_timestamp()} [MCU] Sent NACK for seq={seq_to_nack}")
        except Exception as e:
            print(f"{get_timestamp()} [MCU] NACK send error: {e}")
    
    def process_rtp_packet(self, data):
        """处理RTP包"""
        if len(data) < 12:
            return
            
        try:
            # 解析RTP头部
            header = struct.unpack('!BBHII', data[:12])
            version = (header[0] >> 6) & 0x03
            seq = header[2]
            timestamp = header[3]
            ssrc = header[4]
            
            if version == 2 and ssrc != 0:
                self.received_count += 1
                self.received_seqs.append(seq)
                
                print(f"{get_timestamp()} [MCU] Received seq={seq} (count={self.received_count})")
                
                # 每收到5个包，发送一次NACK
                if self.received_count % 5 == 0:
                    # 随机选择前1-5个包中的一个进行NACK
                    if len(self.received_seqs) >= 5:
                        # 选择前1-5个包中的一个
                        nack_range = min(5, len(self.received_seqs))
                        nack_index = random.randint(1, nack_range)
                        seq_to_nack = self.received_seqs[-nack_index]
                        
                        print(f"{get_timestamp()} [MCU] Triggering NACK: received {self.received_count} packets, requesting retransmit of seq={seq_to_nack}")
                        self.send_nack(seq_to_nack, ssrc)
                
                # 定期打印统计
                if self.received_count % 20 == 0:
                    print(f"{get_timestamp()} [MCU] Stats: received={self.received_count}, nack_sent={self.nack_count}")
                    
        except Exception as e:
            print(f"{get_timestamp()} [MCU] RTP parse error: {e}")
    
    def receive_packets(self):
        """接收数据包"""
        print(f"{get_timestamp()} [MCU] Starting packet receiver...")
        print(f"{get_timestamp()} [MCU] Listening on port {self.port}")
        print(f"{get_timestamp()} [MCU] Features: Every 5 packets send NACK for random previous packet")
        
        self.socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.socket.recvfrom(1500)
                
                # 判断是RTP包还是其他包
                if len(data) >= 12:
                    # 检查是否是RTP包 (version=2)
                    version = (data[0] >> 6) & 0x03
                    if version == 2:
                        # 进一步检查是否是RTP包（不是RTCP）
                        payload_type = data[1] & 0x7F
                        if payload_type < 128:  # RTP包的payload type通常小于128
                            self.process_rtp_packet(data)
                        # 如果是RTCP包，忽略（我们只关心RTP）
                    
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [MCU] Receive error: {e}")
                break
        
        print(f"{get_timestamp()} [MCU] Receiver stopped. Total received: {self.received_count}, NACK sent: {self.nack_count}")
    
    def start(self):
        """启动接收端"""
        print(f"{get_timestamp()} === MCU Receiver ===")
        print(f"{get_timestamp()} [MCU] Terminal IP: {self.terminal_ip}")
        
        self.running = True
        
        try:
            # 启动接收线程
            receiver_thread = threading.Thread(target=self.receive_packets)
            receiver_thread.start()
            
            # 等待中断
            while self.running:
                time.sleep(1)
                
        except KeyboardInterrupt:
            print(f"{get_timestamp()} \n[MCU] Stopping...")
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
        print(f"{get_timestamp()} [MCU] Cleanup completed")

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 mcu_receive.py <terminal_ip>")
        print("Example: python3 mcu_receive.py 10.35.146.109")
        sys.exit(1)
    
    terminal_ip = sys.argv[1]
    
    receiver = MCUReceiver(terminal_ip)
    receiver.start()

if __name__ == "__main__":
    main() 
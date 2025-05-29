#!/usr/bin/env python3
"""
RTP包接收器
用于验证turbo_relay转发的包是否正确
"""

import socket
import struct
import time
import threading
import sys

class RTPReceiver:
    def __init__(self, listen_port=5004, timeout=30):
        self.listen_port = listen_port
        self.timeout = timeout
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.settimeout(1.0)  # 1秒超时
        self.running = False
        self.received_packets = []
        
    def parse_rtp_packet(self, data):
        """解析RTP包"""
        if len(data) < 12:
            return None
            
        # 解析RTP头部
        version_flags, marker_pt, seq_num, timestamp, ssrc = struct.unpack('!BBHII', data[:12])
        
        version = (version_flags >> 6) & 0x3
        padding = (version_flags >> 5) & 0x1
        extension = (version_flags >> 4) & 0x1
        cc = version_flags & 0xF
        marker = (marker_pt >> 7) & 0x1
        payload_type = marker_pt & 0x7F
        
        return {
            'version': version,
            'padding': padding,
            'extension': extension,
            'cc': cc,
            'marker': marker,
            'payload_type': payload_type,
            'seq_num': seq_num,
            'timestamp': timestamp,
            'ssrc': ssrc,
            'payload_size': len(data) - 12
        }
    
    def start_listening(self):
        """开始监听RTP包"""
        try:
            self.sock.bind(('0.0.0.0', self.listen_port))
            print(f"开始监听端口 {self.listen_port}...")
            self.running = True
            
            start_time = time.time()
            while self.running and (time.time() - start_time) < self.timeout:
                try:
                    data, addr = self.sock.recvfrom(2048)
                    receive_time = time.time()
                    
                    rtp_info = self.parse_rtp_packet(data)
                    if rtp_info:
                        packet_info = {
                            'receive_time': receive_time,
                            'source_addr': addr,
                            'rtp': rtp_info
                        }
                        self.received_packets.append(packet_info)
                        
                        print(f"收到RTP包: 来源={addr[0]}:{addr[1]}, "
                              f"SSRC=0x{rtp_info['ssrc']:08x}, "
                              f"序列号={rtp_info['seq_num']}, "
                              f"时间戳={rtp_info['timestamp']}, "
                              f"载荷大小={rtp_info['payload_size']}")
                    else:
                        print(f"收到非RTP包: 来源={addr[0]}:{addr[1]}, 大小={len(data)}")
                        
                except socket.timeout:
                    continue
                except Exception as e:
                    print(f"接收错误: {e}")
                    
        except Exception as e:
            print(f"监听错误: {e}")
        finally:
            self.sock.close()
            
    def stop_listening(self):
        """停止监听"""
        self.running = False
        
    def get_statistics(self):
        """获取统计信息"""
        if not self.received_packets:
            return "未收到任何RTP包"
            
        # 按SSRC分组
        ssrc_groups = {}
        for packet in self.received_packets:
            ssrc = packet['rtp']['ssrc']
            if ssrc not in ssrc_groups:
                ssrc_groups[ssrc] = []
            ssrc_groups[ssrc].append(packet)
            
        stats = []
        stats.append(f"总共收到 {len(self.received_packets)} 个RTP包")
        stats.append(f"涉及 {len(ssrc_groups)} 个不同的SSRC")
        
        for ssrc, packets in ssrc_groups.items():
            stats.append(f"\nSSRC 0x{ssrc:08x}:")
            stats.append(f"  包数量: {len(packets)}")
            
            # 序列号分析
            seq_nums = [p['rtp']['seq_num'] for p in packets]
            seq_nums.sort()
            stats.append(f"  序列号范围: {min(seq_nums)} - {max(seq_nums)}")
            
            # 检查连续性
            missing = []
            for i in range(min(seq_nums), max(seq_nums) + 1):
                if i not in seq_nums:
                    missing.append(i)
            
            if missing:
                stats.append(f"  缺失序列号: {missing}")
            else:
                stats.append(f"  序列号连续")
                
            # 检查顺序
            received_order = [p['rtp']['seq_num'] for p in packets]
            is_ordered = received_order == sorted(received_order)
            stats.append(f"  接收顺序: {'有序' if is_ordered else '乱序'}")
            
            if not is_ordered:
                stats.append(f"  接收顺序: {received_order}")
                stats.append(f"  正确顺序: {sorted(received_order)}")
                
        return '\n'.join(stats)

def main():
    if len(sys.argv) > 1:
        port = int(sys.argv[1])
    else:
        port = 5004
        
    if len(sys.argv) > 2:
        timeout = int(sys.argv[2])
    else:
        timeout = 30
        
    print(f"RTP包接收器")
    print(f"监听端口: {port}")
    print(f"超时时间: {timeout}秒")
    print("按Ctrl+C停止监听")
    print()
    
    receiver = RTPReceiver(port, timeout)
    
    try:
        receiver.start_listening()
    except KeyboardInterrupt:
        print("\n停止监听...")
        receiver.stop_listening()
    
    print("\n=== 统计信息 ===")
    print(receiver.get_statistics())

if __name__ == '__main__':
    main() 
#!/usr/bin/env python3
"""
在109(近端)接收RTP包
检查从测试机7发送的乱序包是否被加速器重排序
"""

import socket
import struct
import time
from typing import List

class RTPReceiver:
    def __init__(self, port: int):
        self.port = port
        self.received_packets = []
        
    def start_receiving(self, timeout: int = 30):
        """开始接收RTP包"""
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.bind(('0.0.0.0', self.port))
        sock.settimeout(1.0)
        
        print(f"在109上监听端口 {self.port}，等待RTP包...")
        print("=" * 50)
        
        start_time = time.time()
        
        while time.time() - start_time < timeout:
            try:
                data, addr = sock.recvfrom(1500)
                if len(data) >= 12:  # RTP最小长度
                    # 解析RTP头部
                    header = struct.unpack('!BBHII', data[:12])
                    version = (header[0] >> 6) & 0x3
                    payload_type = header[1] & 0x7F
                    seq = header[2]
                    timestamp = header[3]
                    ssrc = header[4]
                    
                    # 检查是否为RTP包
                    if version == 2:
                        packet_info = {
                            'seq': seq,
                            'timestamp': timestamp,
                            'ssrc': ssrc,
                            'recv_time': time.time(),
                            'size': len(data),
                            'from_addr': addr
                        }
                        
                        self.received_packets.append(packet_info)
                        print(f"收到RTP包: SSRC={ssrc}, Seq={seq}, 来自={addr}, 大小={len(data)}")
                    else:
                        print(f"收到非RTP包: 来自={addr}, 大小={len(data)}")
                        
            except socket.timeout:
                continue
            except Exception as e:
                print(f"接收错误: {e}")
        
        sock.close()
        print(f"\n接收完成，共收到 {len(self.received_packets)} 个RTP包")
        
    def analyze_results(self):
        """分析接收结果"""
        if not self.received_packets:
            print("没有收到任何RTP包！")
            return
        
        print("\n=== 接收结果分析 ===")
        
        # 按SSRC分组
        ssrc_groups = {}
        for pkt in self.received_packets:
            ssrc = pkt['ssrc']
            if ssrc not in ssrc_groups:
                ssrc_groups[ssrc] = []
            ssrc_groups[ssrc].append(pkt)
        
        for ssrc, packets in ssrc_groups.items():
            print(f"\nSSRC {ssrc} 的包:")
            
            # 按接收时间排序
            packets.sort(key=lambda x: x['recv_time'])
            
            sequences = []
            print("接收顺序:")
            for i, pkt in enumerate(packets):
                print(f"  {i+1}: Seq={pkt['seq']}, 时间={pkt['recv_time']:.3f}")
                sequences.append(pkt['seq'])
            
            # 检查是否有序
            is_ordered = self._check_sequence_order(sequences)
            
            if is_ordered:
                print("✅ 包已正确排序！加速器工作正常")
            else:
                print("❌ 包仍然乱序，加速器可能未启用排序功能")
                
            # 显示期望顺序
            if ssrc == 12345678:  # 基本乱序测试
                expected = [100, 101, 102, 103, 104, 105, 106, 107, 108]
                print(f"期望顺序: {expected}")
            elif ssrc == 87654321:  # 回绕测试
                expected = [65534, 65535, 0, 1, 2]
                print(f"期望顺序: {expected}")
    
    def _check_sequence_order(self, sequences: List[int]) -> bool:
        """检查序列号是否有序（考虑回绕）"""
        if len(sequences) <= 1:
            return True
        
        for i in range(1, len(sequences)):
            prev_seq = sequences[i-1]
            curr_seq = sequences[i]
            
            # 计算序列号差值（考虑回绕）
            diff = (curr_seq - prev_seq) % 65536
            if diff > 32768:
                diff -= 65536
            
            # 序列号应该递增
            if diff <= 0:
                print(f"发现乱序: {prev_seq} -> {curr_seq} (diff={diff})")
                return False
            elif diff > 100:  # 跳跃太大也认为有问题
                print(f"序列号跳跃过大: {prev_seq} -> {curr_seq} (diff={diff})")
                return False
        
        return True

def main():
    print("RTP包接收器 - 在109(近端)运行")
    print("检查从测试机7发送的乱序包是否被加速器重排序")
    print("=" * 60)
    
    receiver = RTPReceiver(5004)
    
    try:
        # 开始接收，等待30秒
        receiver.start_receiving(timeout=30)
        
        # 分析结果
        receiver.analyze_results()
        
    except KeyboardInterrupt:
        print("\n接收被中断")
    except Exception as e:
        print(f"错误: {e}")

if __name__ == "__main__":
    main() 
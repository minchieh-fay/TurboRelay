#!/usr/bin/env python3
"""
RTP乱序包测试脚本
测试TurboRelay的乱序包重排序功能
"""

import socket
import struct
import time
import threading
import random
from typing import List, Tuple

class RTPPacket:
    def __init__(self, ssrc: int, seq: int, timestamp: int, payload: bytes = b''):
        self.ssrc = ssrc
        self.seq = seq
        self.timestamp = timestamp
        self.payload = payload or b'A' * 100  # 默认100字节载荷
    
    def to_bytes(self) -> bytes:
        """构造RTP包"""
        # RTP头部 (12字节)
        header = struct.pack('!BBHII',
            0x80,  # V=2, P=0, X=0, CC=0
            96,    # M=0, PT=96 (动态载荷类型)
            self.seq,
            self.timestamp,
            self.ssrc
        )
        return header + self.payload

class RTPTester:
    def __init__(self, target_ip: str, target_port: int, listen_port: int):
        self.target_ip = target_ip
        self.target_port = target_port
        self.listen_port = listen_port
        self.received_packets = []
        self.received_lock = threading.Lock()
        self.running = False
        
    def start_receiver(self):
        """启动接收线程"""
        self.running = True
        receiver_thread = threading.Thread(target=self._receiver_loop)
        receiver_thread.daemon = True
        receiver_thread.start()
        return receiver_thread
    
    def _receiver_loop(self):
        """接收循环"""
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.bind(('0.0.0.0', self.listen_port))
        sock.settimeout(1.0)
        
        print(f"Listening on port {self.listen_port}...")
        
        while self.running:
            try:
                data, addr = sock.recvfrom(1500)
                if len(data) >= 12:  # RTP最小长度
                    # 解析RTP头部
                    header = struct.unpack('!BBHII', data[:12])
                    seq = header[2]
                    timestamp = header[3]
                    ssrc = header[4]
                    
                    with self.received_lock:
                        self.received_packets.append({
                            'seq': seq,
                            'timestamp': timestamp,
                            'ssrc': ssrc,
                            'recv_time': time.time(),
                            'size': len(data)
                        })
                    
                    print(f"Received: SSRC={ssrc}, Seq={seq}, TS={timestamp}, Size={len(data)}")
                    
            except socket.timeout:
                continue
            except Exception as e:
                print(f"Receiver error: {e}")
        
        sock.close()
    
    def send_ordered_packets(self, ssrc: int, start_seq: int, count: int, interval: float = 0.01):
        """发送有序的RTP包"""
        print(f"\n=== 发送有序包测试 ===")
        print(f"SSRC: {ssrc}, 起始序列号: {start_seq}, 包数: {count}")
        
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 使用合适的RTP时间戳（32位范围内）
        base_timestamp = int(time.time()) % (2**31)  # 确保在32位范围内
        
        for i in range(count):
            seq = (start_seq + i) % 65536
            timestamp = (base_timestamp + i * 3600) % (2**32)  # 40ms间隔，保持32位
            packet = RTPPacket(ssrc, seq, timestamp)
            
            sock.sendto(packet.to_bytes(), (self.target_ip, self.target_port))
            print(f"Sent: Seq={seq}")
            time.sleep(interval)
        
        sock.close()
        print("有序包发送完成\n")
    
    def send_reordered_packets(self, ssrc: int, start_seq: int, count: int, interval: float = 0.01):
        """发送乱序的RTP包"""
        print(f"\n=== 发送乱序包测试 ===")
        print(f"SSRC: {ssrc}, 起始序列号: {start_seq}, 包数: {count}")
        
        # 生成序列号列表
        sequences = [(start_seq + i) % 65536 for i in range(count)]
        
        # 使用合适的RTP时间戳（32位范围内）
        base_timestamp = int(time.time()) % (2**31)  # 确保在32位范围内
        
        # 创建乱序模式：部分包延迟发送
        reorder_pattern = []
        for i, seq in enumerate(sequences):
            timestamp = (base_timestamp + i * 3600) % (2**32)  # 保持32位
            reorder_pattern.append((seq, timestamp, i))
        
        # 乱序策略：每3个包中，第2个包延迟发送
        delayed_packets = []
        immediate_packets = []
        
        for i, (seq, ts, orig_idx) in enumerate(reorder_pattern):
            if i % 3 == 1:  # 每3个包中的第2个包延迟
                delayed_packets.append((seq, ts, orig_idx))
            else:
                immediate_packets.append((seq, ts, orig_idx))
        
        print("发送顺序:")
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        # 先发送非延迟的包
        for seq, ts, orig_idx in immediate_packets:
            packet = RTPPacket(ssrc, seq, ts)
            sock.sendto(packet.to_bytes(), (self.target_ip, self.target_port))
            print(f"Sent immediate: Seq={seq} (原序号{orig_idx})")
            time.sleep(interval)
        
        # 稍等一下再发送延迟的包
        time.sleep(0.05)
        print("发送延迟包:")
        
        for seq, ts, orig_idx in delayed_packets:
            packet = RTPPacket(ssrc, seq, ts)
            sock.sendto(packet.to_bytes(), (self.target_ip, self.target_port))
            print(f"Sent delayed: Seq={seq} (原序号{orig_idx})")
            time.sleep(interval)
        
        sock.close()
        print("乱序包发送完成\n")
    
    def send_wrap_around_packets(self, ssrc: int, interval: float = 0.01):
        """测试序列号回绕的乱序包"""
        print(f"\n=== 序列号回绕乱序测试 ===")
        print(f"SSRC: {ssrc}")
        
        # 测试65534, 65535, 0, 1, 2的乱序
        sequences = [65535, 1, 65534, 2, 0]  # 乱序发送
        expected_order = [65534, 65535, 0, 1, 2]  # 期望接收顺序
        
        print(f"发送顺序: {sequences}")
        print(f"期望接收顺序: {expected_order}")
        
        # 使用合适的RTP时间戳（32位范围内）
        base_timestamp = int(time.time()) % (2**31)  # 确保在32位范围内
        
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        for i, seq in enumerate(sequences):
            timestamp = (base_timestamp + i * 3600) % (2**32)  # 保持32位
            packet = RTPPacket(ssrc, seq, timestamp)
            sock.sendto(packet.to_bytes(), (self.target_ip, self.target_port))
            print(f"Sent: Seq={seq}")
            time.sleep(interval)
        
        sock.close()
        print("回绕乱序包发送完成\n")
    
    def analyze_results(self, expected_ssrc: int, test_name: str):
        """分析接收结果"""
        print(f"\n=== {test_name} 结果分析 ===")
        
        with self.received_lock:
            # 过滤指定SSRC的包
            test_packets = [p for p in self.received_packets if p['ssrc'] == expected_ssrc]
            
        if not test_packets:
            print("没有接收到任何包！")
            return False
        
        # 按接收时间排序
        test_packets.sort(key=lambda x: x['recv_time'])
        
        print(f"接收到 {len(test_packets)} 个包:")
        sequences = []
        for i, pkt in enumerate(test_packets):
            print(f"  {i+1}: Seq={pkt['seq']}, 接收时间={pkt['recv_time']:.3f}")
            sequences.append(pkt['seq'])
        
        # 检查是否按序列号顺序接收
        is_ordered = self._check_sequence_order(sequences)
        
        if is_ordered:
            print("✅ 包已正确重排序！")
        else:
            print("❌ 包未正确重排序")
        
        return is_ordered
    
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
            
            # 序列号应该递增（允许小的跳跃）
            if diff <= 0 or diff > 100:
                print(f"序列号乱序: {prev_seq} -> {curr_seq} (diff={diff})")
                return False
        
        return True
    
    def clear_received(self):
        """清空接收缓存"""
        with self.received_lock:
            self.received_packets.clear()
    
    def stop(self):
        """停止测试"""
        self.running = False

def main():
    # 配置 - 使用正确的RTP/RTCP端口分配
    TARGET_IP = "10.35.146.109"  # 加速器IP
    RTP_PORT = 5004              # RTP端口（偶数）
    RTCP_PORT = 5005             # RTCP端口（RTP端口+1）
    LISTEN_RTP_PORT = 6004       # 监听RTP端口（避免冲突）
    LISTEN_RTCP_PORT = 6005      # 监听RTCP端口
    
    print("RTP乱序包重排序测试")
    print(f"发送RTP到: {TARGET_IP}:{RTP_PORT}")
    print(f"发送RTCP到: {TARGET_IP}:{RTCP_PORT}")
    print(f"监听RTP: 0.0.0.0:{LISTEN_RTP_PORT}")
    print(f"监听RTCP: 0.0.0.0:{LISTEN_RTCP_PORT}")
    print("=" * 50)
    
    tester = RTPTester(TARGET_IP, RTP_PORT, LISTEN_RTP_PORT)
    
    # 启动接收器
    receiver_thread = tester.start_receiver()
    time.sleep(1)  # 等待接收器启动
    
    try:
        # 测试1: 基本乱序测试
        ssrc1 = 12345678  # 使用简单的整数值
        tester.clear_received()
        tester.send_reordered_packets(ssrc1, 100, 9, 0.02)
        time.sleep(2)  # 等待所有包处理完成
        tester.analyze_results(ssrc1, "基本乱序测试")
        
        time.sleep(3)
        
        # 测试2: 序列号回绕测试
        ssrc2 = 87654321  # 使用简单的整数值
        tester.clear_received()
        tester.send_wrap_around_packets(ssrc2, 0.02)
        time.sleep(2)
        tester.analyze_results(ssrc2, "序列号回绕测试")
        
        time.sleep(3)
        
        # 测试3: 有序包对比测试
        ssrc3 = 11223344  # 使用简单的整数值
        tester.clear_received()
        tester.send_ordered_packets(ssrc3, 200, 6, 0.02)
        time.sleep(2)
        tester.analyze_results(ssrc3, "有序包对比测试")
        
    except KeyboardInterrupt:
        print("\n测试被中断")
    finally:
        tester.stop()
        print("\n测试完成")

if __name__ == "__main__":
    main() 
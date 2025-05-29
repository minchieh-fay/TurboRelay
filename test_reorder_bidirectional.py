#!/usr/bin/env python3
"""
双向RTP乱序包测试脚本
模拟真实的近端(if1)和远端(if2)通信场景
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

class BidirectionalRTPTester:
    def __init__(self):
        # 加速器配置
        self.accelerator_ip = "10.35.146.109"
        
        # 端口配置 - 根据实际的turbo_relay配置
        # 假设加速器的if1连接到5004端口，if2连接到5006端口
        self.near_end_port = 5004    # 近端端口 (if1)
        self.far_end_port = 5006     # 远端端口 (if2)
        
        # 监听端口
        self.listen_near_port = 6004  # 监听近端返回的包
        self.listen_far_port = 6006   # 监听远端返回的包
        
        self.received_packets = []
        self.received_lock = threading.Lock()
        self.running = False
        
    def start_receivers(self):
        """启动双向接收器"""
        self.running = True
        
        # 启动近端接收器
        near_thread = threading.Thread(target=self._receiver_loop, 
                                     args=(self.listen_near_port, "近端"))
        near_thread.daemon = True
        near_thread.start()
        
        # 启动远端接收器
        far_thread = threading.Thread(target=self._receiver_loop, 
                                    args=(self.listen_far_port, "远端"))
        far_thread.daemon = True
        far_thread.start()
        
        return near_thread, far_thread
    
    def _receiver_loop(self, port, name):
        """接收循环"""
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.bind(('0.0.0.0', port))
        sock.settimeout(1.0)
        
        print(f"{name}接收器启动，监听端口 {port}")
        
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
                            'size': len(data),
                            'interface': name,
                            'from_addr': addr
                        })
                    
                    print(f"{name}收到: SSRC={ssrc}, Seq={seq}, 来自={addr}")
                    
            except socket.timeout:
                continue
            except Exception as e:
                print(f"{name}接收器错误: {e}")
        
        sock.close()
    
    def send_near_to_far_ordered(self, ssrc: int, start_seq: int, count: int):
        """近端到远端：发送有序包（模拟终端设备）"""
        print(f"\n=== 近端到远端有序包测试 ===")
        print(f"发送到加速器近端接口: {self.accelerator_ip}:{self.near_end_port}")
        print(f"SSRC: {ssrc}, 起始序列号: {start_seq}, 包数: {count}")
        
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        base_timestamp = int(time.time()) % (2**31)
        
        for i in range(count):
            seq = (start_seq + i) % 65536
            timestamp = (base_timestamp + i * 3600) % (2**32)
            packet = RTPPacket(ssrc, seq, timestamp)
            
            sock.sendto(packet.to_bytes(), (self.accelerator_ip, self.near_end_port))
            print(f"近端发送: Seq={seq}")
            time.sleep(0.02)
        
        sock.close()
        print("近端有序包发送完成\n")
    
    def send_far_to_near_reordered(self, ssrc: int, start_seq: int, count: int):
        """远端到近端：发送乱序包（模拟MCU）"""
        print(f"\n=== 远端到近端乱序包测试 ===")
        print(f"发送到加速器远端接口: {self.accelerator_ip}:{self.far_end_port}")
        print(f"SSRC: {ssrc}, 起始序列号: {start_seq}, 包数: {count}")
        
        # 生成乱序序列
        sequences = [(start_seq + i) % 65536 for i in range(count)]
        base_timestamp = int(time.time()) % (2**31)
        
        # 创建乱序模式：每3个包中，第2个包延迟发送
        reorder_pattern = []
        for i, seq in enumerate(sequences):
            timestamp = (base_timestamp + i * 3600) % (2**32)
            reorder_pattern.append((seq, timestamp, i))
        
        delayed_packets = []
        immediate_packets = []
        
        for i, (seq, ts, orig_idx) in enumerate(reorder_pattern):
            if i % 3 == 1:  # 每3个包中的第2个包延迟
                delayed_packets.append((seq, ts, orig_idx))
            else:
                immediate_packets.append((seq, ts, orig_idx))
        
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        print("发送立即包:")
        for seq, ts, orig_idx in immediate_packets:
            packet = RTPPacket(ssrc, seq, ts)
            sock.sendto(packet.to_bytes(), (self.accelerator_ip, self.far_end_port))
            print(f"远端发送立即: Seq={seq} (原序号{orig_idx})")
            time.sleep(0.02)
        
        # 延迟发送
        time.sleep(0.1)
        print("发送延迟包:")
        for seq, ts, orig_idx in delayed_packets:
            packet = RTPPacket(ssrc, seq, ts)
            sock.sendto(packet.to_bytes(), (self.accelerator_ip, self.far_end_port))
            print(f"远端发送延迟: Seq={seq} (原序号{orig_idx})")
            time.sleep(0.02)
        
        sock.close()
        print("远端乱序包发送完成\n")
    
    def send_far_wrap_around_reordered(self, ssrc: int):
        """远端到近端：序列号回绕乱序测试"""
        print(f"\n=== 远端序列号回绕乱序测试 ===")
        print(f"发送到加速器远端接口: {self.accelerator_ip}:{self.far_end_port}")
        print(f"SSRC: {ssrc}")
        
        # 测试65534, 65535, 0, 1, 2的乱序
        sequences = [65535, 1, 65534, 2, 0]  # 乱序发送
        expected_order = [65534, 65535, 0, 1, 2]  # 期望接收顺序
        
        print(f"发送顺序: {sequences}")
        print(f"期望接收顺序: {expected_order}")
        
        base_timestamp = int(time.time()) % (2**31)
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        
        for i, seq in enumerate(sequences):
            timestamp = (base_timestamp + i * 3600) % (2**32)
            packet = RTPPacket(ssrc, seq, timestamp)
            sock.sendto(packet.to_bytes(), (self.accelerator_ip, self.far_end_port))
            print(f"远端发送: Seq={seq}")
            time.sleep(0.02)
        
        sock.close()
        print("远端回绕乱序包发送完成\n")
    
    def analyze_results(self, expected_ssrc: int, test_name: str, expected_interface: str):
        """分析接收结果"""
        print(f"\n=== {test_name} 结果分析 ===")
        
        with self.received_lock:
            # 过滤指定SSRC和接口的包
            test_packets = [p for p in self.received_packets 
                          if p['ssrc'] == expected_ssrc and p['interface'] == expected_interface]
            
        if not test_packets:
            print(f"在{expected_interface}接口没有接收到SSRC={expected_ssrc}的包！")
            return False
        
        # 按接收时间排序
        test_packets.sort(key=lambda x: x['recv_time'])
        
        print(f"在{expected_interface}接口接收到 {len(test_packets)} 个包:")
        sequences = []
        for i, pkt in enumerate(test_packets):
            print(f"  {i+1}: Seq={pkt['seq']}, 来自={pkt['from_addr']}")
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
    print("双向RTP乱序包重排序测试")
    print("=" * 50)
    
    tester = BidirectionalRTPTester()
    
    # 启动双向接收器
    near_thread, far_thread = tester.start_receivers()
    time.sleep(2)  # 等待接收器启动
    
    try:
        # 测试1: 近端到远端有序包（应该直接透传）
        print("测试1: 近端到远端有序包传输")
        ssrc1 = 11111111
        tester.clear_received()
        tester.send_near_to_far_ordered(ssrc1, 100, 6)
        time.sleep(3)
        tester.analyze_results(ssrc1, "近端到远端有序包", "远端")
        
        time.sleep(2)
        
        # 测试2: 远端到近端乱序包（应该重排序）
        print("测试2: 远端到近端乱序包重排序")
        ssrc2 = 22222222
        tester.clear_received()
        tester.send_far_to_near_reordered(ssrc2, 200, 9)
        time.sleep(3)
        tester.analyze_results(ssrc2, "远端到近端乱序包", "近端")
        
        time.sleep(2)
        
        # 测试3: 远端到近端序列号回绕乱序
        print("测试3: 远端到近端序列号回绕乱序")
        ssrc3 = 33333333
        tester.clear_received()
        tester.send_far_wrap_around_reordered(ssrc3)
        time.sleep(3)
        tester.analyze_results(ssrc3, "远端序列号回绕乱序", "近端")
        
    except KeyboardInterrupt:
        print("\n测试被中断")
    finally:
        tester.stop()
        print("\n测试完成")

if __name__ == "__main__":
    main() 
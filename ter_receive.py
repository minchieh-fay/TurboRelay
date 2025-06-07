#!/usr/bin/env python3
"""
RTP接收端测试脚本
功能：
1. 接收RTP包并维护排序队列
2. 检测缺失包但默认不发送NACK
3. 前面10个连续包就删除
4. 打印收到的seq和缺失的seq
"""

import socket
import struct
import time
import threading
import sys
from datetime import datetime
from collections import defaultdict

def get_timestamp():
    """获取精确到毫秒的时间戳"""
    return datetime.now().strftime("%H:%M:%S.%f")[:-3]

class RTPReceiver:
    def __init__(self, sender_ip, port=5533, enable_nack=False):
        self.sender_ip = sender_ip
        self.port = port
        self.enable_nack = enable_nack
        
        # 接收队列 - 按SSRC分组
        self.receive_queues = defaultdict(list)  # {ssrc: [seq1, seq2, ...]}
        self.processed_max_seq = defaultdict(int)  # {ssrc: max_processed_seq} 记录已处理的最大连续序列号
        self.queue_lock = threading.Lock()
        
        # 统计
        self.received_count = 0
        self.nack_sent_count = 0
        self.processed_count = 0
        
        # 创建socket
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.socket.bind(('0.0.0.0', port))
        
        self.running = False
        
    def create_nack_packet(self, ssrc, missing_seq):
        """创建RTCP NACK包"""
        # RTCP NACK包格式
        version = 2
        padding = 0
        fmt = 1  # Generic NACK
        packet_type = 205  # RTPFB
        length = 3  # 长度（32位字为单位）
        
        # RTCP头部
        byte1 = (version << 6) | (padding << 5) | fmt
        
        nack_packet = struct.pack('!BBHIIHH',
                                 byte1, packet_type, length,
                                 ssrc,  # Sender SSRC (我们的SSRC)
                                 ssrc,  # Media SSRC (对方的SSRC)
                                 missing_seq,  # PID
                                 0)     # BLP
        
        return nack_packet
    
    def send_nack(self, ssrc, missing_seq):
        """发送NACK请求"""
        if not self.enable_nack:
            return
            
        try:
            nack_packet = self.create_nack_packet(ssrc, missing_seq)
            self.socket.sendto(nack_packet, (self.sender_ip, self.port))
            self.nack_sent_count += 1
            print(f"{get_timestamp()} [Receiver] Sent NACK for seq={missing_seq}")
        except Exception as e:
            print(f"{get_timestamp()} [Receiver] NACK send error: {e}")
    
    def find_missing_sequences(self, queue):
        """找到队列中缺失的序列号"""
        if len(queue) < 2:
            return []
        
        # 排序队列
        sorted_queue = sorted(queue)
        missing = []
        
        # 检查连续性
        for i in range(len(sorted_queue) - 1):
            current = sorted_queue[i]
            next_seq = sorted_queue[i + 1]
            
            # 计算期望的下一个序列号（考虑回绕）
            expected_next = (current + 1) % 65536
            
            # 如果下一个序列号不是期望的，说明中间有缺失
            while expected_next != next_seq:
                missing.append(expected_next)
                expected_next = (expected_next + 1) % 65536
                
                # 防止无限循环（如果跨度太大，可能是回绕导致的误判）
                if len(missing) > 100:
                    break
        
        return missing
    
    def remove_consecutive_from_front(self, queue, ssrc):
        """从队列前面删除连续的10个包"""
        if len(queue) < 10:
            return 0
        
        # 排序队列
        sorted_queue = sorted(queue)
        
        # 找到从最小值开始的连续序列
        consecutive_count = 1
        start_seq = sorted_queue[0]
        
        for i in range(1, len(sorted_queue)):
            current_seq = sorted_queue[i]
            expected_seq = (start_seq + i) % 65536
            
            if current_seq == expected_seq:
                consecutive_count += 1
            else:
                break
        
        # 如果前面有10个或更多连续包，删除前10个
        if consecutive_count >= 10:
            removed_seqs = []
            for i in range(10):
                seq_to_remove = (start_seq + i) % 65536
                if seq_to_remove in queue:
                    queue.remove(seq_to_remove)
                    removed_seqs.append(seq_to_remove)
            
            # 更新已处理的最大序列号
            if removed_seqs:
                # 最大的序列号就是start_seq + 9（考虑回绕）
                max_removed = (start_seq + 9) % 65536
                self.processed_max_seq[ssrc] = max_removed
                print(f"{get_timestamp()} [Receiver] Removed 10 consecutive packets: {removed_seqs[0]}-{removed_seqs[-1]}")
                print(f"{get_timestamp()} [Receiver] Updated processed_max_seq[0x{ssrc:08X}] = {self.processed_max_seq[ssrc]}")
            
            return 10
        
        return 0
    
    def process_rtp_packet(self, data, addr):
        """处理RTP包"""
        if len(data) < 12:
            return
        
        try:
            # 解析RTP头部
            header = struct.unpack('!BBHII', data[:12])
            seq = header[2]
            ssrc = header[4]
            
            # 检查是否是重传包（payload更大）
            is_retransmit = len(data) > 50  # 简单判断
            retransmit_flag = " (RETRANSMIT)" if is_retransmit else ""
            
            with self.queue_lock:
                # 检查是否是已经处理过的序列号
                processed_max = self.processed_max_seq[ssrc]
                if self.is_seq_before_or_equal(seq, processed_max):
                    print(f"{get_timestamp()} [Receiver] Ignoring old seq={seq} (already processed up to {processed_max}){retransmit_flag}")
                    return
                
                # 添加到对应SSRC的队列
                queue = self.receive_queues[ssrc]
                
                # 检查是否已存在（重复包）
                if seq in queue:
                    print(f"{get_timestamp()} [Receiver] Duplicate seq={seq}{retransmit_flag}")
                    return
                
                self.received_count += 1
                queue.append(seq)
                
                # 找到缺失的序列号（只在processed_max之后的范围内查找）
                missing = self.find_missing_sequences_after(queue, processed_max)
                
                # 打印接收信息
                missing_str = f"[{','.join(map(str, missing))}]" if missing else "[]"
                nack_status = " (NACK disabled)" if missing and not self.enable_nack else ""
                print(f"{get_timestamp()} [Receiver] {seq}----- {missing_str}{retransmit_flag}{nack_status}")
                
                # 只有启用NACK时才发送
                if self.enable_nack:
                    for missing_seq in missing:
                        self.send_nack(ssrc, missing_seq)
                
                # 尝试删除前面的连续包
                removed = self.remove_consecutive_from_front(queue, ssrc)
                if removed > 0:
                    self.processed_count += removed
                
                # 每100个包打印统计
                if self.received_count % 100 == 0:
                    print(f"{get_timestamp()} [Receiver] Stats: received={self.received_count}, "
                          f"processed={self.processed_count}, nacks={self.nack_sent_count}, "
                          f"queue_size={len(queue)}")
                    
        except Exception as e:
            print(f"{get_timestamp()} [Receiver] RTP parse error: {e}")
    
    def find_missing_sequences_after(self, queue, processed_max):
        """找到队列中缺失的序列号（只在processed_max之后）"""
        if len(queue) < 2:
            return []
        
        # 过滤掉已处理过的序列号
        valid_queue = []
        for seq in queue:
            if not self.is_seq_before_or_equal(seq, processed_max):
                valid_queue.append(seq)
        
        if len(valid_queue) < 2:
            return []
        
        # 排序有效队列
        sorted_queue = sorted(valid_queue)
        missing = []
        
        # 检查连续性
        for i in range(len(sorted_queue) - 1):
            current = sorted_queue[i]
            next_seq = sorted_queue[i + 1]
            
            # 计算期望的下一个序列号（考虑回绕）
            expected_next = (current + 1) % 65536
            
            # 如果下一个序列号不是期望的，说明中间有缺失
            while expected_next != next_seq:
                # 确保缺失的序列号在processed_max之后
                if not self.is_seq_before_or_equal(expected_next, processed_max):
                    missing.append(expected_next)
                expected_next = (expected_next + 1) % 65536
                
                # 防止无限循环
                if len(missing) > 100:
                    break
        
        return missing
    
    def is_seq_before_or_equal(self, seq, max_seq):
        """判断序列号seq是否在max_seq之前或等于（考虑回绕）"""
        if max_seq == 0:  # 还没有处理过任何包
            return False
            
        # 使用序列号差值来判断，考虑16位回绕
        # 如果差值在合理范围内（< 32768），说明seq在max_seq之后
        # 如果差值很大（>= 32768），说明发生了回绕，seq实际在max_seq之前
        
        diff = (seq - max_seq) % 65536
        
        # 如果差值 <= 0，说明seq <= max_seq（在同一轮中）
        # 如果差值 > 32768，说明seq在上一轮，也应该被忽略
        if diff == 0:
            return True  # seq == max_seq
        elif diff > 32768:
            return True  # seq在上一轮，已经处理过
        else:
            return False  # seq > max_seq，是新的包
    
    def receive_packets(self):
        """接收数据包"""
        print(f"{get_timestamp()} [Receiver] Starting packet receiver...")
        self.socket.settimeout(1.0)
        
        while self.running:
            try:
                data, addr = self.socket.recvfrom(1500)
                
                # 简单判断是RTP还是RTCP
                if len(data) >= 12:
                    # 检查版本和包类型
                    version = (data[0] >> 6) & 0x03
                    packet_type = data[1]
                    
                    if version == 2:
                        if packet_type < 128:
                            # RTP包
                            self.process_rtp_packet(data, addr)
                        else:
                            # RTCP包，忽略（我们只关心RTP）
                            pass
                    
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"{get_timestamp()} [Receiver] Receive error: {e}")
                break
        
        print(f"{get_timestamp()} [Receiver] Packet receiver stopped")
    
    def print_queue_status(self):
        """定期打印队列状态"""
        while self.running:
            time.sleep(5)  # 每5秒打印一次
            
            with self.queue_lock:
                for ssrc, queue in self.receive_queues.items():
                    if queue:
                        sorted_queue = sorted(queue)
                        missing = self.find_missing_sequences(queue)
                        print(f"{get_timestamp()} [Receiver] SSRC 0x{ssrc:08X}: "
                              f"queue_size={len(queue)}, "
                              f"range={sorted_queue[0]}-{sorted_queue[-1]}, "
                              f"missing={len(missing)}")
    
    def start(self):
        """启动接收端"""
        print(f"{get_timestamp()} === RTP Receiver ===")
        print(f"{get_timestamp()} Listening on: 0.0.0.0:{self.port}")
        print(f"{get_timestamp()} Sender: {self.sender_ip}:{self.port}")
        print(f"{get_timestamp()} NACK enabled: {self.enable_nack}")
        print(f"{get_timestamp()} Features: Auto-sort queue, detect missing packets")
        print(f"{get_timestamp()} Press Ctrl+C to stop...")
        
        self.running = True
        
        try:
            # 启动线程
            receiver_thread = threading.Thread(target=self.receive_packets)
            status_thread = threading.Thread(target=self.print_queue_status)
            
            receiver_thread.start()
            status_thread.start()
            
            # 等待中断
            while self.running:
                time.sleep(1)
                
        except KeyboardInterrupt:
            print(f"{get_timestamp()} \n[Receiver] Stopping...")
            self.running = False
        
        finally:
            self.cleanup()
    
    def cleanup(self):
        """清理资源"""
        self.running = False
        
        # 打印最终统计
        with self.queue_lock:
            for ssrc, queue in self.receive_queues.items():
                if queue:
                    sorted_queue = sorted(queue)
                    missing = self.find_missing_sequences(queue)
                    print(f"{get_timestamp()} [Receiver] Final SSRC 0x{ssrc:08X}: "
                          f"queue_size={len(queue)}, "
                          f"range={sorted_queue[0]}-{sorted_queue[-1]}, "
                          f"missing_count={len(missing)}")
                    if missing:
                        print(f"{get_timestamp()} [Receiver] Missing seqs: {missing[:20]}...")  # 只显示前20个
        
        print(f"{get_timestamp()} [Receiver] Final stats: received={self.received_count}, "
              f"processed={self.processed_count}, nacks={self.nack_sent_count}")
        
        try:
            self.socket.close()
        except:
            pass
        print(f"{get_timestamp()} [Receiver] Cleanup completed")

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 receive.py <sender_ip> [enable_nack]")
        print("Example: python3 receive.py 10.35.146.109")
        print("Example: python3 receive.py 10.35.146.109 true")
        sys.exit(1)
    
    sender_ip = sys.argv[1]
    enable_nack = False  # 默认不启用NACK
    
    if len(sys.argv) > 2:
        enable_nack = sys.argv[2].lower() in ['true', 'yes', '1']
    
    receiver = RTPReceiver(sender_ip, enable_nack=enable_nack)
    receiver.start()

if __name__ == "__main__":
    main() 
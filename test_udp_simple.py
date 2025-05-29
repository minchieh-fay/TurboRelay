#!/usr/bin/env python3
"""
简单UDP测试脚本
测试基本的UDP通信是否正常
"""

import socket
import time
import threading

def udp_receiver(port):
    """UDP接收器"""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(('0.0.0.0', port))
    sock.settimeout(5.0)
    
    print(f"UDP接收器启动，监听端口 {port}")
    
    try:
        while True:
            try:
                data, addr = sock.recvfrom(1500)
                print(f"收到UDP包: 来自 {addr}, 大小 {len(data)} 字节, 内容: {data[:50]}...")
            except socket.timeout:
                print("接收超时，继续等待...")
                break
    except KeyboardInterrupt:
        print("接收器停止")
    finally:
        sock.close()

def udp_sender(target_ip, target_port, message, count=5):
    """UDP发送器"""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    
    print(f"发送UDP包到 {target_ip}:{target_port}")
    
    for i in range(count):
        data = f"{message} - 包 {i+1}".encode('utf-8')
        sock.sendto(data, (target_ip, target_port))
        print(f"发送: {data}")
        time.sleep(0.5)
    
    sock.close()

def main():
    TARGET_IP = "10.35.146.109"
    TARGET_PORT = 5004
    LISTEN_PORT = 6004
    
    print("简单UDP通信测试")
    print(f"目标: {TARGET_IP}:{TARGET_PORT}")
    print(f"监听: 0.0.0.0:{LISTEN_PORT}")
    print("=" * 40)
    
    # 启动接收器
    receiver_thread = threading.Thread(target=udp_receiver, args=(LISTEN_PORT,))
    receiver_thread.daemon = True
    receiver_thread.start()
    
    time.sleep(1)  # 等待接收器启动
    
    # 发送测试包
    udp_sender(TARGET_IP, TARGET_PORT, "UDP测试包")
    
    # 等待接收
    print("\n等待接收包...")
    time.sleep(6)
    
    print("测试完成")

if __name__ == "__main__":
    main() 
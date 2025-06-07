# 反向RTP测试说明 (Terminal → MCU)

## 测试拓扑
```
终端 (10.35.146.109:5533) → TurboRelay → MCU (10.35.146.7:5533)
```

## 测试脚本

### 1. ter_send.py - 终端发送脚本
**功能**：
- 正常发送RTP包，不模拟乱序和丢包
- 每50ms发送一个包，序列号从0-65535循环
- 接收NACK并响应重传
- 使用SSRC: 0x12345678

**特点**：
- 模拟真实终端设备的正常发送行为
- 能够响应MCU的NACK请求进行重传
- 重传包的payload会标记为"RETRANSMIT"便于识别

### 2. mcu_receive.py - MCU接收脚本
**功能**：
- 接收RTP包并打印序列号
- 每收到5个包，向终端发送NACK要求重传前面某个包（前1-5随机选择）
- 使用SSRC: 0x87654321

**特点**：
- 模拟MCU主动请求重传的行为
- 随机选择前面的包进行NACK，测试重传机制
- 只处理RTP包，忽略RTCP包

## 测试步骤

### 1. 启动TurboRelay
```bash
# 在加速器上运行
sudo ./turbo_relay -i1 eth0 -i2 eth1 -v
```

### 2. 启动MCU接收端
```bash
# 在10.35.146.7上运行
python3 mcu_receive.py 10.35.146.109
```

### 3. 启动终端发送端
```bash
# 在10.35.146.109上运行
python3 ter_send.py 10.35.146.7
```

## 预期结果

### 终端发送端日志
```
=== Terminal Sender ===
14:30:15.123 [Terminal] Starting RTP sender...
14:30:15.124 [Terminal] Target: 10.35.146.7:5533
14:30:15.124 [Terminal] Features: Normal sending, 50ms interval
14:30:15.124 [Terminal] Starting NACK receiver...
14:30:15.125 [Terminal] Sent seq=0
14:30:15.175 [Terminal] Sent seq=1
...
14:30:16.425 [Terminal] Received NACK for seq=2
14:30:16.426 [Terminal] Retransmitted seq=2
```

### MCU接收端日志
```
=== MCU Receiver ===
14:30:15.200 [MCU] Terminal IP: 10.35.146.109
14:30:15.201 [MCU] Starting packet receiver...
14:30:15.201 [MCU] Listening on port 5533
14:30:15.201 [MCU] Features: Every 5 packets send NACK for random previous packet
14:30:15.250 [MCU] Received seq=0 (count=1)
14:30:15.300 [MCU] Received seq=1 (count=2)
...
14:30:15.550 [MCU] Received seq=4 (count=5)
14:30:15.551 [MCU] Triggering NACK: received 5 packets, requesting retransmit of seq=2
14:30:15.552 [MCU] Sent NACK for seq=2
14:30:15.580 [MCU] Received seq=2 (count=6)  # 重传包
```

### TurboRelay日志
```
[RTP] Processing RTP packet: seq=0, ssrc=0x12345678
[RTP] Forwarding RTP packet to far-end
[RTP] Processing RTCP NACK: requesting seq=2
[RTP] Sending NACK to near-end for seq=2
[RTP] Processing retransmitted RTP packet: seq=2
[RTP] Forwarding retransmitted packet to far-end
```

## 测试验证点

1. **正常转发**：终端发送的RTP包能正确到达MCU
2. **NACK机制**：MCU发送的NACK能正确到达终端
3. **重传机制**：终端收到NACK后能正确重传对应的包
4. **序列号处理**：所有序列号都能正确处理和显示
5. **双向通信**：RTP和RTCP包都能正确双向转发

## 调试命令

### 抓包分析
```bash
# 在终端机器上抓包
sudo tcpdump -i any -n port 5533 -v

# 在MCU机器上抓包
sudo tcpdump -i any -n port 5533 -v

# 在TurboRelay机器上抓包
sudo tcpdump -i eth0 -n port 5533 -v
sudo tcpdump -i eth1 -n port 5533 -v
```

### 网络连通性测试
```bash
# 从终端ping MCU
ping 10.35.146.7

# 从MCU ping终端
ping 10.35.146.109

# UDP端口测试
nc -u 10.35.146.7 5533
nc -u 10.35.146.109 5533
```

## 故障排除

1. **终端发送但MCU收不到**：检查TurboRelay转发配置
2. **MCU发送NACK但终端收不到**：检查RTCP包转发
3. **终端收到NACK但不重传**：检查NACK包格式和解析
4. **重传包MCU收不到**：检查重传包的转发路径

## 测试场景扩展

1. **高频NACK**：修改MCU脚本每收到2个包就发NACK
2. **批量NACK**：修改MCU脚本一次NACK多个包
3. **序列号回绕**：让终端发送接近65535的序列号测试回绕
4. **长时间测试**：运行数小时测试稳定性 
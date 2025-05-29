# RTP通讯测试说明

## 测试拓扑
```
远端MCU(10.35.146.7:22) <---> 加速器(运行turbo_relay) <---> 近端终端(10.35.146.109:10810)
```

## 测试程序功能

### 远端测试程序 (test_far_end.py)
运行在远端MCU (10.35.146.7)

**接收功能：**
1. 接收来自近端的RTP流
2. 打印接收到的SSRC和序列号
3. 模拟10%丢包（即便收到了，假装没收到）
4. 每10个包丢一个，打印"ssrc xxx seq xxx lost"
5. 丢包后20ms发送NACK给近端，要求重发
6. 维护接收序列号容器，删除连续的序列号
7. 每秒打印当前缺失的包列表

**发送功能：**
1. 每50ms发送一个RTP包给近端
2. 序列号从0-65535循环发送
3. 模拟乱序：不超过3个包的延迟
4. 模拟丢包：每发送10个包丢一个

### 近端测试程序 (test_near_end.py)
运行在近端终端 (10.35.146.109)

**接收功能：**
1. 接收来自远端的RTP流
2. 严格检查序列号连续性、无乱序、无丢包
3. 一旦发现问题立即打印错误并停止程序

**发送功能：**
1. 每50ms发送一个RTP包给远端
2. 序列号从0-65535循环发送
3. 正常发送，不模拟任何网络问题

## 测试步骤

### 1. 启动TurboRelay
在加速器上运行：
```bash
sudo ./turbo_relay -i1 eth0 -i2 eth1 -v
```

### 2. 启动远端测试程序
在远端MCU (10.35.146.7) 上运行：
```bash
python3 test_far_end.py 10.35.146.109
```

### 3. 启动近端测试程序
在近端终端 (10.35.146.109) 上运行：
```bash
python3 test_near_end.py 10.35.146.7
```

## 预期结果

### 正常情况
- 两个程序可以一直运行下去
- 远端程序会模拟网络问题但正常工作
- 近端程序接收到的包应该是连续的（经过TurboRelay转发后）
- 远端程序会定期打印缺失包列表和NACK信息

### 异常情况
- 如果近端程序检测到乱序、丢包或重复包，会立即停止并报错
- 这表明TurboRelay的转发或RTP处理有问题

## 测试特点

1. **序列号回绕测试**：从0-65535循环，测试65535→0的回绕处理
2. **网络问题模拟**：远端模拟真实网络环境的丢包和乱序
3. **严格验证**：近端进行严格的连续性检查
4. **NACK机制**：远端会发送NACK包请求重传
5. **长期运行**：正常情况下可以持续运行

## 调试命令

### 查看网络流量
```bash
# 监控RTP端口5533
sudo tcpdump -i any -n port 5533

# 监控RTCP端口5534  
sudo tcpdump -i any -n port 5534

# 查看特定IP的流量
sudo tcpdump -i any -n host 10.35.146.7 or host 10.35.146.109
```

### 检查端口占用
```bash
netstat -ulnp | grep 553
```

### 测试网络连通性
```bash
# 从近端测试到远端
ping 10.35.146.7

# 从远端测试到近端  
ping 10.35.146.109
```

## 故障排除

1. **程序无法启动**
   - 检查端口是否被占用
   - 确认Python3已安装
   - 检查防火墙设置

2. **近端程序立即报错**
   - 检查TurboRelay是否正常运行
   - 确认网络路由配置正确
   - 查看是否有其他程序干扰

3. **无法接收到包**
   - 检查IP地址和端口配置
   - 确认网络连通性
   - 查看TurboRelay日志

4. **序列号异常**
   - 检查是否有多个发送程序同时运行
   - 确认SSRC配置正确
   - 查看网络是否有重复转发

## 日志分析

### 远端程序日志示例
```
[Far-end] Received ssrc 0x12345678 seq 100
[Far-end] ssrc 0x12345678 seq 101 lost (simulated)
[Far-end] Sent NACK for seq=101
[Far-end] Sent RTP seq=50
[Far-end] Lost: [101, 105, 108]
```

### 近端程序日志示例
```
[Near-end] Received ssrc 0x87654321 seq 50
[Near-end] Sent 100 RTP packets, seq=99
[Near-end] Received 100 packets, all in order
```

### 错误日志示例
```
[Near-end] ERROR: Sequence discontinuity!
[Near-end] Expected seq=102, but got seq=104
[Near-end] ERROR: Packet loss detected!
[Near-end] ERROR DETECTED! Stopping test...
```
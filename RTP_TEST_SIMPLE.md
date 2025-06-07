# 简化版RTP测试脚本说明

## 脚本概述

重新设计了两个简单的测试脚本：
- `send.py` - 发送端脚本
- `receive.py` - 接收端脚本

## 功能特点

### 发送端 (send.py)
- **端口**: 5533 (RTP和RTCP共用)
- **发送间隔**: 20ms
- **模拟问题**: 10%乱序 + 10%丢包
- **NACK响应**: 可选开启/关闭
- **重传标识**: 重传包payload更大，便于抓包识别

### 接收端 (receive.py)
- **端口**: 5533 (RTP和RTCP共用)
- **排序队列**: 自动维护，不限长度
- **自动清理**: 前面10个连续包自动删除
- **缺失检测**: 实时显示缺失的序列号
- **NACK发送**: 自动发送NACK请求

## 使用方法

### 1. 启动发送端
```bash
# 启用NACK响应（默认）
python3 send.py 10.35.146.7

# 禁用NACK响应
python3 send.py 10.35.146.7 false
```

### 2. 启动接收端
```bash
python3 receive.py 10.35.146.109
```

## 输出格式

### 发送端输出
```
[Sender] Sent 100 packets, dropped 12, reordered 8, retransmitted 3
[Sender] Dropped seq=45
[Sender] Buffered for reorder seq=67
[Sender] Received NACK for seq=45
[Sender] Retransmitted seq=45 (larger payload)
```

### 接收端输出
```
[Receiver] 233----- [230,231]
[Receiver] 230----- [231] (RETRANSMIT)
[Receiver] 231----- [] (RETRANSMIT)
[Receiver] Sent NACK for seq=230
[Receiver] Removed 10 consecutive packets: 220-229
```

## 测试场景

### 场景1: 基本功能测试
1. 在109机器运行: `python3 receive.py 10.35.146.7`
2. 在7机器运行: `python3 send.py 10.35.146.109`
3. 观察乱序、丢包、NACK重传过程

### 场景2: 无NACK测试
1. 在109机器运行: `python3 receive.py 10.35.146.7`
2. 在7机器运行: `python3 send.py 10.35.146.109 false`
3. 观察丢包累积，无重传

### 场景3: TurboRelay转发测试
1. 启动TurboRelay在中间转发
2. 在7机器运行: `python3 send.py <turbo_relay_ip>`
3. 在109机器运行: `python3 receive.py <turbo_relay_ip>`
4. 观察TurboRelay的RTP处理效果

## 关键特性

### 乱序模拟
- 10%的包会被缓存，等3个包后一起乱序发送
- 模拟真实网络的乱序情况

### 丢包模拟
- 10%的包直接跳过不发送
- 序列号继续递增，模拟真实丢包

### NACK机制
- 接收端检测到缺失包立即发送NACK
- 发送端收到NACK后重传对应包
- 重传包payload更大，便于抓包区分

### 队列管理
- 接收端维护排序队列
- 前面10个连续包自动删除，避免内存累积
- 实时显示当前缺失的序列号

## 调试建议

1. **抓包分析**: 重传包payload更大，容易识别
2. **日志观察**: 详细的时间戳和状态信息
3. **统计监控**: 定期输出发送/接收/重传统计
4. **队列状态**: 每5秒显示队列大小和范围

这个简化版本更容易理解和调试，专注于RTP的核心功能测试。 
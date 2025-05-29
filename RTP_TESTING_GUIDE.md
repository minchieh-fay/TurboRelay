# RTP序列号窗口测试指南

## 概述

本文档描述如何测试TurboRelay的RTP序列号窗口功能。新的序列号窗口逻辑使用32位滑动窗口来处理乱序、丢包和重复包。

## 测试环境

- **近端设备**: 10.35.146.109:10810 (终端)
- **远端设备**: 10.35.146.7:22 (MCU)
- **测试端口**: 5004 (RTP)

## 测试工具

### 1. Python测试发送器 (推荐)

```bash
# 基本用法
python3 rtp_test_sender.py <目标IP> <目标端口>

# 示例：从远端发送到近端测试远端包处理
python3 rtp_test_sender.py 10.35.146.9 5004 --source "远端MCU"

# 示例：从近端发送到远端测试近端包处理
python3 rtp_test_sender.py 10.35.146.7 5004 --source "近端终端"

# 运行特定测试
python3 rtp_test_sender.py 10.35.146.9 5004 --test normal --interval 0.2
python3 rtp_test_sender.py 10.35.146.9 5004 --test ooo --interval 0.5
python3 rtp_test_sender.py 10.35.146.9 5004 --test loss --interval 1.0
```

### 2. Bash测试脚本

```bash
# 本地测试
chmod +x test_rtp_window.sh
./test_rtp_window.sh

# 远程测试
chmod +x test_rtp_remote.sh
./test_rtp_remote.sh
```

## 测试场景

### 1. 正常序列测试
**目的**: 验证正常序列号处理
**序列**: 1, 2, 3, 4, 5
**预期**: 所有包按顺序转发

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test normal --source "远端MCU"
```

### 2. 乱序测试
**目的**: 验证序列号窗口的乱序处理
**序列**: 10, 12, 11, 13, 15, 14
**预期**: 
- 10: 立即转发 (新会话)
- 12: 缓存 (等待11)
- 11: 转发11和12
- 13: 立即转发
- 15: 缓存 (等待14)
- 14: 转发14和15

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test ooo --source "远端MCU"
```

### 3. 丢包测试
**目的**: 验证NACK机制
**序列**: 20, 21, 22, 25, 26, [延迟], 23, 24
**预期**:
- 20-22: 正常转发
- 25-26: 缓存，触发NACK
- 超时后: 转发缓存的包
- 23-24: 补发包处理

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test loss --source "远端MCU"
```

### 4. 序列号回绕测试
**目的**: 验证16位序列号回绕处理
**序列**: 65533, 65534, 65535, 0, 1, 2
**预期**: 正确处理回绕，按顺序转发

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test wrap --source "远端MCU"
```

### 5. 重复包测试
**目的**: 验证重复包检测
**序列**: 30, 31, 31, 32, 32, 33
**预期**: 重复的31和32被丢弃

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test dup --source "远端MCU"
```

### 6. 大跳跃测试
**目的**: 验证窗口重置机制
**序列**: 40, 41, 42, 1000, 1001
**预期**: 1000触发窗口重置，1000和1001正常转发

```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --test jump --source "远端MCU"
```

## 双向测试

### 近端包测试
```bash
# 从近端发送 (应该直接转发并缓存)
python3 rtp_test_sender.py 10.35.146.7 5004 --source "近端终端" --test normal
```

### 远端包测试
```bash
# 从远端发送 (使用序列号窗口处理)
python3 rtp_test_sender.py 10.35.146.9 5004 --source "远端MCU" --test all
```

## 验证要点

### 1. 日志检查
在turbo_relay的调试日志中查看：

```
Far-end RTP: SSRC=305419896, Seq=10, Forward=true, New=true, Window=[Base:10 Max:10 Mask:0x00000001 Missing:[]]
Far-end RTP: SSRC=305419896, Seq=12, Forward=false, New=true, Window=[Base:10 Max:12 Mask:0x00000005 Missing:[11]]
Far-end RTP: SSRC=305419896, Seq=11, Forward=true, New=true, Window=[Base:10 Max:12 Mask:0x00000007 Missing:[]]
```

### 2. 统计信息
检查RTP处理统计：
- `TotalRTPPackets`: 总RTP包数
- `FarEndStats.ReceivedPackets`: 远端接收包数
- `FarEndStats.ForwardedPackets`: 远端转发包数
- `FarEndStats.BufferedPackets`: 远端缓存包数
- `FarEndStats.DroppedPackets`: 远端丢弃包数
- `FarEndStats.NACKsSent`: 发送的NACK数

### 3. 窗口状态
关注窗口状态信息：
- `Base`: 窗口基准序列号
- `Max`: 窗口内最大序列号
- `Mask`: 接收位掩码 (1表示已接收)
- `Missing`: 缺失的序列号列表

## 故障排除

### 1. 包未被识别为RTP
- 检查包长度 (至少12字节)
- 检查RTP版本 (应为2)
- 检查payload type (应 < 128)

### 2. 序列号处理异常
- 检查`calculateSeqDiff`函数的回绕处理
- 验证窗口滑动逻辑
- 确认位掩码操作正确

### 3. NACK未触发
- 检查NACK超时设置
- 验证缺失序列号检测
- 确认NACK包构建正确

### 4. 性能问题
- 监控缓存大小
- 检查清理机制
- 观察内存使用

## 测试脚本参数

### Python发送器参数
- `target_ip`: 目标IP地址
- `target_port`: 目标端口
- `--source`: 源描述 (用于日志)
- `--test`: 测试类型 (all/normal/ooo/loss/wrap/dup/jump)
- `--interval`: 包间隔时间 (秒)

### 测试类型说明
- `all`: 运行所有测试
- `normal`: 正常序列测试
- `ooo`: 乱序测试 (out-of-order)
- `loss`: 丢包测试
- `wrap`: 序列号回绕测试
- `dup`: 重复包测试
- `jump`: 大跳跃测试

## 示例测试流程

1. **启动turbo_relay** (在目标设备上)
```bash
./turbo_relay if1 if2 --enable-rtp --debug
```

2. **运行远端包测试**
```bash
python3 rtp_test_sender.py 10.35.146.9 5004 --source "远端MCU" --test all --interval 1.0
```

3. **运行近端包测试**
```bash
python3 rtp_test_sender.py 10.35.146.7 5004 --source "近端终端" --test normal --interval 0.5
```

4. **检查日志和统计信息**
观察turbo_relay的输出，验证序列号窗口逻辑是否正常工作。

## 注意事项

1. **网络延迟**: 测试时考虑网络延迟对NACK超时的影响
2. **包大小**: 默认使用160字节payload，可根据需要调整
3. **SSRC**: 默认使用0x12345678，可在代码中修改
4. **端口**: 确保测试端口未被其他程序占用
5. **权限**: 某些系统可能需要root权限发送UDP包

通过这些测试，可以全面验证RTP序列号窗口逻辑的正确性和性能。 
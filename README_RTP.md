# RTP包设计文档

## 概述

RTP包实现了实时传输协议（RTP）和实时传输控制协议（RTCP）的处理功能，专门用于TurboRelay网络转发器中的音视频流优化。

## 核心特性

### 1. 双向SSRC处理
- **问题**：同一个SSRC在不同方向可能代表不同的流
- **解决方案**：使用`SessionKey`结构体，包含SSRC和方向信息
- **支持场景**：
  - if1（终端）→ MCU的SSRC=2流
  - MCU → if1（终端）的SSRC=2流
  - 音频流、视频流、演示流等多种流类型

### 2. 近端包处理（if1来源）
- **特点**：认为不会丢包（直连）
- **处理策略**：
  - 记录会话信息（IP、端口、MAC地址等）
  - 缓存100-500ms的RTP数据包
  - 所有包都转发给对端（MCU）
  - 支持NACK重传请求

### 3. 远端包处理（if2来源）
- **特点**：MCU发来的包可能丢包（路径长）
- **处理策略**：
  - 记录会话信息
  - 缓存并排序RTP包（处理序列号65535→0的循环）
  - 顺序正确直接转发给if1
  - 丢包或乱序时sleep 10-50ms后发送NACK
  - NACK优先使用RTCP信息，否则使用RTP端口

### 4. 超时清理机制
- **清理周期**：每30秒检查一次
- **超时时间**：1分钟未使用的SSRC数据
- **清理内容**：
  - 会话信息
  - 近端缓存
  - 远端缓存
  - 统计信息

## 接口设计

### 核心接口

```go
type PacketProcessor interface {
    ProcessPacket(packet gopacket.Packet, isNearEnd bool) (shouldForward bool, err error)
    GetStats() ProcessorStats
    SetPacketSender(sender PacketSender)
    Start() error
    Stop()
}
```

### 配置结构

```go
type ProcessorConfig struct {
    NearEndInterface string        // 近端接口名称
    FarEndInterface  string        // 远端接口名称
    BufferDuration   time.Duration // 近端缓存时长 (100-500ms)
    NACKTimeout      time.Duration // NACK超时时间 (10-50ms)
    Debug            bool          // 调试模式
}
```

### 统计信息

```go
type ProcessorStats struct {
    TotalUDPPackets    uint64
    TotalRTPPackets    uint64
    TotalRTCPPackets   uint64
    TotalNonRTPPackets uint64
    NearEndStats       EndpointStats
    FarEndStats        EndpointStats
    ActiveSessions     map[uint32]*SessionInfo
}
```

## 使用方法

### 1. 基本使用

```go
// 创建配置
config := rtp.ProcessorConfig{
    NearEndInterface: "eth1",
    FarEndInterface:  "eth0", 
    BufferDuration:   200 * time.Millisecond,
    NACKTimeout:      30 * time.Millisecond,
    Debug:            true,
}

// 创建处理器
processor := rtp.NewPacketProcessor(config)

// 设置包发送回调
sender := rtp.NewPacketSender(func(data []byte, toInterface string) error {
    // 调用实际的包发送函数
    return sendPacketToInterface(data, toInterface)
})
processor.SetPacketSender(sender)

// 启动处理器
processor.Start()
defer processor.Stop()

// 处理包
shouldForward, err := processor.ProcessPacket(packet, isNearEnd)
if err != nil {
    log.Printf("RTP processing error: %v", err)
}
if shouldForward {
    // 转发包
}
```

### 2. 使用ProcessorManager

```go
// 创建管理器
manager := rtp.NewProcessorManager(config)

// 设置包发送函数
sendFunc := func(data []byte, toInterface string) error {
    return sendPacketToInterface(data, toInterface)
}
sender := rtp.NewPacketSender(sendFunc)
manager.SetPacketSender(sender)

// 启动
manager.Start()
defer manager.Stop()

// 动态控制
manager.SetEnabled(false) // 禁用RTP处理
manager.SetEnabled(true)  // 重新启用

// 获取统计信息
stats := manager.GetStats()
```

## 集成到网络转发器

### 在forwardPackets函数中集成

```go
func forwardPackets(/* 参数 */) {
    // ... 现有代码 ...
    
    // 在writePacket之前劫持UDP包
    if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
        // 判断是否为近端包
        isNearEnd := (sourceInterface == nearEndInterface)
        
        // RTP处理
        shouldForward, err := rtpManager.ProcessPacket(packet, isNearEnd)
        if err != nil {
            log.Printf("RTP processing error: %v", err)
        }
        
        if !shouldForward {
            // RTP处理器决定不转发此包
            continue
        }
    }
    
    // 正常转发逻辑
    writePacket(packet, targetInterface)
}
```

## 技术细节

### 1. 序列号处理
- 支持16位序列号的回绕（65535 → 0）
- 使用差值计算处理乱序和丢包检测
- 合理范围内的跳跃（≤100）触发缓存和NACK

### 2. NACK机制
- 基于RTCP RFC 4585标准
- 优先使用已记录的RTCP端口信息
- 简化的NACK包构建（生产环境需要完整实现）

### 3. 缓存管理
- 近端缓存：支持重传请求
- 远端缓存：排序和丢包检测
- 自动清理过期数据

### 4. 线程安全
- 使用读写锁保护共享数据
- 异步处理NACK超时
- 独立的清理协程

## 扩展性

### 1. 包发送接口
- 抽象的PacketSender接口
- 支持不同的底层发送实现
- 回调函数模式便于集成

### 2. 统计监控
- 详细的统计信息
- 支持实时监控
- 便于性能分析和调试

### 3. 配置灵活性
- 可调节的缓存时长
- 可配置的NACK超时
- 调试模式开关

## 注意事项

1. **RTP/RTCP端口**：两者端口可能不同，需要分别记录
2. **序列号回绕**：正确处理65535→0的循环
3. **内存管理**：定期清理过期会话避免内存泄漏
4. **性能考虑**：高频包处理需要优化锁竞争
5. **错误处理**：网络异常时的容错机制

## 未来优化

1. **完整RTCP实现**：支持更多RTCP包类型
2. **自适应缓存**：根据网络状况动态调整缓存大小
3. **QoS支持**：基于包类型的优先级处理
4. **统计增强**：更详细的性能指标和报告 
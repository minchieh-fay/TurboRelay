# TurboRelay 接口设计文档

## 概述

TurboRelay 现在采用了模块化的接口设计，将网络包转发功能封装为标准的 Go 接口，提供更好的可扩展性和可测试性。

## 核心接口

### PacketForwarder 接口

```go
type PacketForwarder interface {
    Start() error                    // 启动包转发器
    Stop()                          // 停止包转发器
    GetStats() ForwardingStats      // 获取转发统计信息
    SetDebugMode(enabled bool)      // 设置调试模式
}
```

## 数据结构

### ForwarderConfig - 转发器配置

```go
type ForwarderConfig struct {
    Interface1   string  // 第一个网络接口（近端，直连设备）
    Interface2   string  // 第二个网络接口（远端，MCU）
    Debug        bool    // 是否启用调试模式
    If1IsNearEnd bool    // Interface1是否为近端
    If2IsNearEnd bool    // Interface2是否为近端
}
```

### ForwardingStats - 转发统计信息

```go
type ForwardingStats struct {
    If1ToIf2Stats   DirectionStats  // Interface1 到 Interface2 方向统计
    If2ToIf1Stats   DirectionStats  // Interface2 到 Interface1 方向统计
    TotalPackets    uint64          // 总包数
    TotalSuccess    uint64          // 总成功数
    TotalErrors     uint64          // 总错误数
    OverallLossRate float64         // 总体丢包率
}
```

### DirectionStats - 方向统计信息

```go
type DirectionStats struct {
    SourceInterface string   // 源接口
    DestInterface   string   // 目标接口
    EndType         string   // 端点类型："近端(直连设备)" 或 "远端(MCU)"
    PacketCount     uint64   // 包总数
    SuccessCount    uint64   // 成功转发数
    ErrorCount      uint64   // 错误数
    LossRate        float64  // 丢包率
    ICMPCount       uint64   // ICMP包数量
    ARPCount        uint64   // ARP包数量
    OtherCount      uint64   // 其他包数量
}
```

## 使用方法

### 1. 基本使用

```go
// 创建配置
config := net.ForwarderConfig{
    Interface1:   "eth1",  // 近端接口
    Interface2:   "eth0",  // 远端接口
    Debug:        false,
    If1IsNearEnd: true,    // eth1连接直连设备
    If2IsNearEnd: false,   // eth0连接MCU
}

// 创建转发器
forwarder := net.NewPacketForwarder(config)

// 启动转发
if err := forwarder.Start(); err != nil {
    log.Fatal(err)
}

// 获取统计信息
stats := forwarder.GetStats()
fmt.Printf("总包数: %d, 丢包率: %.2f%%\n", 
    stats.TotalPackets, stats.OverallLossRate)

// 停止转发
forwarder.Stop()
```

### 2. 统计信息监控

```go
// 定期获取统计信息
ticker := time.NewTicker(5 * time.Second)
go func() {
    for range ticker.C {
        stats := forwarder.GetStats()
        
        // 输出JSON格式统计
        jsonData, _ := json.MarshalIndent(stats, "", "  ")
        fmt.Printf("统计信息:\n%s\n", jsonData)
        
        // 检查丢包率警告
        if stats.OverallLossRate > 5.0 {
            log.Printf("警告: 丢包率过高 (%.2f%%)", stats.OverallLossRate)
        }
    }
}()
```

### 3. 动态调试模式

```go
// 运行时切换调试模式
forwarder.SetDebugMode(true)   // 开启调试
time.Sleep(10 * time.Second)
forwarder.SetDebugMode(false)  // 关闭调试
```

## 网络拓扑

```
[3方设备] ←→ [eth1] ←→ [TurboRelay] ←→ [eth0] ←→ [MCU]
   近端              双向转发              远端
  (稳定)                                (可能丢包)
```

## 转发特性

### 近端连接 (Interface1 → Interface2)
- 连接3方设备，直连稳定
- 每个丢包都会记录详细错误
- 预期丢包率接近0%

### 远端连接 (Interface2 → Interface1)  
- 连接MCU，可能通过网络
- 丢包统计较宽松（每100个丢包报告一次）
- 允许一定的丢包率

## 包类型支持

- **IP包**: 支持IPv4和IPv6
- **ICMP包**: ping包，优先显示
- **ARP包**: 地址解析协议包，优先显示
- **TCP/UDP包**: 传输层协议包

## 统计功能

- 实时包计数和成功率统计
- 按包类型分类统计（ICMP/ARP/其他）
- 双向独立统计
- 丢包率计算
- JSON格式导出

## 文件结构

```
net/
├── interface.go    # 对外接口定义
├── forwarder.go    # 核心转发实现
└── ...

examples/
└── usage_example.go # 使用示例

main.go            # 主程序入口
```

## 扩展性

通过接口设计，可以轻松：
- 添加新的统计指标
- 实现不同的转发策略
- 集成到其他系统中
- 编写单元测试
- 支持配置热更新 
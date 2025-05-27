package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"turborelay/net"
)

// 这个文件展示了如何使用TurboRelay的PacketForwarder接口

func main() {
	// 示例1: 基本使用
	basicExample()

	// 示例2: 统计信息监控
	// statisticsExample()

	// 示例3: 动态调试模式
	// debugModeExample()
}

func basicExample() {
	fmt.Println("=== 基本使用示例 ===")

	// 创建配置
	config := net.ForwarderConfig{
		Interface1:   "eth1", // 近端接口（连接3方设备）
		Interface2:   "eth0", // 远端接口（连接MCU）
		Debug:        false,
		If1IsNearEnd: true,  // if1是近端
		If2IsNearEnd: false, // if2是远端
	}

	// 创建转发器实例
	forwarder := net.NewPacketForwarder(config)

	// 启动转发器
	if err := forwarder.Start(); err != nil {
		log.Fatalf("Failed to start forwarder: %v", err)
	}

	// 运行一段时间
	time.Sleep(10 * time.Second)

	// 获取统计信息
	stats := forwarder.GetStats()
	fmt.Printf("转发统计: 总包数=%d, 成功=%d, 失败=%d, 丢包率=%.2f%%\n",
		stats.TotalPackets, stats.TotalSuccess, stats.TotalErrors, stats.OverallLossRate)

	// 停止转发器
	forwarder.Stop()
}

func statisticsExample() {
	fmt.Println("=== 统计信息监控示例 ===")

	config := net.ForwarderConfig{
		Interface1:   "eth1",
		Interface2:   "eth0",
		Debug:        false,
		If1IsNearEnd: true,
		If2IsNearEnd: false,
	}

	forwarder := net.NewPacketForwarder(config)

	if err := forwarder.Start(); err != nil {
		log.Fatalf("Failed to start forwarder: %v", err)
	}

	// 定期打印统计信息
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			stats := forwarder.GetStats()

			// 转换为JSON格式输出
			jsonData, _ := json.MarshalIndent(stats, "", "  ")
			fmt.Printf("统计信息:\n%s\n", jsonData)

			// 检查丢包率
			if stats.OverallLossRate > 5.0 {
				fmt.Printf("警告: 丢包率过高 (%.2f%%)\n", stats.OverallLossRate)
			}
		}
	}()

	// 运行30秒
	time.Sleep(30 * time.Second)
	forwarder.Stop()
}

func debugModeExample() {
	fmt.Println("=== 动态调试模式示例 ===")

	config := net.ForwarderConfig{
		Interface1:   "eth1",
		Interface2:   "eth0",
		Debug:        false, // 初始关闭调试
		If1IsNearEnd: true,
		If2IsNearEnd: false,
	}

	forwarder := net.NewPacketForwarder(config)

	if err := forwarder.Start(); err != nil {
		log.Fatalf("Failed to start forwarder: %v", err)
	}

	// 运行5秒后开启调试模式
	time.Sleep(5 * time.Second)
	fmt.Println("开启调试模式...")
	forwarder.SetDebugMode(true)

	// 调试模式运行10秒
	time.Sleep(10 * time.Second)

	// 关闭调试模式
	fmt.Println("关闭调试模式...")
	forwarder.SetDebugMode(false)

	// 再运行5秒
	time.Sleep(5 * time.Second)

	forwarder.Stop()
}

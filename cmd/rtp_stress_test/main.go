package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// StressTestConfig 压力测试配置
type StressTestConfig struct {
	PacketCount      int           // 总包数
	DropRate         float64       // 丢包率 (0.0-1.0)
	ReorderRate      float64       // 乱序率 (0.0-1.0)
	MaxReorderDelay  int           // 最大乱序延迟包数
	StartSeq         uint16        // 起始序列号
	SSRC             uint32        // SSRC
	PacketInterval   time.Duration // 包间隔
	EnableWrapAround bool          // 是否启用回绕测试
}

// StressTestResult 压力测试结果
type StressTestResult struct {
	Config            StressTestConfig
	SentPackets       int
	ReceivedPackets   int
	ForwardedPackets  int
	DroppedPackets    int
	BufferedPackets   int
	NACKsSent         int
	OutOfOrderPackets int
	ProcessingTime    time.Duration
	ThroughputPPS     float64 // 每秒处理包数
}

// StressTestSender 压力测试发送器
type StressTestSender struct {
	forwardedCount int
	nackCount      int
	rtpCount       int
	rtcpCount      int
	logFile        *os.File
}

func (s *StressTestSender) SendPacket(data []byte, toInterface string) error {
	if len(data) >= 12 {
		version := (data[0] >> 6) & 0x3
		if version == 2 {
			payloadType := data[1] & 0x7F
			if payloadType >= 200 && payloadType <= 204 {
				s.rtcpCount++
				s.nackCount++
			} else {
				s.rtpCount++
				s.forwardedCount++
			}
		}
	} else {
		// 非RTP包也算作转发包
		s.forwardedCount++
	}

	// 记录关键信息到日志
	if s.logFile != nil && s.forwardedCount <= 10 {
		if len(data) >= 4 {
			seq := binary.BigEndian.Uint16(data[2:4])
			fmt.Fprintf(s.logFile, "转发包: seq=%d 到 %s\n", seq, toInterface)
		} else {
			fmt.Fprintf(s.logFile, "转发包: 长度=%d 到 %s\n", len(data), toInterface)
		}
	}

	return nil
}

func (s *StressTestSender) GetStats() (int, int, int, int) {
	return s.forwardedCount, s.nackCount, s.rtpCount, s.rtcpCount
}

func (s *StressTestSender) Reset() {
	s.forwardedCount = 0
	s.nackCount = 0
	s.rtpCount = 0
	s.rtcpCount = 0
}

// createTestRTPPacket 创建测试RTP包
func createTestRTPPacket(ssrc uint32, seq uint16, timestamp uint32, payload []byte) []byte {
	rtpHeader := make([]byte, 12)

	rtpHeader[0] = 0x80 // Version 2
	rtpHeader[1] = 96   // Payload Type
	binary.BigEndian.PutUint16(rtpHeader[2:4], seq)
	binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
	binary.BigEndian.PutUint32(rtpHeader[8:12], ssrc)

	result := make([]byte, len(rtpHeader)+len(payload))
	copy(result, rtpHeader)
	copy(result[len(rtpHeader):], payload)

	return result
}

func simulateUDPPacket(rtpData []byte) []byte {
	eth := &layers.Ethernet{
		SrcMAC:       []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}

	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    []byte{192, 168, 1, 200},
		DstIP:    []byte{192, 168, 1, 100},
	}

	udp := &layers.UDP{
		SrcPort: layers.UDPPort(5004),
		DstPort: layers.UDPPort(5004),
	}
	udp.SetNetworkLayerForChecksum(ip)

	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	if err := gopacket.SerializeLayers(buffer, opts, eth, ip, udp, gopacket.Payload(rtpData)); err != nil {
		return nil
	}

	return buffer.Bytes()
}

// generatePacketSequence 生成包序列（包含丢包和乱序）
func generatePacketSequence(config StressTestConfig) []uint16 {
	sequences := make([]uint16, 0, config.PacketCount)

	// 生成基础序列
	for i := 0; i < config.PacketCount; i++ {
		seq := uint32(config.StartSeq) + uint32(i)
		if config.EnableWrapAround && seq > 65535 {
			seq = seq - 65536
		}
		sequences = append(sequences, uint16(seq))
	}

	// 应用丢包
	if config.DropRate > 0 {
		dropCount := int(float64(len(sequences)) * config.DropRate)
		for i := 0; i < dropCount; i++ {
			dropIndex := rand.Intn(len(sequences))
			sequences = append(sequences[:dropIndex], sequences[dropIndex+1:]...)
		}
	}

	// 应用乱序
	if config.ReorderRate > 0 {
		reorderCount := int(float64(len(sequences)) * config.ReorderRate)
		for i := 0; i < reorderCount; i++ {
			if len(sequences) < 2 {
				break
			}

			// 随机选择一个包
			srcIndex := rand.Intn(len(sequences))

			// 计算目标位置（在最大延迟范围内）
			maxDelay := config.MaxReorderDelay
			if maxDelay > len(sequences)-1 {
				maxDelay = len(sequences) - 1
			}

			dstIndex := srcIndex + rand.Intn(maxDelay+1)
			if dstIndex >= len(sequences) {
				dstIndex = len(sequences) - 1
			}

			// 交换包位置
			if srcIndex != dstIndex {
				sequences[srcIndex], sequences[dstIndex] = sequences[dstIndex], sequences[srcIndex]
			}
		}
	}

	return sequences
}

// runStressTest 运行压力测试
func runStressTest(config StressTestConfig) StressTestResult {
	fmt.Printf("\n=== 压力测试: %d包, 丢包率%.1f%%, 乱序率%.1f%% ===\n",
		config.PacketCount, config.DropRate*100, config.ReorderRate*100)

	// 创建日志文件
	logFileName := fmt.Sprintf("stress_test_%d_drop%.0f_reorder%.0f.log",
		config.PacketCount, config.DropRate*100, config.ReorderRate*100)
	logFile, err := os.Create(logFileName)
	if err != nil {
		log.Printf("创建日志文件失败: %v", err)
	}
	defer logFile.Close()

	// 创建RTP处理器
	rtpConfig := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   100 * time.Millisecond,
		NACKTimeout:      30 * time.Millisecond,
		Debug:            false, // 关闭调试以提高性能
	}

	processor := rtp.NewPacketProcessor(rtpConfig)
	sender := &StressTestSender{logFile: logFile}
	processor.SetPacketSender(sender)

	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 生成包序列
	sequences := generatePacketSequence(config)
	fmt.Printf("生成了 %d 个包（原始 %d 个，丢包 %d 个）\n",
		len(sequences), config.PacketCount, config.PacketCount-len(sequences))

	// 记录开始时间
	startTime := time.Now()

	// 发送包
	payload := []byte("Stress test RTP payload data")
	baseTimestamp := uint32(1000)

	for i, seq := range sequences {
		timestamp := baseTimestamp + uint32(seq*160)
		rtpData := createTestRTPPacket(config.SSRC, seq, timestamp, payload)

		udpPacket := simulateUDPPacket(rtpData)
		if udpPacket == nil {
			continue
		}

		packet := gopacket.NewPacket(udpPacket, layers.LayerTypeEthernet, gopacket.Default)

		// 处理包（模拟远端包）
		_, err := processor.ProcessPacket(packet, false)
		if err != nil {
			log.Printf("处理包失败: %v", err)
			continue
		}

		// 显示进度
		if i%1000 == 0 && i > 0 {
			fmt.Printf("已处理 %d/%d 包\n", i, len(sequences))
		}

		// 添加包间隔
		if config.PacketInterval > 0 {
			time.Sleep(config.PacketInterval)
		}
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	// 记录结束时间
	endTime := time.Now()
	processingTime := endTime.Sub(startTime)

	// 获取统计信息
	stats := processor.GetStats()
	forwardedCount, nackCount, _, _ := sender.GetStats()

	// 计算吞吐量
	throughputPPS := float64(len(sequences)) / processingTime.Seconds()

	result := StressTestResult{
		Config:            config,
		SentPackets:       len(sequences),
		ReceivedPackets:   int(stats.FarEndStats.ReceivedPackets),
		ForwardedPackets:  forwardedCount,
		DroppedPackets:    int(stats.FarEndStats.DroppedPackets),
		BufferedPackets:   int(stats.FarEndStats.BufferedPackets),
		NACKsSent:         nackCount,
		OutOfOrderPackets: int(stats.FarEndStats.OutOfOrderPackets),
		ProcessingTime:    processingTime,
		ThroughputPPS:     throughputPPS,
	}

	// 输出结果
	fmt.Printf("\n测试结果:\n")
	fmt.Printf("  发送包数: %d\n", result.SentPackets)
	fmt.Printf("  接收包数: %d\n", result.ReceivedPackets)
	fmt.Printf("  转发包数: %d\n", result.ForwardedPackets)
	fmt.Printf("  丢弃包数: %d\n", result.DroppedPackets)
	fmt.Printf("  缓冲包数: %d\n", result.BufferedPackets)
	fmt.Printf("  NACK发送: %d\n", result.NACKsSent)
	fmt.Printf("  乱序包数: %d\n", result.OutOfOrderPackets)
	fmt.Printf("  处理时间: %v\n", result.ProcessingTime)
	fmt.Printf("  吞吐量: %.2f 包/秒\n", result.ThroughputPPS)
	fmt.Printf("  转发率: %.2f%%\n", float64(result.ForwardedPackets)/float64(result.SentPackets)*100)

	return result
}

// 定义测试场景
func getStressTestScenarios() []StressTestConfig {
	return []StressTestConfig{
		{
			PacketCount:      1000,
			DropRate:         0.01, // 1% 丢包
			ReorderRate:      0.05, // 5% 乱序
			MaxReorderDelay:  5,
			StartSeq:         65530, // 从接近回绕点开始
			SSRC:             0x12345678,
			PacketInterval:   0,
			EnableWrapAround: true,
		},
		{
			PacketCount:      2000,
			DropRate:         0.02, // 2% 丢包
			ReorderRate:      0.10, // 10% 乱序
			MaxReorderDelay:  10,
			StartSeq:         65520,
			SSRC:             0x23456789,
			PacketInterval:   0,
			EnableWrapAround: true,
		},
		{
			PacketCount:      5000,
			DropRate:         0.05, // 5% 丢包
			ReorderRate:      0.15, // 15% 乱序
			MaxReorderDelay:  20,
			StartSeq:         65500,
			SSRC:             0x3456789A,
			PacketInterval:   0,
			EnableWrapAround: true,
		},
		{
			PacketCount:      10000,
			DropRate:         0.03, // 3% 丢包
			ReorderRate:      0.08, // 8% 乱序
			MaxReorderDelay:  15,
			StartSeq:         65000,
			SSRC:             0x456789AB,
			PacketInterval:   0,
			EnableWrapAround: true,
		},
	}
}

func main() {
	fmt.Println("=== RTP压力测试程序 ===")
	fmt.Println("测试序列号回绕、丢包和乱序处理性能")

	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	scenarios := getStressTestScenarios()
	results := make([]StressTestResult, 0, len(scenarios))

	for i, config := range scenarios {
		fmt.Printf("\n[%d/%d] ", i+1, len(scenarios))
		result := runStressTest(config)
		results = append(results, result)

		// 测试间等待
		time.Sleep(1 * time.Second)
	}

	// 输出汇总结果
	fmt.Printf("\n=== 压力测试汇总 ===\n")
	fmt.Printf("%-8s %-8s %-8s %-8s %-8s %-8s %-10s %-10s\n",
		"包数", "丢包率", "乱序率", "转发率", "NACK", "吞吐量", "处理时间", "状态")
	fmt.Printf("%s\n", strings.Repeat("-", 80))

	for _, result := range results {
		forwardRate := float64(result.ForwardedPackets) / float64(result.SentPackets) * 100
		status := "正常"
		if forwardRate < 90 {
			status = "异常"
		}

		fmt.Printf("%-8d %-8.1f %-8.1f %-8.1f %-8d %-10.0f %-10v %-10s\n",
			result.Config.PacketCount,
			result.Config.DropRate*100,
			result.Config.ReorderRate*100,
			forwardRate,
			result.NACKsSent,
			result.ThroughputPPS,
			result.ProcessingTime.Truncate(time.Millisecond),
			status)
	}

	// 性能分析
	fmt.Printf("\n=== 性能分析 ===\n")

	// 计算平均吞吐量
	var totalThroughput float64
	for _, result := range results {
		totalThroughput += result.ThroughputPPS
	}
	avgThroughput := totalThroughput / float64(len(results))
	fmt.Printf("平均吞吐量: %.2f 包/秒\n", avgThroughput)

	// 找出最高和最低吞吐量
	sort.Slice(results, func(i, j int) bool {
		return results[i].ThroughputPPS > results[j].ThroughputPPS
	})

	fmt.Printf("最高吞吐量: %.2f 包/秒 (包数: %d)\n",
		results[0].ThroughputPPS, results[0].Config.PacketCount)
	fmt.Printf("最低吞吐量: %.2f 包/秒 (包数: %d)\n",
		results[len(results)-1].ThroughputPPS, results[len(results)-1].Config.PacketCount)

	fmt.Printf("\n=== 测试完成 ===\n")
}

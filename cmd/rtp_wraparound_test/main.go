package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// TestScenario 测试场景
type TestScenario struct {
	Name            string
	Description     string
	Sequences       []uint16
	SSRC            uint32
	ExpectedResults string
}

// WrapAroundMockSender 回绕测试模拟发送器
type WrapAroundMockSender struct {
	logFile     *os.File
	sentPackets []SentPacket
}

type SentPacket struct {
	Timestamp  time.Time
	Interface  string
	Seq        uint16
	SSRC       uint32
	PacketType string
	Length     int
}

func (w *WrapAroundMockSender) SendPacket(data []byte, toInterface string) error {
	timestamp := time.Now()

	// 解析包信息
	var seq uint16
	var ssrc uint32
	var packetType string

	if len(data) >= 12 {
		version := (data[0] >> 6) & 0x3
		if version == 2 {
			payloadTypeField := data[1] & 0x7F
			seq = binary.BigEndian.Uint16(data[2:4])
			ssrc = binary.BigEndian.Uint32(data[8:12])

			if payloadTypeField >= 200 && payloadTypeField <= 204 {
				packetType = fmt.Sprintf("RTCP(PT=%d)", payloadTypeField)
			} else {
				packetType = fmt.Sprintf("RTP(PT=%d)", payloadTypeField)
			}
		}
	}

	// 记录发送的包
	sentPacket := SentPacket{
		Timestamp:  timestamp,
		Interface:  toInterface,
		Seq:        seq,
		SSRC:       ssrc,
		PacketType: packetType,
		Length:     len(data),
	}
	w.sentPackets = append(w.sentPackets, sentPacket)

	// 记录到日志文件
	if w.logFile != nil {
		fmt.Fprintf(w.logFile, "[%s] 发送到 %s: %s seq=%d SSRC=%08X 长度=%d\n",
			timestamp.Format("15:04:05.000"), toInterface, packetType, seq, ssrc, len(data))
	}

	return nil
}

func (w *WrapAroundMockSender) GetSentPackets() []SentPacket {
	return w.sentPackets
}

func (w *WrapAroundMockSender) Reset() {
	w.sentPackets = nil
}

// createTestRTPPacket 创建测试RTP包
func createTestRTPPacket(ssrc uint32, seq uint16, timestamp uint32, payloadType uint8, payload []byte) []byte {
	rtpHeader := make([]byte, 12)

	// RTP版本2，无填充，无扩展，无CSRC
	rtpHeader[0] = 0x80
	// 载荷类型
	rtpHeader[1] = payloadType
	// 序列号
	binary.BigEndian.PutUint16(rtpHeader[2:4], seq)
	// 时间戳
	binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
	// SSRC
	binary.BigEndian.PutUint32(rtpHeader[8:12], ssrc)

	// 组合头部和载荷
	result := make([]byte, len(rtpHeader)+len(payload))
	copy(result, rtpHeader)
	copy(result[len(rtpHeader):], payload)

	return result
}

func simulateUDPPacket(rtpData []byte, srcIP, dstIP string, srcPort, dstPort uint16) []byte {
	// 创建以太网层
	eth := &layers.Ethernet{
		SrcMAC:       []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}

	// 创建IP层
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    []byte{192, 168, 1, 100},
		DstIP:    []byte{192, 168, 1, 200},
	}

	// 创建UDP层
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(srcPort),
		DstPort: layers.UDPPort(dstPort),
	}
	udp.SetNetworkLayerForChecksum(ip)

	// 序列化包
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	if err := gopacket.SerializeLayers(buffer, opts, eth, ip, udp, gopacket.Payload(rtpData)); err != nil {
		log.Printf("序列化包失败: %v", err)
		return nil
	}

	return buffer.Bytes()
}

// 定义各种测试场景
func getTestScenarios() []TestScenario {
	return []TestScenario{
		{
			Name:            "正常回绕",
			Description:     "序列号从65535正常回绕到0",
			Sequences:       []uint16{65533, 65534, 65535, 0, 1, 2, 3},
			SSRC:            0x11111111,
			ExpectedResults: "所有包应该按顺序转发",
		},
		{
			Name:            "回绕时丢包",
			Description:     "在回绕点丢失包65535",
			Sequences:       []uint16{65533, 65534, 0, 1, 2, 3},
			SSRC:            0x22222222,
			ExpectedResults: "应该检测到65535丢失并发送NACK",
		},
		{
			Name:            "回绕时乱序",
			Description:     "回绕点附近包乱序到达",
			Sequences:       []uint16{65533, 65535, 65534, 1, 0, 2, 3},
			SSRC:            0x33333333,
			ExpectedResults: "应该重新排序并按正确顺序转发",
		},
		{
			Name:            "复杂回绕乱序",
			Description:     "回绕前后都有乱序和丢包",
			Sequences:       []uint16{65532, 65534, 65533, 1, 0, 3, 2, 5, 4},
			SSRC:            0x44444444,
			ExpectedResults: "应该检测到65535丢失，其他包重新排序",
		},
		{
			Name:            "多次回绕",
			Description:     "连续多次序列号回绕",
			Sequences:       []uint16{65534, 65535, 0, 1, 65535, 0, 1, 2},
			SSRC:            0x55555555,
			ExpectedResults: "应该正确处理重复的回绕序列",
		},
		{
			Name:            "大跳跃回绕",
			Description:     "序列号大幅跳跃导致的回绕",
			Sequences:       []uint16{100, 65535, 0, 1, 200},
			SSRC:            0x66666666,
			ExpectedResults: "应该检测到大量丢包并发送NACK",
		},
		{
			Name:            "反向回绕",
			Description:     "从小序列号跳到大序列号（反向回绕）",
			Sequences:       []uint16{1, 2, 65534, 65535, 3, 4},
			SSRC:            0x77777777,
			ExpectedResults: "应该识别为旧包或重复包",
		},
		{
			Name:            "极端乱序回绕",
			Description:     "回绕点极度乱序",
			Sequences:       []uint16{2, 65535, 1, 0, 65534, 3, 65533},
			SSRC:            0x88888888,
			ExpectedResults: "应该正确排序并检测丢包",
		},
	}
}

// runTestScenario 运行单个测试场景
func runTestScenario(scenario TestScenario, processor rtp.PacketProcessor, sender *WrapAroundMockSender) {
	fmt.Printf("\n=== 测试场景: %s ===\n", scenario.Name)
	fmt.Printf("描述: %s\n", scenario.Description)
	fmt.Printf("输入序列: %v\n", scenario.Sequences)
	fmt.Printf("期望结果: %s\n", scenario.ExpectedResults)

	// 重置发送器
	sender.Reset()

	// 创建测试数据
	payload := []byte(fmt.Sprintf("Test data for %s", scenario.Name))
	baseTimestamp := uint32(1000)

	fmt.Printf("\n处理包序列:\n")

	for i, seq := range scenario.Sequences {
		timestamp := baseTimestamp + uint32(seq*160)
		rtpData := createTestRTPPacket(scenario.SSRC, seq, timestamp, 96, payload)

		// 模拟UDP包
		udpPacket := simulateUDPPacket(rtpData, "192.168.1.200", "192.168.1.100", 5004, 5004)
		if udpPacket == nil {
			continue
		}

		// 解析包
		packet := gopacket.NewPacket(udpPacket, layers.LayerTypeEthernet, gopacket.Default)

		// 处理包（模拟远端包）
		shouldForward, err := processor.ProcessPacket(packet, false)
		if err != nil {
			log.Printf("处理包失败: %v", err)
			continue
		}

		fmt.Printf("  包 #%d: seq=%d, 转发=%v\n", i+1, seq, shouldForward)

		// 添加延迟模拟网络传输
		time.Sleep(20 * time.Millisecond)
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	// 分析结果
	fmt.Printf("\n结果分析:\n")
	sentPackets := sender.GetSentPackets()

	// 统计发送的包
	rtpCount := 0
	rtcpCount := 0
	nackCount := 0

	fmt.Printf("发送的包:\n")
	for _, sent := range sentPackets {
		fmt.Printf("  → %s: %s seq=%d\n", sent.Interface, sent.PacketType, sent.Seq)

		if len(sent.PacketType) >= 3 && sent.PacketType[:3] == "RTP" {
			rtpCount++
		} else if len(sent.PacketType) >= 4 && sent.PacketType[:4] == "RTCP" {
			rtcpCount++
			nackCount++
		}
	}

	fmt.Printf("统计: RTP包=%d, RTCP/NACK包=%d\n", rtpCount, nackCount)

	// 获取处理器统计
	stats := processor.GetStats()
	fmt.Printf("处理器统计: 接收=%d, 转发=%d, 缓冲=%d, 丢弃=%d, 乱序=%d, NACK发送=%d\n",
		stats.FarEndStats.ReceivedPackets,
		stats.FarEndStats.ForwardedPackets,
		stats.FarEndStats.BufferedPackets,
		stats.FarEndStats.DroppedPackets,
		stats.FarEndStats.OutOfOrderPackets,
		stats.FarEndStats.NACKsSent)
}

func main() {
	fmt.Println("=== RTP序列号回绕和乱序测试程序 ===")

	// 创建日志文件
	logFile, err := os.Create("wraparound_test.log")
	if err != nil {
		log.Fatalf("创建日志文件失败: %v", err)
	}
	defer logFile.Close()

	// 创建RTP处理器
	fmt.Println("\n初始化RTP处理器...")
	config := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   100 * time.Millisecond, // 较短的缓冲时间以便观察效果
		NACKTimeout:      50 * time.Millisecond,  // 较短的NACK超时
		Debug:            true,
	}

	processor := rtp.NewPacketProcessor(config)

	// 设置包发送器
	sender := &WrapAroundMockSender{
		logFile: logFile,
	}
	processor.SetPacketSender(sender)

	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 运行所有测试场景
	scenarios := getTestScenarios()

	for _, scenario := range scenarios {
		runTestScenario(scenario, processor, sender)

		// 场景间等待，让处理器完成所有操作
		time.Sleep(500 * time.Millisecond)
		fmt.Println("\n" + strings.Repeat("-", 80))
	}

	// 最终统计
	fmt.Printf("\n=== 最终统计信息 ===\n")
	finalStats := processor.GetStats()
	fmt.Printf("总体统计:\n")
	fmt.Printf("  总UDP包: %d\n", finalStats.TotalUDPPackets)
	fmt.Printf("  总RTP包: %d\n", finalStats.TotalRTPPackets)
	fmt.Printf("  总RTCP包: %d\n", finalStats.TotalRTCPPackets)
	fmt.Printf("  非RTP包: %d\n", finalStats.TotalNonRTPPackets)

	fmt.Printf("\n远端统计:\n")
	fmt.Printf("  接收包: %d\n", finalStats.FarEndStats.ReceivedPackets)
	fmt.Printf("  转发包: %d\n", finalStats.FarEndStats.ForwardedPackets)
	fmt.Printf("  缓冲包: %d\n", finalStats.FarEndStats.BufferedPackets)
	fmt.Printf("  丢弃包: %d\n", finalStats.FarEndStats.DroppedPackets)
	fmt.Printf("  乱序包: %d\n", finalStats.FarEndStats.OutOfOrderPackets)
	fmt.Printf("  发送NACK: %d\n", finalStats.FarEndStats.NACKsSent)
	fmt.Printf("  接收NACK: %d\n", finalStats.FarEndStats.NACKsReceived)

	fmt.Printf("\n活跃会话: %d\n", len(finalStats.ActiveSessions))
	for ssrc, session := range finalStats.ActiveSessions {
		fmt.Printf("  SSRC 0x%08X: 最后活跃 %v\n", ssrc, session.LastSeen.Format("15:04:05"))
		if session.SeqWindow != nil {
			fmt.Printf("    序列号窗口: Base=%d, Max=%d, Mask=0x%08X\n",
				session.SeqWindow.BaseSeq, session.SeqWindow.MaxSeq, session.SeqWindow.ReceivedMask)
		}
	}

	fmt.Printf("\n=== 测试完成 ===\n")
	fmt.Printf("详细日志已保存到: wraparound_test.log\n")
}

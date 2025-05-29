package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// ExtremeTestCase 极端测试用例
type ExtremeTestCase struct {
	Name        string
	Description string
	Generator   func() []uint16
	SSRC        uint32
	Expected    string
}

// ExtremeSender 极端测试发送器
type ExtremeSender struct {
	sentPackets []SentPacket
	rtpCount    int
	nackCount   int
}

type SentPacket struct {
	Seq       uint16
	Interface string
	Type      string
	Timestamp time.Time
}

func (s *ExtremeSender) SendPacket(data []byte, toInterface string) error {
	if len(data) >= 12 {
		seq := binary.BigEndian.Uint16(data[2:4])
		version := (data[0] >> 6) & 0x3
		if version == 2 {
			payloadType := data[1] & 0x7F
			packetType := "RTP"
			if payloadType >= 200 && payloadType <= 204 {
				packetType = "RTCP/NACK"
				s.nackCount++
			} else {
				s.rtpCount++
			}
			s.sentPackets = append(s.sentPackets, SentPacket{
				Seq:       seq,
				Interface: toInterface,
				Type:      packetType,
				Timestamp: time.Now(),
			})
		}
	}
	return nil
}

func (s *ExtremeSender) GetStats() (int, int) {
	return s.rtpCount, s.nackCount
}

func (s *ExtremeSender) Reset() {
	s.sentPackets = nil
	s.rtpCount = 0
	s.nackCount = 0
}

// createRTPPacket 创建RTP包
func createRTPPacket(ssrc uint32, seq uint16, timestamp uint32) []byte {
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80 // Version 2
	rtpHeader[1] = 96   // Payload Type
	binary.BigEndian.PutUint16(rtpHeader[2:4], seq)
	binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
	binary.BigEndian.PutUint32(rtpHeader[8:12], ssrc)

	payload := []byte("Extreme test payload data")
	result := make([]byte, len(rtpHeader)+len(payload))
	copy(result, rtpHeader)
	copy(result[len(rtpHeader):], payload)

	return result
}

func createUDPPacket(rtpData []byte) []byte {
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

// 极端测试用例生成器

// generateHighDropRate 生成高丢包率序列
func generateHighDropRate() []uint16 {
	sequences := make([]uint16, 0, 100)
	for i := uint16(65530); i < 65535; i++ {
		sequences = append(sequences, i)
	}
	for i := uint16(0); i < 50; i++ {
		sequences = append(sequences, i)
	}

	// 50%丢包率
	result := make([]uint16, 0, len(sequences)/2)
	for i, seq := range sequences {
		if i%2 == 0 { // 每隔一个包丢弃
			result = append(result, seq)
		}
	}
	return result
}

// generateMassiveReorder 生成大量乱序序列
func generateMassiveReorder() []uint16 {
	sequences := []uint16{65533, 65534, 65535, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// 完全打乱顺序
	rand.Seed(time.Now().UnixNano())
	for i := len(sequences) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		sequences[i], sequences[j] = sequences[j], sequences[i]
	}

	return sequences
}

// generateContinuousWrapAround 生成连续回绕序列
func generateContinuousWrapAround() []uint16 {
	sequences := make([]uint16, 0, 20)

	// 第一次回绕
	sequences = append(sequences, 65534, 65535, 0, 1)
	// 第二次回绕（跳跃）
	sequences = append(sequences, 65533, 65534, 65535, 0, 1, 2)
	// 第三次回绕（乱序）
	sequences = append(sequences, 0, 65535, 1, 65534, 2, 3)

	return sequences
}

// generateDuplicateFlood 生成重复包洪水
func generateDuplicateFlood() []uint16 {
	sequences := make([]uint16, 0, 50)

	// 正常序列
	base := []uint16{65534, 65535, 0, 1, 2}
	sequences = append(sequences, base...)

	// 大量重复
	for i := 0; i < 10; i++ {
		sequences = append(sequences, base...)
	}

	return sequences
}

// generateBurstLoss 生成突发丢包序列
func generateBurstLoss() []uint16 {
	sequences := make([]uint16, 0, 30)

	// 正常开始
	sequences = append(sequences, 65530, 65531, 65532)
	// 突发丢包（丢失65533-65535和0-9）
	sequences = append(sequences, 10, 11, 12, 13, 14, 15)

	return sequences
}

// generateJitterPattern 生成抖动模式序列
func generateJitterPattern() []uint16 {
	// 模拟网络抖动：包到达顺序完全随机
	base := []uint16{65532, 65533, 65534, 65535, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// 创建随机延迟模式
	delayed := make([]uint16, len(base))
	copy(delayed, base)

	// 随机交换
	rand.Seed(time.Now().UnixNano() + 1)
	for i := 0; i < len(delayed)*2; i++ {
		a := rand.Intn(len(delayed))
		b := rand.Intn(len(delayed))
		delayed[a], delayed[b] = delayed[b], delayed[a]
	}

	return delayed
}

// generateRapidWrapAround 生成快速回绕序列
func generateRapidWrapAround() []uint16 {
	sequences := make([]uint16, 0, 30)

	// 快速连续回绕
	for cycle := 0; cycle < 3; cycle++ {
		start := uint16(65533 + cycle)
		sequences = append(sequences, start, start+1, start+2)
		if start+2 > 65535 {
			// 处理回绕
			wrapEnd := int(start+2) - 65536
			for i := uint16(0); i <= uint16(wrapEnd); i++ {
				sequences = append(sequences, i)
			}
		}
	}

	return sequences
}

// runExtremeTest 运行极端测试
func runExtremeTest(testCase ExtremeTestCase) {
	fmt.Printf("\n=== %s ===\n", testCase.Name)
	fmt.Printf("描述: %s\n", testCase.Description)

	sequences := testCase.Generator()
	fmt.Printf("生成序列长度: %d\n", len(sequences))
	fmt.Printf("序列: %v\n", sequences)
	fmt.Printf("期望结果: %s\n", testCase.Expected)

	// 创建RTP处理器
	config := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   100 * time.Millisecond, // 增加缓冲时间
		NACKTimeout:      30 * time.Millisecond,
		Debug:            false, // 关闭详细调试以减少输出
	}

	processor := rtp.NewPacketProcessor(config)
	sender := &ExtremeSender{}
	processor.SetPacketSender(sender)

	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 发送包序列
	baseTimestamp := uint32(10000)
	startTime := time.Now()

	for i, seq := range sequences {
		timestamp := baseTimestamp + uint32(seq*160)
		rtpData := createRTPPacket(testCase.SSRC, seq, timestamp)
		udpPacket := createUDPPacket(rtpData)

		if udpPacket != nil {
			packet := gopacket.NewPacket(udpPacket, layers.LayerTypeEthernet, gopacket.Default)

			// 处理包（模拟远端包）
			shouldForward, err := processor.ProcessPacket(packet, false)
			if err != nil {
				log.Printf("处理包失败: %v", err)
				continue
			}

			if i < 10 || i%10 == 0 { // 只显示前10个和每10个包的状态
				fmt.Printf("  包 #%d: seq=%d, 转发=%t\n", i+1, seq, shouldForward)
			}
		}

		// 模拟网络延迟
		time.Sleep(2 * time.Millisecond)
	}

	// 等待处理完成
	time.Sleep(200 * time.Millisecond)

	processingTime := time.Since(startTime)

	// 分析结果
	rtpCount, nackCount := sender.GetStats()

	fmt.Printf("\n结果分析:\n")
	fmt.Printf("  输入包数: %d\n", len(sequences))
	fmt.Printf("  转发RTP包: %d\n", rtpCount)
	fmt.Printf("  发送NACK: %d\n", nackCount)
	fmt.Printf("  处理时间: %v\n", processingTime)

	// 获取处理器统计
	stats := processor.GetStats()
	fmt.Printf("  接收包数: %d\n", stats.FarEndStats.ReceivedPackets)
	fmt.Printf("  转发包数: %d\n", stats.FarEndStats.ForwardedPackets)
	fmt.Printf("  缓冲包数: %d\n", stats.FarEndStats.BufferedPackets)
	fmt.Printf("  丢弃包数: %d\n", stats.FarEndStats.DroppedPackets)
	fmt.Printf("  NACK发送: %d\n", stats.FarEndStats.NACKsSent)

	// 计算效率
	if len(sequences) > 0 {
		forwardRate := float64(stats.FarEndStats.ForwardedPackets) / float64(len(sequences)) * 100
		fmt.Printf("  转发率: %.1f%%\n", forwardRate)
	}

	fmt.Printf("%s\n", "--------------------------------------------------------------------------------")
}

// getExtremeTestCases 获取极端测试用例
func getExtremeTestCases() []ExtremeTestCase {
	return []ExtremeTestCase{
		{
			Name:        "高丢包率测试",
			Description: "50%丢包率的回绕序列",
			Generator:   generateHighDropRate,
			SSRC:        0x11111111,
			Expected:    "应该检测到大量丢包并发送NACK",
		},
		{
			Name:        "大量乱序测试",
			Description: "完全打乱的回绕序列",
			Generator:   generateMassiveReorder,
			SSRC:        0x22222222,
			Expected:    "应该重新排序并正确转发",
		},
		{
			Name:        "连续回绕测试",
			Description: "连续多次回绕的复杂序列",
			Generator:   generateContinuousWrapAround,
			SSRC:        0x33333333,
			Expected:    "应该正确处理多次回绕",
		},
		{
			Name:        "重复包洪水测试",
			Description: "大量重复包的攻击模式",
			Generator:   generateDuplicateFlood,
			SSRC:        0x44444444,
			Expected:    "应该识别并丢弃重复包",
		},
		{
			Name:        "突发丢包测试",
			Description: "连续大量包丢失的极端情况",
			Generator:   generateBurstLoss,
			SSRC:        0x55555555,
			Expected:    "应该检测到突发丢包并发送NACK",
		},
		{
			Name:        "网络抖动测试",
			Description: "模拟严重网络抖动的随机到达",
			Generator:   generateJitterPattern,
			SSRC:        0x66666666,
			Expected:    "应该缓冲并重新排序包",
		},
		{
			Name:        "快速回绕测试",
			Description: "快速连续的序列号回绕",
			Generator:   generateRapidWrapAround,
			SSRC:        0x77777777,
			Expected:    "应该正确处理快速回绕",
		},
	}
}

func main() {
	fmt.Println("=== RTP极端情况测试程序 ===")
	fmt.Println("测试各种极端网络条件下的RTP处理能力")

	testCases := getExtremeTestCases()

	for i, testCase := range testCases {
		fmt.Printf("\n[%d/%d] ", i+1, len(testCases))
		runExtremeTest(testCase)

		// 测试间等待
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n=== 极端测试完成 ===\n")
	fmt.Println("所有极端情况已测试完毕")
	fmt.Println("RTP处理器在各种极端条件下的表现已评估")
}

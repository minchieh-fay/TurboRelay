package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// BoundaryTestCase 边界测试用例
type BoundaryTestCase struct {
	Name        string
	Description string
	Sequences   []uint16
	SSRC        uint32
	Expected    string
}

// SimpleSender 简单发送器
type SimpleSender struct {
	sentPackets []SentPacket
}

type SentPacket struct {
	Seq       uint16
	Interface string
	Type      string
}

func (s *SimpleSender) SendPacket(data []byte, toInterface string) error {
	if len(data) >= 12 {
		seq := binary.BigEndian.Uint16(data[2:4])
		version := (data[0] >> 6) & 0x3
		if version == 2 {
			payloadType := data[1] & 0x7F
			packetType := "RTP"
			if payloadType >= 200 && payloadType <= 204 {
				packetType = "RTCP/NACK"
			}
			s.sentPackets = append(s.sentPackets, SentPacket{
				Seq:       seq,
				Interface: toInterface,
				Type:      packetType,
			})
		}
	}
	return nil
}

func (s *SimpleSender) GetSentPackets() []SentPacket {
	return s.sentPackets
}

func (s *SimpleSender) Reset() {
	s.sentPackets = nil
}

// createRTPPacket 创建RTP包
func createRTPPacket(ssrc uint32, seq uint16, timestamp uint32) []byte {
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80 // Version 2
	rtpHeader[1] = 96   // Payload Type
	binary.BigEndian.PutUint16(rtpHeader[2:4], seq)
	binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
	binary.BigEndian.PutUint32(rtpHeader[8:12], ssrc)

	payload := []byte("Test RTP payload")
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

// runBoundaryTest 运行边界测试
func runBoundaryTest(testCase BoundaryTestCase) {
	fmt.Printf("\n=== %s ===\n", testCase.Name)
	fmt.Printf("描述: %s\n", testCase.Description)
	fmt.Printf("序列号: %v\n", testCase.Sequences)
	fmt.Printf("期望结果: %s\n", testCase.Expected)

	// 创建RTP处理器
	config := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   50 * time.Millisecond,
		NACKTimeout:      20 * time.Millisecond,
		Debug:            true, // 启用调试
	}

	processor := rtp.NewPacketProcessor(config)
	sender := &SimpleSender{}
	processor.SetPacketSender(sender)

	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 发送包序列
	baseTimestamp := uint32(1000)
	for i, seq := range testCase.Sequences {
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

			fmt.Printf("  包 #%d: seq=%d, 转发=%t\n", i+1, seq, shouldForward)
		}

		// 短暂延迟
		time.Sleep(5 * time.Millisecond)
	}

	// 等待NACK超时
	time.Sleep(100 * time.Millisecond)

	// 分析结果
	sentPackets := sender.GetSentPackets()
	fmt.Printf("\n发送的包:\n")
	rtpCount := 0
	nackCount := 0

	for _, sent := range sentPackets {
		fmt.Printf("  → %s: %s seq=%d\n", sent.Interface, sent.Type, sent.Seq)
		if sent.Type == "RTP" {
			rtpCount++
		} else if sent.Type == "RTCP/NACK" {
			nackCount++
		}
	}

	fmt.Printf("\n统计: RTP包=%d, NACK包=%d\n", rtpCount, nackCount)

	// 获取处理器统计
	stats := processor.GetStats()
	fmt.Printf("处理器统计: 接收=%d, 转发=%d, 缓冲=%d, 丢弃=%d, NACK发送=%d\n",
		stats.FarEndStats.ReceivedPackets,
		stats.FarEndStats.ForwardedPackets,
		stats.FarEndStats.BufferedPackets,
		stats.FarEndStats.DroppedPackets,
		stats.FarEndStats.NACKsSent)

	fmt.Printf("%s\n", "--------------------------------------------------------------------------------")
}

// getBoundaryTestCases 获取边界测试用例
func getBoundaryTestCases() []BoundaryTestCase {
	return []BoundaryTestCase{
		{
			Name:        "精确回绕点",
			Description: "序列号从65535精确回绕到0",
			Sequences:   []uint16{65534, 65535, 0, 1, 2},
			SSRC:        0x11111111,
			Expected:    "所有包应该按顺序转发",
		},
		{
			Name:        "回绕点丢包",
			Description: "在回绕点丢失关键包",
			Sequences:   []uint16{65534, 0, 1, 2}, // 丢失65535
			SSRC:        0x22222222,
			Expected:    "应该检测到65535丢失并发送NACK",
		},
		{
			Name:        "回绕点乱序",
			Description: "回绕点包乱序到达",
			Sequences:   []uint16{65534, 0, 65535, 1, 2}, // 65535延迟到达
			SSRC:        0x33333333,
			Expected:    "应该重新排序并正确转发",
		},
		{
			Name:        "多重回绕",
			Description: "连续多次回绕",
			Sequences:   []uint16{65534, 65535, 0, 1, 65535, 0, 1, 2}, // 重复回绕
			SSRC:        0x44444444,
			Expected:    "应该识别重复包并正确处理",
		},
		{
			Name:        "大间隔回绕",
			Description: "大序列号间隔的回绕",
			Sequences:   []uint16{65530, 65535, 0, 5}, // 跳跃式回绕
			SSRC:        0x55555555,
			Expected:    "应该检测到中间丢包并发送NACK",
		},
		{
			Name:        "反向跳跃",
			Description: "从小序列号跳到大序列号",
			Sequences:   []uint16{10, 65530, 11, 12}, // 反向跳跃
			SSRC:        0x66666666,
			Expected:    "应该识别为旧包或窗口扩展",
		},
		{
			Name:        "极端乱序回绕",
			Description: "回绕点极度乱序",
			Sequences:   []uint16{2, 65535, 1, 0, 65534, 3}, // 极度乱序
			SSRC:        0x77777777,
			Expected:    "应该正确排序并检测丢包",
		},
		{
			Name:        "回绕边界重复",
			Description: "回绕边界的重复包",
			Sequences:   []uint16{65535, 0, 65535, 0, 1}, // 重复边界包
			SSRC:        0x88888888,
			Expected:    "应该识别重复包并丢弃",
		},
	}
}

func main() {
	fmt.Println("=== RTP序列号回绕边界测试程序 ===")
	fmt.Println("专门测试序列号在65535附近的各种边界情况")

	testCases := getBoundaryTestCases()

	for i, testCase := range testCases {
		fmt.Printf("\n[%d/%d] ", i+1, len(testCases))
		runBoundaryTest(testCase)

		// 测试间等待
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n=== 边界测试完成 ===\n")
	fmt.Println("所有回绕边界情况已测试完毕")
}

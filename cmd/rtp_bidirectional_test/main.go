package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// BidirectionalMockSender 双向模拟发送器
type BidirectionalMockSender struct {
	nearFile *os.File
	farFile  *os.File
	logFile  *os.File
}

func (b *BidirectionalMockSender) SendPacket(data []byte, toInterface string) error {
	timestamp := time.Now().Format("15:04:05.000")

	// 选择输出文件
	var targetFile *os.File
	if toInterface == "near" {
		targetFile = b.nearFile
	} else {
		targetFile = b.farFile
	}

	// 记录日志
	if b.logFile != nil {
		fmt.Fprintf(b.logFile, "[%s] 发送到 %s，长度: %d", timestamp, toInterface, len(data))

		// 解析包类型
		if len(data) >= 12 {
			version := (data[0] >> 6) & 0x3
			if version == 2 {
				payloadType := data[1] & 0x7F
				seq := binary.BigEndian.Uint16(data[2:4])
				ssrc := binary.BigEndian.Uint32(data[8:12])

				if payloadType >= 200 && payloadType <= 204 {
					fmt.Fprintf(b.logFile, " [RTCP PT=%d SSRC=%08X]", payloadType, ssrc)
				} else {
					fmt.Fprintf(b.logFile, " [RTP seq=%d PT=%d SSRC=%08X]", seq, payloadType, ssrc)
				}
			}
		}
		fmt.Fprintln(b.logFile)
	}

	// 写入文件
	if targetFile != nil {
		lengthBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
		targetFile.Write(lengthBytes)
		targetFile.Write(data)
	}

	return nil
}

// createBidirectionalTestFiles 创建双向测试文件
func createBidirectionalTestFiles() error {
	// 创建近端到远端的流（音频）
	if err := createDirectionalFile("test_near_to_far.rtp", 0x11111111, 96, []uint16{1, 2, 4, 3, 5, 7, 6, 8, 10, 9}); err != nil {
		return fmt.Errorf("创建近端到远端文件失败: %v", err)
	}

	// 创建远端到近端的流（视频）
	if err := createDirectionalFile("test_far_to_near.rtp", 0x22222222, 97, []uint16{101, 102, 103, 105, 104, 107, 106, 108, 110, 109}); err != nil {
		return fmt.Errorf("创建远端到近端文件失败: %v", err)
	}

	return nil
}

func createDirectionalFile(filename string, ssrc uint32, payloadType uint8, sequences []uint16) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	baseTimestamp := uint32(1000)
	payload := []byte("Test RTP Data for bidirectional test")

	fmt.Printf("创建 %s，SSRC=%08X，序列: %v\n", filename, ssrc, sequences)

	for _, seq := range sequences {
		timestamp := baseTimestamp + uint32(seq*160)
		rtpData := createTestRTPPacket(ssrc, seq, timestamp, payloadType, payload)

		if err := writePacketToFile(file, rtpData); err != nil {
			return fmt.Errorf("写入包 seq=%d 失败: %v", seq, err)
		}
	}

	return nil
}

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

func writePacketToFile(file *os.File, data []byte) error {
	// 写入包长度（4字节小端序）
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
	if _, err := file.Write(lengthBytes); err != nil {
		return err
	}

	// 写入包数据
	if _, err := file.Write(data); err != nil {
		return err
	}

	return nil
}

func readPacketFromFile(file *os.File) ([]byte, error) {
	// 读取包长度
	lengthBytes := make([]byte, 4)
	if _, err := file.Read(lengthBytes); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(lengthBytes)
	if length > 1500 {
		return nil, fmt.Errorf("包长度异常: %d", length)
	}

	// 读取包数据
	data := make([]byte, length)
	if _, err := file.Read(data); err != nil {
		return nil, err
	}

	return data, nil
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

func main() {
	fmt.Println("=== 双向RTP流测试程序 ===")

	// 创建测试文件
	fmt.Println("\n1. 创建双向测试文件...")
	if err := createBidirectionalTestFiles(); err != nil {
		log.Fatalf("创建测试文件失败: %v", err)
	}

	// 创建输出文件
	nearFile, err := os.Create("output_to_near.rtp")
	if err != nil {
		log.Fatalf("创建近端输出文件失败: %v", err)
	}
	defer nearFile.Close()

	farFile, err := os.Create("output_to_far.rtp")
	if err != nil {
		log.Fatalf("创建远端输出文件失败: %v", err)
	}
	defer farFile.Close()

	logFile, err := os.Create("bidirectional_test.log")
	if err != nil {
		log.Fatalf("创建日志文件失败: %v", err)
	}
	defer logFile.Close()

	// 创建RTP处理器
	fmt.Println("\n2. 初始化RTP处理器...")
	config := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   200 * time.Millisecond,
		NACKTimeout:      30 * time.Millisecond,
		Debug:            true,
	}

	processor := rtp.NewPacketProcessor(config)

	// 设置包发送器
	sender := &BidirectionalMockSender{
		nearFile: nearFile,
		farFile:  farFile,
		logFile:  logFile,
	}
	processor.SetPacketSender(sender)

	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 处理近端到远端的流
	fmt.Println("\n3. 处理近端到远端的RTP流...")
	if err := processDirectionalFlow("test_near_to_far.rtp", processor, true); err != nil {
		log.Printf("处理近端流失败: %v", err)
	}

	// 等待一段时间让NACK处理完成
	time.Sleep(100 * time.Millisecond)

	// 处理远端到近端的流
	fmt.Println("\n4. 处理远端到近端的RTP流...")
	if err := processDirectionalFlow("test_far_to_near.rtp", processor, false); err != nil {
		log.Printf("处理远端流失败: %v", err)
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)

	// 显示统计信息
	fmt.Println("\n5. 处理统计信息:")
	stats := processor.GetStats()
	fmt.Printf("总UDP包数: %d\n", stats.TotalUDPPackets)
	fmt.Printf("RTP包数: %d\n", stats.TotalRTPPackets)
	fmt.Printf("RTCP包数: %d\n", stats.TotalRTCPPackets)
	fmt.Printf("非RTP包数: %d\n", stats.TotalNonRTPPackets)
	fmt.Printf("近端转发包数: %d\n", stats.NearEndStats.ForwardedPackets)
	fmt.Printf("远端转发包数: %d\n", stats.FarEndStats.ForwardedPackets)
	fmt.Printf("近端NACK发送数: %d\n", stats.NearEndStats.NACKsSent)
	fmt.Printf("远端NACK发送数: %d\n", stats.FarEndStats.NACKsSent)
	fmt.Printf("活跃会话数: %d\n", len(stats.ActiveSessions))

	fmt.Println("\n=== 测试完成 ===")
	fmt.Println("输出文件:")
	fmt.Println("  - output_to_near.rtp (发送到近端的包)")
	fmt.Println("  - output_to_far.rtp (发送到远端的包)")
	fmt.Println("  - bidirectional_test.log (详细日志)")
}

func processDirectionalFlow(filename string, processor rtp.PacketProcessor, isFromNear bool) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	direction := "远端到近端"
	if isFromNear {
		direction = "近端到远端"
	}

	fmt.Printf("处理 %s 流...", direction)

	packetCount := 0
	for {
		rtpData, err := readPacketFromFile(file)
		if err != nil {
			break // 文件结束
		}

		packetCount++

		// 模拟UDP包
		var udpPacket []byte
		if isFromNear {
			udpPacket = simulateUDPPacket(rtpData, "192.168.1.100", "192.168.1.200", 5004, 5004)
		} else {
			udpPacket = simulateUDPPacket(rtpData, "192.168.1.200", "192.168.1.100", 5004, 5004)
		}

		if udpPacket == nil {
			continue
		}

		// 解析包
		packet := gopacket.NewPacket(udpPacket, layers.LayerTypeEthernet, gopacket.Default)

		// 处理包
		shouldForward, err := processor.ProcessPacket(packet, isFromNear)
		if err != nil {
			log.Printf("处理包失败: %v", err)
			continue
		}

		if packetCount <= 5 {
			seq := binary.BigEndian.Uint16(rtpData[2:4])
			fmt.Printf("  包 #%d: seq=%d, 转发=%v\n", packetCount, seq, shouldForward)
		}

		// 添加一些延迟模拟真实网络
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Printf("完成，共处理 %d 个包\n", packetCount)
	return nil
}

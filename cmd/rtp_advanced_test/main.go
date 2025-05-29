package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
	"turborelay/rtp"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// MockPacketSender 模拟包发送器，将数据写入文件
type MockPacketSender struct {
	outputFile *os.File
	logFile    *os.File
}

func (m *MockPacketSender) SendPacket(data []byte, toInterface string) error {
	// 记录发送日志
	if m.logFile != nil {
		timestamp := time.Now().Format("15:04:05.000")
		fmt.Fprintf(m.logFile, "[%s] 发送包到 %s，长度: %d\n", timestamp, toInterface, len(data))

		// 如果是RTP包，解析序列号
		if len(data) >= 12 {
			seq := binary.BigEndian.Uint16(data[2:4])
			fmt.Fprintf(m.logFile, "  → RTP seq=%d\n", seq)
		}
	}

	// 写入包长度（4字节）+ 包数据
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))

	if _, err := m.outputFile.Write(lengthBytes); err != nil {
		return err
	}

	if _, err := m.outputFile.Write(data); err != nil {
		return err
	}

	fmt.Printf("发送包到 %s，长度: %d\n", toInterface, len(data))
	return nil
}

// createTestRTPPacket 创建测试RTP包
func createTestRTPPacket(ssrc uint32, seq uint16, timestamp uint32, payload []byte) []byte {
	// RTP头部 (12字节)
	header := make([]byte, 12)

	// Version(2) + Padding(1) + Extension(1) + CSRC Count(4) = 0x80
	header[0] = 0x80

	// Marker(1) + Payload Type(7) = 0x60 (假设payload type = 96)
	header[1] = 0x60

	// Sequence Number (2字节)
	binary.BigEndian.PutUint16(header[2:4], seq)

	// Timestamp (4字节)
	binary.BigEndian.PutUint32(header[4:8], timestamp)

	// SSRC (4字节)
	binary.BigEndian.PutUint32(header[8:12], ssrc)

	// 组合头部和载荷
	packet := append(header, payload...)
	return packet
}

// createMockUDPPacket 创建模拟的UDP包（包含RTP数据）
func createMockUDPPacket(rtpData []byte, srcIP, dstIP net.IP, srcPort, dstPort uint16) gopacket.Packet {
	// 创建以太网层
	ethLayer := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}

	// 创建IP层
	ipLayer := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP,
		DstIP:    dstIP,
	}

	// 创建UDP层
	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(srcPort),
		DstPort: layers.UDPPort(dstPort),
	}
	udpLayer.SetNetworkLayerForChecksum(ipLayer)

	// 序列化包
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	err := gopacket.SerializeLayers(buffer, opts,
		ethLayer,
		ipLayer,
		udpLayer,
		gopacket.Payload(rtpData),
	)

	if err != nil {
		log.Printf("序列化包失败: %v", err)
		return nil
	}

	// 解析包
	packet := gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	return packet
}

// writePacketToFile 将包写入文件（包长度 + 包数据）
func writePacketToFile(file *os.File, packet []byte) error {
	// 写入包长度（4字节）
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(packet)))

	if _, err := file.Write(lengthBytes); err != nil {
		return err
	}

	// 写入包数据
	if _, err := file.Write(packet); err != nil {
		return err
	}

	return nil
}

// readPacketFromFile 从文件读取包（包长度 + 包数据）
func readPacketFromFile(file *os.File) ([]byte, error) {
	// 读取包长度（4字节）
	lengthBytes := make([]byte, 4)
	if _, err := file.Read(lengthBytes); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(lengthBytes)
	if length == 0 || length > 65536 { // 合理性检查
		return nil, fmt.Errorf("无效的包长度: %d", length)
	}

	// 读取包数据
	packet := make([]byte, length)
	if _, err := file.Read(packet); err != nil {
		return nil, err
	}

	return packet, nil
}

// createTestInputFile 创建测试输入文件，包含乱序和丢包的RTP包
func createTestInputFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	ssrc := uint32(0x12345678)
	baseTimestamp := uint32(1000)
	payload := []byte("Hello RTP Test Data")

	// 创建更复杂的乱序和丢包场景: 1, 2, 3, 5, 4, 7, 6, 8, 10, 12, 11, 13
	// 缺少序列号 9
	sequences := []uint16{1, 2, 3, 5, 4, 7, 6, 8, 10, 12, 11, 13}

	fmt.Printf("创建测试输入文件，包含乱序和丢包的RTP包: %v\n", sequences)
	fmt.Printf("注意：序列号 9 被故意丢弃以测试NACK机制\n")

	for _, seq := range sequences {
		timestamp := baseTimestamp + uint32(seq*160) // 使用序列号计算时间戳
		rtpData := createTestRTPPacket(ssrc, seq, timestamp, payload)

		if err := writePacketToFile(file, rtpData); err != nil {
			return fmt.Errorf("写入包 seq=%d 失败: %v", seq, err)
		}

		fmt.Printf("写入RTP包: seq=%d, timestamp=%d\n", seq, timestamp)
	}

	return nil
}

// verifyOutputFile 验证输出文件中的包顺序
func verifyOutputFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Printf("\n=== 验证输出文件包顺序 ===\n")

	var lastSeq uint16 = 0
	packetCount := 0
	missingSeqs := []uint16{}

	for {
		packetData, err := readPacketFromFile(file)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if len(packetData) >= 12 {
			seq := binary.BigEndian.Uint16(packetData[2:4])
			packetCount++

			fmt.Printf("包 #%d: seq=%d", packetCount, seq)

			if packetCount > 1 {
				if seq == lastSeq+1 {
					fmt.Printf(" ✓ 顺序正确")
				} else {
					fmt.Printf(" ✗ 顺序错误 (期望: %d)", lastSeq+1)
					// 记录缺失的序列号
					for missing := lastSeq + 1; missing < seq; missing++ {
						missingSeqs = append(missingSeqs, missing)
					}
				}
			}
			fmt.Println()

			lastSeq = seq
		}
	}

	fmt.Printf("总共验证了 %d 个包\n", packetCount)
	if len(missingSeqs) > 0 {
		fmt.Printf("缺失的序列号: %v\n", missingSeqs)
	}
	return nil
}

// testAdvancedRTPProcessing 高级RTP处理测试主函数
func testAdvancedRTPProcessing() {
	inputFile := "test_input_advanced.rtp"
	outputFile := "test_output_advanced.rtp"
	logFile := "test_log_advanced.txt"

	// 创建测试输入文件
	fmt.Println("=== 创建测试输入文件 ===")
	if err := createTestInputFile(inputFile); err != nil {
		log.Fatalf("创建测试输入文件失败: %v", err)
	}

	// 创建RTP处理器
	fmt.Println("\n=== 初始化RTP处理器 ===")
	config := rtp.ProcessorConfig{
		NearEndInterface: "near",
		FarEndInterface:  "far",
		BufferDuration:   200 * time.Millisecond,
		NACKTimeout:      50 * time.Millisecond,
		Debug:            true,
	}

	processor := rtp.NewPacketProcessor(config)

	// 创建输出文件和日志文件
	outFile, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("创建输出文件失败: %v", err)
	}
	defer outFile.Close()

	logF, err := os.Create(logFile)
	if err != nil {
		log.Fatalf("创建日志文件失败: %v", err)
	}
	defer logF.Close()

	// 设置包发送器
	sender := &MockPacketSender{
		outputFile: outFile,
		logFile:    logF,
	}
	processor.SetPacketSender(sender)

	// 启动处理器
	if err := processor.Start(); err != nil {
		log.Fatalf("启动RTP处理器失败: %v", err)
	}
	defer processor.Stop()

	// 打开输入文件
	inFile, err := os.Open(inputFile)
	if err != nil {
		log.Fatalf("打开输入文件失败: %v", err)
	}
	defer inFile.Close()

	// 读取并处理包
	fmt.Println("\n=== 开始处理RTP包 ===")
	packetCount := 0

	// 模拟网络地址
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("192.168.1.200")
	srcPort := uint16(5004)
	dstPort := uint16(5004)

	for {
		// 从文件读取RTP数据
		rtpData, err := readPacketFromFile(inFile)
		if err != nil {
			if err == io.EOF {
				fmt.Println("输入文件读取完毕")
				break
			}
			log.Printf("读取包失败: %v", err)
			continue
		}

		packetCount++

		// 解析RTP头部获取序列号
		if len(rtpData) >= 12 {
			seq := binary.BigEndian.Uint16(rtpData[2:4])
			fmt.Printf("处理包 #%d: seq=%d\n", packetCount, seq)
		}

		// 创建模拟的UDP包
		packet := createMockUDPPacket(rtpData, srcIP, dstIP, srcPort, dstPort)
		if packet == nil {
			log.Printf("创建模拟包失败")
			continue
		}

		// 使用RTP处理器处理包（模拟远端包，isNearEnd = false）
		shouldForward, err := processor.ProcessPacket(packet, false)
		if err != nil {
			log.Printf("处理包失败: %v", err)
			continue
		}

		fmt.Printf("  → 处理结果: shouldForward=%t\n", shouldForward)

		// 添加延迟模拟网络传输
		time.Sleep(20 * time.Millisecond)
	}

	// 等待处理器完成缓冲的包和NACK处理
	fmt.Println("\n=== 等待处理器完成 ===")
	time.Sleep(1 * time.Second)

	// 显示统计信息
	stats := processor.GetStats()
	fmt.Printf("\n=== 处理统计 ===\n")
	fmt.Printf("总UDP包: %d\n", stats.TotalUDPPackets)
	fmt.Printf("总RTP包: %d\n", stats.TotalRTPPackets)
	fmt.Printf("总非RTP包: %d\n", stats.TotalNonRTPPackets)
	fmt.Printf("\n远端统计:\n")
	fmt.Printf("  接收包: %d\n", stats.FarEndStats.ReceivedPackets)
	fmt.Printf("  转发包: %d\n", stats.FarEndStats.ForwardedPackets)
	fmt.Printf("  缓冲包: %d\n", stats.FarEndStats.BufferedPackets)
	fmt.Printf("  丢弃包: %d\n", stats.FarEndStats.DroppedPackets)
	fmt.Printf("  乱序包: %d\n", stats.FarEndStats.OutOfOrderPackets)
	fmt.Printf("  发送NACK: %d\n", stats.FarEndStats.NACKsSent)
	fmt.Printf("  接收NACK: %d\n", stats.FarEndStats.NACKsReceived)

	fmt.Printf("\n活跃会话: %d\n", len(stats.ActiveSessions))
	for ssrc, session := range stats.ActiveSessions {
		fmt.Printf("  SSRC 0x%08X: 最后活跃 %v\n", ssrc, session.LastSeen.Format("15:04:05"))
	}

	// 关闭输出文件以便验证
	outFile.Close()
	logF.Close()

	// 验证输出文件
	if err := verifyOutputFile(outputFile); err != nil {
		log.Printf("验证输出文件失败: %v", err)
	}

	fmt.Printf("\n=== 测试完成 ===\n")
	fmt.Printf("输入文件: %s\n", inputFile)
	fmt.Printf("输出文件: %s\n", outputFile)
	fmt.Printf("日志文件: %s\n", logFile)
	fmt.Printf("处理包数: %d\n", packetCount)
	fmt.Printf("\n可以使用以下命令查看文件:\n")
	fmt.Printf("ls -la %s %s %s\n", inputFile, outputFile, logFile)
	fmt.Printf("cat %s\n", logFile)
}

func main() {
	testAdvancedRTPProcessing()
}

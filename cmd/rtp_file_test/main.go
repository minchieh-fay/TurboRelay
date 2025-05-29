package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

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

// createTestInputFile 创建测试输入文件，包含乱序的RTP包
func createTestInputFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	ssrc := uint32(0x12345678)
	baseTimestamp := uint32(1000)
	payload := []byte("Hello RTP Test Data")

	// 创建乱序的序列号: 1, 2, 3, 4, 6, 5, 7, 8, 10, 9, 11
	sequences := []uint16{1, 2, 3, 4, 6, 5, 7, 8, 10, 9, 11}

	fmt.Printf("创建测试输入文件，包含乱序RTP包: %v\n", sequences)

	for i, seq := range sequences {
		timestamp := baseTimestamp + uint32(i*160) // 假设20ms间隔，8kHz采样率
		packet := createTestRTPPacket(ssrc, seq, timestamp, payload)

		if err := writePacketToFile(file, packet); err != nil {
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
				}
			}
			fmt.Println()

			lastSeq = seq
		}
	}

	fmt.Printf("总共验证了 %d 个包\n", packetCount)
	return nil
}

// testRTPFileProcessing RTP文件处理测试主函数
func testRTPFileProcessing() {
	inputFile := "test_input.rtp"
	outputFile := "test_output.rtp"

	// 创建测试输入文件
	fmt.Println("=== 创建测试输入文件 ===")
	if err := createTestInputFile(inputFile); err != nil {
		log.Fatalf("创建测试输入文件失败: %v", err)
	}

	// 创建输出文件
	outFile, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("创建输出文件失败: %v", err)
	}
	defer outFile.Close()

	// 打开输入文件
	inFile, err := os.Open(inputFile)
	if err != nil {
		log.Fatalf("打开输入文件失败: %v", err)
	}
	defer inFile.Close()

	// 读取并处理包
	fmt.Println("\n=== 开始处理RTP包 ===")
	packetCount := 0

	// 简单的排序缓冲区
	packetBuffer := make(map[uint16][]byte)
	expectedSeq := uint16(1)

	for {
		// 从文件读取包
		packetData, err := readPacketFromFile(inFile)
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
		if len(packetData) >= 12 {
			seq := binary.BigEndian.Uint16(packetData[2:4])
			fmt.Printf("读取包 #%d: seq=%d", packetCount, seq)

			// 将包放入缓冲区
			packetBuffer[seq] = packetData

			// 检查是否乱序
			if seq != expectedSeq {
				fmt.Printf(" (乱序，期望: %d)", expectedSeq)
			}
			fmt.Println()

			// 尝试输出连续的包
			outputCount := 0
			for {
				if data, exists := packetBuffer[expectedSeq]; exists {
					fmt.Printf("  → 输出排序后的包: seq=%d\n", expectedSeq)
					if err := writePacketToFile(outFile, data); err != nil {
						log.Printf("写入输出文件失败: %v", err)
					}
					delete(packetBuffer, expectedSeq)
					expectedSeq++
					outputCount++
				} else {
					break
				}
			}

			if outputCount > 0 {
				fmt.Printf("  → 本次输出了 %d 个包\n", outputCount)
			}
		}

		// 添加小延迟模拟网络传输
		time.Sleep(10 * time.Millisecond)
	}

	// 输出剩余的包（如果有的话）
	fmt.Println("\n=== 输出剩余缓冲包 ===")
	remainingCount := 0
	for seq := expectedSeq; seq <= expectedSeq+100; seq++ {
		if data, exists := packetBuffer[seq]; exists {
			fmt.Printf("输出剩余包: seq=%d\n", seq)
			if err := writePacketToFile(outFile, data); err != nil {
				log.Printf("写入输出文件失败: %v", err)
			}
			delete(packetBuffer, seq)
			remainingCount++
		}
	}

	fmt.Printf("输出了 %d 个剩余包\n", remainingCount)

	// 关闭输出文件以便验证
	outFile.Close()

	// 验证输出文件
	if err := verifyOutputFile(outputFile); err != nil {
		log.Printf("验证输出文件失败: %v", err)
	}

	fmt.Printf("\n=== 测试完成 ===\n")
	fmt.Printf("输入文件: %s\n", inputFile)
	fmt.Printf("输出文件: %s\n", outputFile)
	fmt.Printf("处理包数: %d\n", packetCount)
	fmt.Printf("\n可以使用以下命令查看文件:\n")
	fmt.Printf("ls -la %s %s\n", inputFile, outputFile)
}

func main() {
	testRTPFileProcessing()
}

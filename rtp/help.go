package rtp

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// buildModifiedUDPPacket builds a new UDP packet with modified payload
func (p *Processor) buildModifiedUDPPacket(originalPacket gopacket.Packet, newPayload []byte) ([]byte, error) {
	// 提取原始包的网络层信息
	ethLayer := originalPacket.Layer(layers.LayerTypeEthernet)
	ipLayer := originalPacket.Layer(layers.LayerTypeIPv4)
	udpLayer := originalPacket.Layer(layers.LayerTypeUDP)

	if ethLayer == nil || ipLayer == nil || udpLayer == nil {
		return nil, fmt.Errorf("missing required layers")
	}

	eth := ethLayer.(*layers.Ethernet)
	ip := ipLayer.(*layers.IPv4)
	udp := udpLayer.(*layers.UDP)

	// 创建新的层
	newEth := &layers.Ethernet{
		SrcMAC:       eth.SrcMAC,
		DstMAC:       eth.DstMAC,
		EthernetType: eth.EthernetType,
	}

	newIP := &layers.IPv4{
		Version:    ip.Version,
		IHL:        ip.IHL,
		TOS:        ip.TOS,
		Length:     uint16(20 + 8 + len(newPayload)), // IP header + UDP header + payload
		Id:         ip.Id,
		Flags:      ip.Flags,
		FragOffset: ip.FragOffset,
		TTL:        ip.TTL,
		Protocol:   ip.Protocol,
		SrcIP:      ip.SrcIP,
		DstIP:      ip.DstIP,
	}

	newUDP := &layers.UDP{
		SrcPort: udp.SrcPort,
		DstPort: udp.DstPort,
		Length:  uint16(8 + len(newPayload)), // UDP header + payload
	}

	// 设置校验和计算
	newUDP.SetNetworkLayerForChecksum(newIP)

	// 序列化包
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	err := gopacket.SerializeLayers(buffer, opts,
		newEth,
		newIP,
		newUDP,
		gopacket.Payload(newPayload),
	)

	if err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// calculateSeqDiff calculates sequence number difference considering wrap-around
// 返回值：正数表示current在expected之后，负数表示current在expected之前，0表示相等
func (p *Processor) calculateSeqDiff(current, expected uint16) int {
	// 直接计算差值
	diff := int(current) - int(expected)

	// 处理回绕情况
	// 如果差值超过32767，说明发生了回绕
	if diff > 32767 {
		// current实际上在expected之前（回绕到小数值）
		diff -= 65536
	} else if diff < -32767 {
		// current实际上在expected之后（回绕到大数值）
		diff += 65536
	}

	return diff
}

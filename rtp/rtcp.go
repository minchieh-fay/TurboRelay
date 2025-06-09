package rtp

import (
	"encoding/binary"
	"fmt"

	"github.com/google/gopacket"
)

// processRTCPPacket processes an RTCP packet
func (p *Processor) processRTCPPacket(packet gopacket.Packet, payload []byte, isNearEnd bool) (bool, error) {
	// Update RTCP packet statistics (no lock for performance)
	p.stats.TotalRTCPPackets++

	// Parse RTCP header to get SSRC
	if len(payload) < 8 {
		return true, nil
	}

	// 近端发的RTCP包：记录信息并透传
	if isNearEnd {
		// 记录近端RTCP会话信息
		p.recordRTCPSessionInfo(packet, payload, DirectionNearToFar)

		if p.config.Debug {
			logf("Near-end RTCP: recorded session info, forwarding transparently")
		}

		// 近端RTCP包直接透传，不做额外处理
		return true, nil
	}

	// 远端发的RTCP包：记录信息 + 处理NACK + 透传非NACK部分
	// 先记录远端RTCP会话信息
	p.recordRTCPSessionInfo(packet, payload, DirectionFarToNear)

	// 然后处理复合RTCP包（提取NACK，透传其他）
	return p.processRTCPCompound(packet, payload)
}

// recordRTCPSessionInfo records session information from RTCP packets
func (p *Processor) recordRTCPSessionInfo(packet gopacket.Packet, payload []byte, direction Direction) {
	// 提取第一个子报文的SSRC
	ssrc := binary.BigEndian.Uint32(payload[4:8])
	if ssrc == 0 {
		return // 过滤无效SSRC
	}

	sessionKey := SessionKey{
		SSRC:      ssrc,
		Direction: direction,
	}

	// 记录会话信息
	p.updateSessionInfo(packet, sessionKey, RTPHeader{SSRC: ssrc}, false)

	if p.config.Debug {
		packetType := payload[1]
		directionStr := "Near->Far"
		if direction == DirectionFarToNear {
			directionStr = "Far->Near"
		}
		logf("Recorded RTCP session: SSRC=%d, Type=%d, Direction=%s",
			ssrc, packetType, directionStr)
	}
}

// processRTCPCompound processes RTCP compound packet, extracts NACK packets for processing
// and forwards other RTCP packets transparently
func (p *Processor) processRTCPCompound(packet gopacket.Packet, payload []byte) (bool, error) {
	offset := 0
	totalLength := len(payload)
	var nonNACKPackets [][]byte
	hasNACK := false

	// 遍历所有RTCP子报文
	for offset < totalLength {
		if offset+4 > totalLength {
			break
		}

		// 获取当前子报文的长度
		lengthField := binary.BigEndian.Uint16(payload[offset+2 : offset+4])
		packetBytes := (int(lengthField) + 1) * 4

		if offset+packetBytes > totalLength {
			break
		}

		// 提取当前子报文
		subPacket := payload[offset : offset+packetBytes]
		packetType := subPacket[1]

		if packetType == 205 { // RTCP NACK (RFC 4585)
			// 处理NACK包
			hasNACK = true
			if err := p.handleNACKSubPacket(subPacket); err != nil {
				if p.config.Debug {
					logf("Failed to process NACK sub-packet: %v", err)
				}
			}
		} else {
			// 非NACK包，保存用于透传
			nonNACKPackets = append(nonNACKPackets, subPacket)
		}

		offset += packetBytes
	}

	// 如果有非NACK包，需要重新打包并透传
	if len(nonNACKPackets) > 0 {
		// 重新构建RTCP复合包（不包含NACK）
		newPayload := p.rebuildRTCPCompound(nonNACKPackets)
		if len(newPayload) > 0 {
			// 发送重新打包的RTCP包
			if err := p.forwardModifiedRTCPPacket(packet, newPayload); err != nil {
				if p.config.Debug {
					logf("Failed to forward modified RTCP packet: %v", err)
				}
			}
		}
	}

	if p.config.Debug {
		logf("Processed RTCP compound: hasNACK=%t, nonNACKPackets=%d", hasNACK, len(nonNACKPackets))
	}

	// 不转发原始包，因为我们已经处理了NACK并转发了修改后的包
	return false, nil
}

// handleNACKSubPacket processes a single NACK sub-packet
func (p *Processor) handleNACKSubPacket(nackPacket []byte) error {
	if len(nackPacket) < 16 {
		return fmt.Errorf("NACK packet too short: %d bytes", len(nackPacket))
	}

	// Parse NACK packet
	ssrc := binary.BigEndian.Uint32(nackPacket[8:12])
	pid := binary.BigEndian.Uint16(nackPacket[12:14])
	// blp := binary.BigEndian.Uint16(nackPacket[14:16]) // For simplicity, ignore BLP

	// 远端发来的NACK包，需要从近端RTP缓存中重传
	sessionKey := SessionKey{
		SSRC:      ssrc,
		Direction: DirectionNearToFar,
	}

	// Update NACK statistics (no lock for performance)
	p.stats.NearEndStats.NACKsReceived++

	p.resendFromNearEndBuffer(sessionKey, pid)

	if p.config.Debug {
		logf("Processed NACK sub-packet: SSRC=%d, PID=%d", ssrc, pid)
	}

	return nil
}

// rebuildRTCPCompound rebuilds RTCP compound packet from non-NACK sub-packets
func (p *Processor) rebuildRTCPCompound(subPackets [][]byte) []byte {
	if len(subPackets) == 0 {
		return nil
	}

	// 计算总长度
	totalLen := 0
	for _, subPacket := range subPackets {
		totalLen += len(subPacket)
	}

	// 重新组合
	result := make([]byte, totalLen)
	offset := 0
	for _, subPacket := range subPackets {
		copy(result[offset:], subPacket)
		offset += len(subPacket)
	}

	return result
}

// forwardModifiedRTCPPacket forwards the modified RTCP packet
func (p *Processor) forwardModifiedRTCPPacket(originalPacket gopacket.Packet, newPayload []byte) error {
	if p.sender == nil {
		return fmt.Errorf("packet sender not set")
	}

	// 构建新的UDP包
	newPacket, err := p.buildModifiedUDPPacket(originalPacket, newPayload)
	if err != nil {
		return fmt.Errorf("failed to build modified packet: %v", err)
	}

	// 发送到近端接口
	targetInterface := p.config.NearEndInterface
	if err := p.sender.SendPacket(newPacket, targetInterface); err != nil {
		return fmt.Errorf("failed to send modified RTCP packet: %v", err)
	}

	if p.config.Debug {
		logf("Forwarded modified RTCP packet: %d bytes", len(newPayload))
	}

	return nil
}

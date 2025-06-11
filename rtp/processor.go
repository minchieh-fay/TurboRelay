package rtp

import (
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// logf 带时间戳的日志输出
func logf(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05.000")
	log.Printf("%s "+format, append([]interface{}{timestamp}, args...)...)
}

// Direction represents packet direction
type Direction int

const (
	DirectionNearToFar Direction = iota // if1 -> if2 (终端 -> MCU)
	DirectionFarToNear                  // if2 -> if1 (MCU -> 终端)
)

// SessionKey uniquely identifies an RTP session with direction
type SessionKey struct {
	SSRC      uint32    `json:"ssrc"`
	Direction Direction `json:"direction"`
}

// PacketType represents the type of packet
type PacketType int

const (
	PacketTypeOther PacketType = iota
	PacketTypeRTP
	PacketTypeRTCP
)

// Processor implements PacketProcessor interface
type Processor struct {
	config ProcessorConfig
	sender PacketSender

	// Session management
	mutex    sync.RWMutex
	sessions map[SessionKey]*SessionInfo

	// Packet buffers
	nearEndBuffer map[SessionKey][]*BufferedPacket // 近端缓存 (100-500ms)
	farEndBuffer  map[SessionKey][]*BufferedPacket // 远端排序缓存

	// Statistics
	stats ProcessorStats

	// Control channels
	stopChan chan struct{}
	started  bool

	// Cleanup ticker
	cleanupTicker *time.Ticker
}

// NewProcessor creates a new RTP processor
func NewProcessor(config ProcessorConfig) *Processor {
	return &Processor{
		config:        config,
		sessions:      make(map[SessionKey]*SessionInfo),
		nearEndBuffer: make(map[SessionKey][]*BufferedPacket),
		farEndBuffer:  make(map[SessionKey][]*BufferedPacket),
		stats: ProcessorStats{
			ActiveSessions: make(map[uint32]*SessionInfo),
		},
		stopChan: make(chan struct{}),
	}
}

// Start starts the RTP processor
func (p *Processor) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.started {
		return fmt.Errorf("processor already started")
	}

	// Start cleanup routine
	p.cleanupTicker = time.NewTicker(30 * time.Second) // 每30秒清理一次
	go p.cleanupRoutine()

	p.started = true
	logf("RTP processor started with buffer duration: %v, NACK timeout: %v",
		p.config.BufferDuration, p.config.NACKTimeout)

	// 验证序列号处理逻辑
	if 1 == 2 && p.config.Debug {
		if p.validateSequenceLogic() {
			p.testSequenceHandling()
		}
	}

	return nil
}

// Stop stops the RTP processor
func (p *Processor) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.started {
		return
	}

	close(p.stopChan)
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
	}

	p.started = false
	logf("RTP processor stopped")
}

// SetPacketSender sets the packet sender callback
func (p *Processor) SetPacketSender(sender PacketSender) {
	p.sender = sender
}

// ProcessPacket processes a UDP packet and returns whether it should be forwarded
func (p *Processor) ProcessPacket(packet gopacket.Packet, isNearEnd bool) (shouldForward bool, err error) {
	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return true, nil // Not UDP, forward normally
	}

	udp, ok := udpLayer.(*layers.UDP)
	if !ok {
		return true, nil
	}

	payload := udp.Payload

	// Update UDP packet statistics (no lock for performance)
	p.stats.TotalUDPPackets++

	// Determine packet type once
	packetType := p.checkPacketType(payload)

	if packetType == PacketTypeRTP {
		return p.processRTPPacket(packet, payload, isNearEnd)
	} else if packetType == PacketTypeRTCP {
		return p.processRTCPPacket(packet, payload, isNearEnd)
	}

	// Not RTP/RTCP, update statistics and forward normally (no lock for performance)
	p.stats.TotalNonRTPPackets++

	return true, nil
}

// checkPacketType determines if payload is RTP, RTCP, or other packet type
func (p *Processor) checkPacketType(payload []byte) PacketType {
	if len(payload) < 8 {
		return PacketTypeOther
	}

	// Check RTP version (should be 2)
	version := (payload[0] >> 6) & 0x03
	if version != 2 {
		return PacketTypeOther
	}

	// Get the packet type field
	// 如果是rtcp的话  第二个byte都是pt
	// 如果是rtp的话  第二个byte的后7个bit是pt
	packetType := payload[1]
	rtpPacketType := packetType & 0x7F

	// RTP packets have payload type < 128
	if rtpPacketType < 64 || rtpPacketType > 95 {
		// Additional RTP validation: minimum 12 bytes for RTP header
		if len(payload) < 12 {
			return PacketTypeOther
		}
		return PacketTypeRTP
	}

	// RTCP packets have packet type >= 128 and <= 223
	if packetType >= 128 && packetType <= 223 {
		// 验证RTCP复合报文的长度字段
		return p.validateRTCPCompound(payload)
	}

	return PacketTypeOther
}

// validateRTCPCompound validates RTCP compound packet structure
func (p *Processor) validateRTCPCompound(payload []byte) PacketType {
	offset := 0
	totalLength := len(payload)

	// 遍历所有可能的RTCP子报文
	for offset < totalLength {
		// 每个RTCP报文至少需要4字节（固定头部）
		if offset+4 > totalLength {
			if p.config.Debug {
				logf("RTCP validation failed: insufficient bytes for header at offset %d", offset)
			}
			return PacketTypeOther
		}

		// 检查版本号
		version := (payload[offset] >> 6) & 0x03
		if version != 2 {
			if p.config.Debug {
				logf("RTCP validation failed: invalid version %d at offset %d", version, offset)
			}
			return PacketTypeOther
		}

		// 检查包类型
		packetType := payload[offset+1]
		if packetType < 128 || packetType > 223 {
			if p.config.Debug {
				logf("RTCP validation failed: invalid packet type %d at offset %d", packetType, offset)
			}
			return PacketTypeOther
		}

		// 获取长度字段（以32位字为单位，减1）
		lengthField := binary.BigEndian.Uint16(payload[offset+2 : offset+4])
		packetBytes := (int(lengthField) + 1) * 4

		// 检查长度是否合理
		if packetBytes < 4 || packetBytes > 1500 {
			if p.config.Debug {
				logf("RTCP validation failed: invalid packet length %d at offset %d", packetBytes, offset)
			}
			return PacketTypeOther
		}

		// 检查是否超出总长度
		if offset+packetBytes > totalLength {
			if p.config.Debug {
				logf("RTCP validation failed: packet length %d exceeds remaining bytes %d at offset %d",
					packetBytes, totalLength-offset, offset)
			}
			return PacketTypeOther
		}

		// 移动到下一个子报文
		offset += packetBytes
	}

	// 如果能完整解析所有子报文，且正好用完所有字节，则认为是有效的RTCP包
	if offset == totalLength {
		return PacketTypeRTCP
	}

	if p.config.Debug {
		logf("RTCP validation failed: total length mismatch, parsed %d bytes, expected %d", offset, totalLength)
	}
	return PacketTypeOther
}

// isSeqInOrder 检查序列号是否在合理的顺序范围内
// 考虑到网络乱序，允许一定范围内的乱序
func (p *Processor) isSeqInOrder(current, expected uint16, maxOutOfOrder int) bool {
	diff := p.calculateSeqDiff(current, expected)
	// 允许向前跳跃（丢包）和向后一定范围的乱序
	return diff >= -maxOutOfOrder && diff <= 1000 // 最大允许1000个包的跳跃
}

// isSeqNewer 检查seq1是否比seq2更新（考虑回绕）
func (p *Processor) isSeqNewer(seq1, seq2 uint16) bool {
	return p.calculateSeqDiff(seq1, seq2) > 0
}

// getSeqDistance 获取两个序列号之间的距离（绝对值）
func (p *Processor) getSeqDistance(seq1, seq2 uint16) int {
	diff := p.calculateSeqDiff(seq1, seq2)
	if diff < 0 {
		return -diff
	}
	return diff
}

// nextSeq returns the next sequence number considering wrap-around
func (p *Processor) nextSeq(seq uint16) uint16 {
	return uint16((int(seq) + 1) % 65536)
}

// prevSeq returns the previous sequence number considering wrap-around
func (p *Processor) prevSeq(seq uint16) uint16 {
	if seq == 0 {
		return 65535
	}
	return seq - 1
}

// seqInRange 检查序列号是否在指定范围内（考虑回绕）
func (p *Processor) seqInRange(seq, start, end uint16) bool {
	if start <= end {
		// 正常范围，没有回绕
		return seq >= start && seq <= end
	} else {
		// 范围跨越了回绕点
		return seq >= start || seq <= end
	}
}

// updateSessionInfo updates session information，
// return seq diff = rtp: if from far-end, return seq diff, if from near-end, return 0
func (p *Processor) updateSessionInfo(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader, isRTP bool) (int, float64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()

	session := p.sessions[sessionKey]
	if session == nil {
		session = &SessionInfo{
			SSRC:        header.SSRC,
			LastSeen:    now,
			FirstSeen:   now,
			MaxSeq:      header.SequenceNumber,
			PacketCount: 0,
			ActiveNACKs: make(map[uint16]*NACKInfo), // 初始化NACK跟踪
		}

		p.sessions[sessionKey] = session

		// Also add to stats map (without direction for compatibility)
		p.stats.ActiveSessions[header.SSRC] = session
	}

	session.LastSeen = now
	session.PacketCount++

	// 定义一个变量代表包的速度
	packetSpeed := 10.0
	// 计算firstseen到now的时间差
	duration := now.Sub(session.FirstSeen)
	if duration > time.Second*2 {
		packetSpeed = float64(session.PacketCount) / duration.Seconds()
		if duration > time.Second*4 {
			session.FirstSeen = session.FirstSeen.Add(time.Second * 2)
			session.PacketCount = session.PacketCount / 3
		}
	}

	// Extract network information
	streamInfo := p.extractStreamInfo(packet)

	if isRTP {
		session.RTPInfo = streamInfo
		if sessionKey.Direction == DirectionFarToNear {
			// Update sequence tracking for far-end packets
			seqdiff := p.calculateSeqDiff(header.SequenceNumber, session.MaxSeq)
			if seqdiff > 0 {
				session.MaxSeq = header.SequenceNumber
				return seqdiff, packetSpeed
			}
		}
	} else {
		session.RTCPInfo = streamInfo
	}
	return 0, packetSpeed
}

// extractStreamInfo extracts network information from packet
func (p *Processor) extractStreamInfo(packet gopacket.Packet) *StreamInfo {
	info := &StreamInfo{}

	// Extract Ethernet layer
	if ethLayer := packet.Layer(layers.LayerTypeEthernet); ethLayer != nil {
		eth, _ := ethLayer.(*layers.Ethernet)
		info.SourceMAC = eth.SrcMAC
		info.DestMAC = eth.DstMAC
	}

	// Extract IP layer
	if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		info.SourceIP = ip.SrcIP
		info.DestIP = ip.DstIP
	}

	// Extract UDP layer
	if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
		udp, _ := udpLayer.(*layers.UDP)
		info.SourcePort = uint16(udp.SrcPort)
		info.DestPort = uint16(udp.DstPort)
	}

	return info
}

// GetStats returns processor statistics
func (p *Processor) GetStats() ProcessorStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Create a copy of stats
	stats := p.stats
	stats.ActiveSessions = make(map[uint32]*SessionInfo)

	// Copy active sessions (group by SSRC, ignoring direction for stats)
	for key, session := range p.sessions {
		if existing, exists := stats.ActiveSessions[key.SSRC]; !exists {
			stats.ActiveSessions[key.SSRC] = session
		} else {
			// Merge information from both directions
			if session.RTPInfo != nil {
				existing.RTPInfo = session.RTPInfo
			}
			if session.RTCPInfo != nil {
				existing.RTCPInfo = session.RTCPInfo
			}
			if session.LastSeen.After(existing.LastSeen) {
				existing.LastSeen = session.LastSeen
			}
		}
	}

	return stats
}

// GetActiveNACKs returns information about currently active NACK requests
func (p *Processor) GetActiveNACKs() map[SessionKey]map[uint16]*NACKInfo {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	result := make(map[SessionKey]map[uint16]*NACKInfo)

	for sessionKey, session := range p.sessions {
		if session.ActiveNACKs != nil && len(session.ActiveNACKs) > 0 {
			// 创建副本避免并发访问问题
			nackCopy := make(map[uint16]*NACKInfo)
			for seq, nackInfo := range session.ActiveNACKs {
				nackCopy[seq] = &NACKInfo{
					SequenceNumber: nackInfo.SequenceNumber,
					FirstNACKTime:  nackInfo.FirstNACKTime,
					LastNACKTime:   nackInfo.LastNACKTime,
					RetryCount:     nackInfo.RetryCount,
					MaxRetries:     nackInfo.MaxRetries,
					GiveUpTime:     nackInfo.GiveUpTime,
				}
			}
			result[sessionKey] = nackCopy
		}
	}

	return result
}

// cleanupRoutine periodically cleans up expired sessions
func (p *Processor) cleanupRoutine() {
	for {
		select {
		case <-p.stopChan:
			return
		case <-p.cleanupTicker.C:
			p.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions removes sessions that haven't been used for 1 minute
func (p *Processor) cleanupExpiredSessions() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()
	expireThreshold := time.Minute // 1分钟超时

	var expiredKeys []SessionKey

	// Find expired sessions
	for key, session := range p.sessions {
		if now.Sub(session.LastSeen) > expireThreshold {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Remove expired sessions
	for _, key := range expiredKeys {
		session := p.sessions[key]

		// 清理该会话的所有活跃NACK
		if session != nil && session.ActiveNACKs != nil {
			nackCount := len(session.ActiveNACKs)
			if nackCount > 0 && p.config.Debug {
				logf("Cleaning up %d active NACKs for expired session: SSRC=%d, Direction=%d",
					nackCount, key.SSRC, key.Direction)
			}
		}

		delete(p.sessions, key)
		delete(p.nearEndBuffer, key)
		delete(p.farEndBuffer, key)

		if p.config.Debug {
			logf("Cleaned up expired session: SSRC=%d, Direction=%d", key.SSRC, key.Direction)
		}
	}

	// Also clean up stats map
	for ssrc, session := range p.stats.ActiveSessions {
		if now.Sub(session.LastSeen) > expireThreshold {
			delete(p.stats.ActiveSessions, ssrc)
		}
	}

	if len(expiredKeys) > 0 {
		logf("Cleaned up %d expired RTP sessions", len(expiredKeys))
	}
}

// resendFromNearEndBuffer resends packets from near-end buffer based on NACK
func (p *Processor) resendFromNearEndBuffer(sessionKey SessionKey, requestedSeq uint16) {
	p.mutex.RLock()
	buffer := p.nearEndBuffer[sessionKey]
	if len(buffer) == 0 {
		p.mutex.RUnlock()
		return
	}

	// Find the requested packet
	var foundPacket *BufferedPacket
	for _, bufferedPacket := range buffer {
		if bufferedPacket.Header.SequenceNumber == requestedSeq {
			foundPacket = bufferedPacket
			break
		}
	}
	p.mutex.RUnlock()

	// Send the packet if found
	if foundPacket != nil && p.sender != nil {
		targetInterface := p.config.FarEndInterface
		if err := p.sender.SendPacket(foundPacket.Data, targetInterface); err != nil {
			logf("Failed to resend packet: %v", err)
		} else {
			// Update statistics (no lock for performance)
			p.stats.NearEndStats.ForwardedPackets++

			if p.config.Debug {
				logf("Resent packet: SSRC=%d, Seq=%d",
					foundPacket.Header.SSRC, foundPacket.Header.SequenceNumber)
			}
		}
	}
}

// testSequenceHandling 测试序列号处理逻辑（用于调试和验证）
func (p *Processor) testSequenceHandling() {
	if !p.config.Debug {
		return
	}

	logf("=== Testing Sequence Number Handling ===")

	// 测试用例1: 65535, 1, 2, 3, 4
	testCases := []struct {
		name     string
		sequence []uint16
		expected []string
	}{
		{
			name:     "Normal wrap-around: 65535, 0, 1, 2, 3",
			sequence: []uint16{65535, 0, 1, 2, 3},
			expected: []string{"first", "next", "next", "next", "next"},
		},
		{
			name:     "Out of order with wrap: 1, 2, 3, 65535, 4, 5, 6",
			sequence: []uint16{1, 2, 3, 65535, 4, 5, 6},
			expected: []string{"first", "next", "next", "old", "next", "next", "next"},
		},
		{
			name:     "Complex out of order: 1, 3, 65535, 2, 5, 4",
			sequence: []uint16{1, 3, 65535, 2, 5, 4},
			expected: []string{"first", "future", "old", "fill", "future", "old"},
		},
		{
			name:     "Wrap-around out of order: 65534, 2, 1, 65535, 3, 4",
			sequence: []uint16{65534, 2, 1, 65535, 3, 4},
			expected: []string{"first", "future", "old", "fill", "next", "next"},
		},
		{
			name:     "Reverse wrap-around: 65535, 2, 1, 65534, 4, 3",
			sequence: []uint16{65535, 2, 1, 65534, 4, 3},
			expected: []string{"first", "future", "old", "old", "future", "old"},
		},
	}

	for _, tc := range testCases {
		logf("Testing: %s", tc.name)

		var expectedSeq uint16 = tc.sequence[0]

		for i, seq := range tc.sequence {
			if i == 0 {
				logf("  Seq %d: %s (baseline)", seq, tc.expected[i])
				expectedSeq = p.nextSeq(seq)
				continue
			}

			diff := p.calculateSeqDiff(seq, expectedSeq)
			var result string

			if diff == 0 {
				result = "next"
				expectedSeq = p.nextSeq(seq)
			} else if diff > 0 {
				if diff <= 100 {
					result = "future"
				} else {
					result = "reset"
				}
			} else {
				if diff >= -50 {
					if diff == -1 {
						result = "fill"
						expectedSeq = seq
					} else {
						result = "old"
					}
				} else {
					result = "drop"
				}
			}

			logf("  Seq %d: %s (expected: %s, diff: %d)",
				seq, result, tc.expected[i], diff)
		}
		logf("")
	}

	logf("=== Sequence Number Test Complete ===")
}

// validateSequenceLogic 验证序列号逻辑的一致性
func (p *Processor) validateSequenceLogic() bool {
	// 测试回绕边界情况
	testCases := []struct {
		current  uint16
		expected uint16
		wantDiff int
	}{
		{0, 65535, 1},      // 正常回绕
		{65535, 0, -1},     // 反向回绕
		{1, 65534, 3},      // 跨回绕点的大跳跃
		{65534, 1, -3},     // 反向跨回绕点
		{32768, 32767, 1},  // 中点附近
		{32767, 32768, -1}, // 中点附近反向
	}

	for _, tc := range testCases {
		diff := p.calculateSeqDiff(tc.current, tc.expected)
		if diff != tc.wantDiff {
			logf("Sequence logic error: calculateSeqDiff(%d, %d) = %d, want %d",
				tc.current, tc.expected, diff, tc.wantDiff)
			return false
		}
	}

	logf("Sequence logic validation passed")
	return true
}

// parseRTPHeader parses RTP header from payload
func (p *Processor) parseRTPHeader(payload []byte) (RTPHeader, error) {
	if len(payload) < 12 {
		return RTPHeader{}, fmt.Errorf("payload too short for RTP header")
	}

	header := RTPHeader{
		Version:        (payload[0] >> 6) & 0x03,
		Padding:        (payload[0] & 0x20) != 0,
		Extension:      (payload[0] & 0x10) != 0,
		CSRCCount:      payload[0] & 0x0F,
		Marker:         (payload[1] & 0x80) != 0,
		PayloadType:    payload[1] & 0x7F,
		SequenceNumber: binary.BigEndian.Uint16(payload[2:4]),
		Timestamp:      binary.BigEndian.Uint32(payload[4:8]),
		SSRC:           binary.BigEndian.Uint32(payload[8:12]),
	}

	return header, nil
}

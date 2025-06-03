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
	p.mutex.Lock()
	p.stats.TotalUDPPackets++
	p.mutex.Unlock()

	// Extract UDP layer
	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return true, nil // Not UDP, forward normally
	}

	udp, _ := udpLayer.(*layers.UDP)
	payload := udp.Payload

	if len(payload) < 12 { // 12字节是rtp头， 一般数据是不会小于12的
		return true, nil // Too short for RTP/RTCP
	}

	// Determine packet type
	if p.isRTPPacket(payload) {
		return p.processRTPPacket(packet, payload, isNearEnd)
	} else if p.isRTCPPacket(payload) {
		return p.processRTCPPacket(packet, payload, isNearEnd)
	}

	// Not RTP/RTCP packet
	p.mutex.Lock()
	p.stats.TotalNonRTPPackets++
	p.mutex.Unlock()

	return true, nil // Forward non-RTP/RTCP packets normally
}

// isRTPPacket checks if payload is an RTP packet
func (p *Processor) isRTPPacket(payload []byte) bool {
	if len(payload) < 12 {
		return false
	}

	// Check RTP version (should be 2)
	version := (payload[0] >> 6) & 0x03
	if version != 2 {
		return false
	}

	// Check payload type (should be < 128 for RTP, >= 128 for RTCP)
	payloadType := payload[1] & 0x7F
	return payloadType < 128
}

// isRTCPPacket checks if payload is an RTCP packet
func (p *Processor) isRTCPPacket(payload []byte) bool {
	if len(payload) < 8 {
		return false
	}

	// Check RTP version (should be 2)
	version := (payload[0] >> 6) & 0x03
	if version != 2 {
		return false
	}

	// Check packet type (should be >= 128 for RTCP)
	packetType := payload[1]
	return packetType >= 128 && packetType <= 223
}

// processRTPPacket processes an RTP packet
func (p *Processor) processRTPPacket(packet gopacket.Packet, payload []byte, isNearEnd bool) (bool, error) {
	header, err := p.parseRTPHeader(payload)
	if err != nil {
		return true, err
	}

	// 过滤SSRC=0的包，这些通常是无效的RTP包
	if header.SSRC == 0 {
		return true, nil // 直接转发，不处理
	}

	p.mutex.Lock()
	p.stats.TotalRTPPackets++
	p.mutex.Unlock()

	direction := DirectionNearToFar
	if !isNearEnd {
		direction = DirectionFarToNear
	}

	sessionKey := SessionKey{
		SSRC:      header.SSRC,
		Direction: direction,
	}

	// Update session info
	seqdiff := p.updateSessionInfo(packet, sessionKey, header, true)

	if isNearEnd {
		return p.processNearEndRTP(packet, sessionKey, header)
	} else {
		return p.processFarEndRTP(packet, sessionKey, header, seqdiff)
	}
}

// processRTCPPacket processes an RTCP packet
func (p *Processor) processRTCPPacket(packet gopacket.Packet, payload []byte, isNearEnd bool) (bool, error) {
	p.mutex.Lock()
	p.stats.TotalRTCPPackets++
	p.mutex.Unlock()

	// Parse RTCP header to get SSRC
	if len(payload) < 8 {
		return true, fmt.Errorf("RTCP packet too short")
	}

	packetType := payload[1]

	// 近端发的RTCP包：只透传并记录信息
	if isNearEnd {
		// 提取SSRC并记录会话信息
		var ssrc uint32
		if len(payload) >= 8 {
			ssrc = binary.BigEndian.Uint32(payload[4:8])
		}

		// 过滤SSRC=0的包
		if ssrc == 0 {
			return true, nil // 直接转发，不处理
		}

		sessionKey := SessionKey{
			SSRC:      ssrc,
			Direction: DirectionNearToFar,
		}

		// 记录RTCP会话信息（IP、端口、MAC地址等）
		p.updateSessionInfo(packet, sessionKey, RTPHeader{SSRC: ssrc}, false)

		if p.config.Debug {
			logf("Near-end RTCP: SSRC=%d, Type=%d, forwarding (transparent)", ssrc, packetType)
		}

		// 近端RTCP包直接透传
		return true, nil
	}

	// 远端发的RTCP包：需要处理NACK等
	// Handle NACK packets specially
	if packetType == 205 { // RTCP NACK (RFC 4585)
		return p.processNACKPacket(packet, payload, isNearEnd)
	}

	// For other RTCP packets from far-end, extract SSRC and update session info
	var ssrc uint32
	if len(payload) >= 8 {
		ssrc = binary.BigEndian.Uint32(payload[4:8])
	}

	// 过滤SSRC=0的包
	if ssrc == 0 {
		return true, nil // 直接转发，不处理
	}

	sessionKey := SessionKey{
		SSRC:      ssrc,
		Direction: DirectionFarToNear,
	}

	// Update session info for RTCP
	p.updateSessionInfo(packet, sessionKey, RTPHeader{SSRC: ssrc}, false)

	if p.config.Debug {
		logf("Far-end RTCP: SSRC=%d, Type=%d, forwarding", ssrc, packetType)
	}

	return true, nil // Forward RTCP packets normally
}

// processNearEndRTP processes RTP packet from near-end (if1 -> if2)
func (p *Processor) processNearEndRTP(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader) (bool, error) {
	// 近端包：缓存100-500ms，全部转发给远端
	bufferedPacket := &BufferedPacket{
		Data:      packet.Data(),
		Header:    header,
		Timestamp: time.Now(),
		Interface: p.config.NearEndInterface,
	}

	p.mutex.Lock()
	p.nearEndBuffer[sessionKey] = append(p.nearEndBuffer[sessionKey], bufferedPacket)
	p.stats.NearEndStats.ReceivedPackets++
	p.stats.NearEndStats.BufferedPackets++
	p.stats.NearEndStats.ForwardedPackets++
	// 如果p.nearEndBuffer[sessionKey]的长度大于1000，则清理老包
	if len(p.nearEndBuffer[sessionKey]) > 1000 {
		p.nearEndBuffer[sessionKey] = p.nearEndBuffer[sessionKey][:1000]
	}
	p.mutex.Unlock()

	if p.config.Debug {
		logf("Near-end RTP: SSRC=%d, Seq=%d, buffered and forwarding",
			header.SSRC, header.SequenceNumber)
	}

	return true, nil // 近端包全部转发
}

// processFarEndRTP processes RTP packet from far-end (if2 -> if1)
func (p *Processor) processFarEndRTP(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader, seqdiff int) (bool, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	session := p.sessions[sessionKey]
	if session == nil {
		// 新会话，直接转发
		p.stats.FarEndStats.ReceivedPackets++
		p.stats.FarEndStats.ForwardedPackets++

		if p.config.Debug {
			logf("Far-end RTP: New session SSRC=%d, Seq=%d, forwarding",
				header.SSRC, header.SequenceNumber)
		}

		return true, nil
	}

	p.stats.FarEndStats.ReceivedPackets++
	currentSeq := header.SequenceNumber

	// 检查并清理对应的NACK跟踪（包已收到）
	if _, exists := session.ActiveNACKs[currentSeq]; exists {
		delete(session.ActiveNACKs, currentSeq)
		if p.config.Debug {
			logf("Far-end RTP: SSRC=%d, Seq=%d, packet received, stopping NACK tracking",
				header.SSRC, currentSeq)
		}
	}

	// TODO: 用户将手动实现简单的丢包检测逻辑, seqdiff > 1 表示丢（乱序）了seqdiff-1个包
	// 举例： 原先是 6， currentSeq=9， seqdiff=3，表示丢（乱序）了2个包：7，8
	if seqdiff > 1 {
		for i := 1; i < seqdiff; i++ {
			nackinfo := &NACKInfo{
				SequenceNumber: currentSeq - uint16(i),
				FirstNACKTime:  time.Now(),
				RetryCount:     0,
				MaxRetries:     3,
				GiveUpTime:     time.Now().Add(time.Second * 5),
			}
			session.ActiveNACKs[currentSeq-uint16(i)] = nackinfo
			go p.scheduleNACK(session, nackinfo, sessionKey)
		}
	}

	// 远端包直接转发给近端
	p.stats.FarEndStats.ForwardedPackets++
	if p.config.Debug {
		logf("Far-end RTP: SSRC=%d, Seq=%d, forwarding directly to near-end",
			header.SSRC, currentSeq)
	}
	return true, nil
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
func (p *Processor) updateSessionInfo(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader, isRTP bool) int {
	now := time.Now()

	session := p.sessions[sessionKey]
	if session == nil {
		session = &SessionInfo{
			SSRC:        header.SSRC,
			LastSeen:    now,
			MaxSeq:      header.SequenceNumber,
			ActiveNACKs: make(map[uint16]*NACKInfo), // 初始化NACK跟踪
		}

		p.sessions[sessionKey] = session

		// Also add to stats map (without direction for compatibility)
		p.stats.ActiveSessions[header.SSRC] = session
	}

	session.LastSeen = now

	// Extract network information
	streamInfo := p.extractStreamInfo(packet)

	if isRTP {
		session.RTPInfo = streamInfo
		if sessionKey.Direction == DirectionFarToNear {
			// Update sequence tracking for far-end packets
			seqdiff := p.calculateSeqDiff(header.SequenceNumber, session.MaxSeq)
			if seqdiff > 0 {
				session.MaxSeq = header.SequenceNumber
				return seqdiff
			}
		}
	} else {
		session.RTCPInfo = streamInfo
	}
	return 0
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

// GetNACKStats returns NACK statistics summary
func (p *Processor) GetNACKStats() map[uint32]struct {
	ActiveNACKCount int
	OldestNACKAge   time.Duration
	TotalRetries    int
} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	result := make(map[uint32]struct {
		ActiveNACKCount int
		OldestNACKAge   time.Duration
		TotalRetries    int
	})

	now := time.Now()

	for sessionKey, session := range p.sessions {
		if session.ActiveNACKs == nil {
			continue
		}

		ssrc := sessionKey.SSRC
		stats := result[ssrc]

		for _, nackInfo := range session.ActiveNACKs {
			stats.ActiveNACKCount++
			stats.TotalRetries += nackInfo.RetryCount

			age := now.Sub(nackInfo.FirstNACKTime)
			if age > stats.OldestNACKAge {
				stats.OldestNACKAge = age
			}
		}

		result[ssrc] = stats
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

// processNACKPacket processes an incoming NACK packet
func (p *Processor) processNACKPacket(packet gopacket.Packet, payload []byte, isNearEnd bool) (bool, error) {
	if len(payload) < 16 {
		return true, fmt.Errorf("NACK packet too short")
	}

	// Parse NACK packet
	ssrc := binary.BigEndian.Uint32(payload[8:12])
	pid := binary.BigEndian.Uint16(payload[12:14])
	// blp := binary.BigEndian.Uint16(payload[14:16]) // For simplicity, ignore BLP

	direction := DirectionFarToNear
	if !isNearEnd {
		direction = DirectionNearToFar
	}

	sessionKey := SessionKey{
		SSRC:      ssrc,
		Direction: direction,
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if isNearEnd {
		// NACK from near-end, resend from near-end buffer
		p.stats.NearEndStats.NACKsReceived++
		p.resendFromNearEndBuffer(sessionKey, pid)
	} else {
		// NACK from far-end, update stats
		p.stats.FarEndStats.NACKsReceived++
	}

	if p.config.Debug {
		logf("Processed NACK: SSRC=%d, PID=%d, isNearEnd=%t", ssrc, pid, isNearEnd)
	}

	return false, nil // Don't forward NACK packets
}

// resendFromNearEndBuffer resends packets from near-end buffer based on NACK
func (p *Processor) resendFromNearEndBuffer(sessionKey SessionKey, requestedSeq uint16) {
	buffer := p.nearEndBuffer[sessionKey]
	if len(buffer) == 0 {
		return
	}

	// Find and resend the requested packet
	for _, bufferedPacket := range buffer {
		if bufferedPacket.Header.SequenceNumber == requestedSeq {
			if p.sender != nil {
				targetInterface := p.config.FarEndInterface
				if err := p.sender.SendPacket(bufferedPacket.Data, targetInterface); err != nil {
					logf("Failed to resend packet: %v", err)
				} else {
					p.stats.NearEndStats.ForwardedPackets++
					if p.config.Debug {
						logf("Resent packet: SSRC=%d, Seq=%d",
							bufferedPacket.Header.SSRC, bufferedPacket.Header.SequenceNumber)
					}
				}
			}
			break
		}
	}
}

// isDuplicatePacket 检查是否是重复包
func (p *Processor) isDuplicatePacket(sessionKey SessionKey, seq uint16) bool {
	buffer := p.farEndBuffer[sessionKey]
	for _, buffered := range buffer {
		if buffered.Header.SequenceNumber == seq {
			return true
		}
	}
	return false
}

// canFillGap 检查包是否能填补空隙 (deprecated - use sequence window)
func (p *Processor) canFillGap(sessionKey SessionKey, seq uint16) bool {
	// This function is deprecated and replaced by sequence window logic
	return false
}

// forwardConsecutiveBufferedPackets 转发缓存中连续的包 (deprecated - use forwardConsecutiveBufferedPacketsWithWindow)
func (p *Processor) forwardConsecutiveBufferedPackets(sessionKey SessionKey) {
	// This function is deprecated, use forwardConsecutiveBufferedPacketsWithWindow instead
	if p.config.Debug {
		logf("forwardConsecutiveBufferedPackets called (deprecated) for SSRC=%d", sessionKey.SSRC)
	}
	p.forwardConsecutiveBufferedPacketsWithWindow(sessionKey)
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

// forwardConsecutiveBufferedPacketsWithWindow 转发缓存中的包（简化版）
func (p *Processor) forwardConsecutiveBufferedPacketsWithWindow(sessionKey SessionKey) {
	session := p.sessions[sessionKey]
	if session == nil {
		return
	}

	buffer := p.farEndBuffer[sessionKey]
	if len(buffer) == 0 {
		return
	}

	// 简化逻辑：直接转发所有缓存包
	forwardedCount := 0
	for _, bufferedPacket := range buffer {
		if p.sender != nil {
			targetInterface := p.config.NearEndInterface
			if err := p.sender.SendPacket(bufferedPacket.Data, targetInterface); err != nil {
				logf("Failed to send buffered packet: %v", err)
				p.stats.FarEndStats.DroppedPackets++
			} else {
				p.stats.FarEndStats.ForwardedPackets++
				forwardedCount++
				if p.config.Debug {
					logf("Forwarded buffered packet: SSRC=%d, Seq=%d",
						bufferedPacket.Header.SSRC, bufferedPacket.Header.SequenceNumber)
				}
			}
		}
	}

	// 清空缓存
	p.farEndBuffer[sessionKey] = nil

	if forwardedCount > 0 && p.config.Debug {
		logf("Forwarded %d buffered packets for SSRC=%d",
			forwardedCount, sessionKey.SSRC)
	}
}

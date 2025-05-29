package rtp

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sort"
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

	// Start NACK timeout routine
	go p.nackTimeoutRoutine()

	p.started = true
	logf("RTP processor started with buffer duration: %v, NACK timeout: %v",
		p.config.BufferDuration, p.config.NACKTimeout)

	// 验证序列号处理逻辑
	if p.config.Debug {
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
	p.updateSessionInfo(packet, sessionKey, header, true)

	if isNearEnd {
		return p.processNearEndRTP(packet, sessionKey, header)
	} else {
		return p.processFarEndRTP(packet, sessionKey, header)
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
func (p *Processor) processFarEndRTP(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader) (bool, error) {
	// 远端包：使用序列号窗口进行排序处理，检查丢包
	p.mutex.Lock()
	defer p.mutex.Unlock()

	session := p.sessions[sessionKey]
	if session == nil {
		// 新会话，直接转发并初始化序列号窗口
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

	// 初始化序列号窗口（如果还没有）
	if session.SeqWindow == nil {
		session.SeqWindow = p.newSequenceWindow(currentSeq)
		session.MaxSeq = currentSeq
		p.stats.FarEndStats.ForwardedPackets++

		if p.config.Debug {
			logf("Far-end RTP: SSRC=%d, Seq=%d, initializing window, forwarding",
				header.SSRC, currentSeq)
		}

		return true, nil
	}

	// 使用序列号窗口处理包
	shouldForward, isNewPacket := p.updateSequenceWindow(session.SeqWindow, currentSeq)

	if p.config.Debug {
		windowStatus := p.getWindowStatus(session.SeqWindow)
		offset := p.calculateSeqDiff(currentSeq, session.SeqWindow.BaseSeq)
		if header.SSRC != 0 {
			logf("Far-end RTP: SSRC=%d, Seq=%d, Offset=%d, Forward=%t, New=%t, Window=[%s]",
				header.SSRC, currentSeq, offset, shouldForward, isNewPacket, windowStatus)
		}
	}

	if !isNewPacket {
		// 重复包或太旧的包
		p.stats.FarEndStats.DroppedPackets++
		if p.config.Debug && header.SSRC != 0 {
			logf("Far-end RTP: SSRC=%d, Seq=%d (duplicate/too old), dropping",
				header.SSRC, currentSeq)
		}
		return true, nil
	}

	// 更新最大序列号
	if p.isSeqNewer(currentSeq, session.MaxSeq) {
		session.MaxSeq = currentSeq
	}

	if shouldForward {
		// 可以立即转发
		p.stats.FarEndStats.ForwardedPackets++

		if p.config.Debug {
			logf("Far-end RTP: SSRC=%d, Seq=%d, forwarding immediately",
				header.SSRC, currentSeq)
		}

		// 检查是否可以转发缓存中的后续包
		p.forwardConsecutiveBufferedPacketsWithWindow(sessionKey)

		return true, nil
	} else {
		// 需要缓存等待前面的包
		bufferedPacket := &BufferedPacket{
			Data:      packet.Data(),
			Header:    header,
			Timestamp: time.Now(),
			Interface: p.config.FarEndInterface,
		}

		p.farEndBuffer[sessionKey] = append(p.farEndBuffer[sessionKey], bufferedPacket)
		p.stats.FarEndStats.BufferedPackets++

		if p.config.Debug {
			missing := p.getMissingSequences(session.SeqWindow)
			logf("Far-end RTP: SSRC=%d, Seq=%d, buffering (missing: %v, buffer_size: %d)",
				header.SSRC, currentSeq, missing, len(p.farEndBuffer[sessionKey]))
		}

		return false, nil
	}
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

// newSequenceWindow 创建新的序列号窗口
func (p *Processor) newSequenceWindow(baseSeq uint16) *SequenceWindow {
	return &SequenceWindow{
		BaseSeq:      baseSeq,
		WindowSize:   32, // 使用32位窗口
		ReceivedMask: 1,  // 标记baseSeq已接收
		MaxSeq:       baseSeq,
		LastUpdate:   time.Now(),
	}
}

// updateSequenceWindow 更新序列号窗口
func (p *Processor) updateSequenceWindow(window *SequenceWindow, seq uint16) (bool, bool) {
	// 返回值：(是否应该转发, 是否是新包)

	if window == nil {
		return false, false
	}

	window.LastUpdate = time.Now()

	// 计算序列号相对于窗口基准的偏移
	offset := p.calculateSeqDiff(seq, window.BaseSeq)

	// 情况1：序列号在当前窗口内
	if offset >= 0 && offset < window.WindowSize {
		// 检查是否已经接收过
		mask := uint32(1) << offset
		if window.ReceivedMask&mask != 0 {
			// 重复包
			return false, false
		}

		// 标记为已接收
		window.ReceivedMask |= mask

		// 更新最大序列号
		if p.isSeqNewer(seq, window.MaxSeq) {
			window.MaxSeq = seq
		}

		// 检查是否可以转发（前面的包都已接收）
		canForward := p.canForwardFromWindow(window, seq)
		return canForward, true
	}

	// 情况2：序列号在窗口前面（乱序的旧包）
	if offset < 0 && offset >= -window.WindowSize {
		// 这是一个乱序包，但在合理范围内
		// 需要扩展窗口向前
		p.expandWindowBackward(window, seq)

		// 乱序包不能直接转发，需要检查是否能填补空隙
		canForward := p.canForwardFromWindow(window, seq)
		return canForward, true
	}

	// 情况3：序列号在窗口后面（新包，需要滑动窗口）
	if offset >= window.WindowSize {
		// 检查跳跃是否合理
		if offset <= window.WindowSize*3 { // 允许3倍窗口大小的跳跃
			// 滑动窗口
			p.slideWindow(window, seq)

			// 新包需要等待前面的包，不能直接转发
			return false, true
		} else {
			// 跳跃太大，重置窗口
			p.resetWindow(window, seq)
			return true, true // 重置后直接转发
		}
	}

	// 情况4：太旧的包，直接丢弃
	return false, false
}

// canForwardFromWindow 检查是否可以从窗口转发包
func (p *Processor) canForwardFromWindow(window *SequenceWindow, seq uint16) bool {
	offset := p.calculateSeqDiff(seq, window.BaseSeq)

	// 检查从baseSeq到当前seq的所有包是否都已接收
	for i := 0; i <= offset; i++ {
		mask := uint32(1) << i
		if window.ReceivedMask&mask == 0 {
			// 有包缺失，不能转发
			return false
		}
	}

	return true
}

// expandWindowBackward 向前扩展窗口以容纳乱序包
func (p *Processor) expandWindowBackward(window *SequenceWindow, seq uint16) {
	diff := p.calculateSeqDiff(window.BaseSeq, seq)
	if diff <= 0 {
		return
	}

	// 向前移动窗口基准到新的序列号
	newBaseSeq := seq

	// 计算需要移动的位数
	shiftAmount := diff

	// 重新计算接收掩码
	oldMask := window.ReceivedMask
	newMask := uint32(1) // 标记新的baseSeq（当前seq）已接收

	// 将旧掩码中的位向右移动相应位数
	if shiftAmount < window.WindowSize {
		// 将旧的接收状态向右移动
		newMask |= (oldMask << shiftAmount)

		// 确保不超出窗口大小
		windowMask := uint32((1 << window.WindowSize) - 1)
		newMask &= windowMask
	}

	window.BaseSeq = newBaseSeq
	window.ReceivedMask = newMask

	if p.config.Debug {
		logf("Expanded window backward: BaseSeq=%d->%d, Mask=0x%08x->0x%08x, Shift=%d",
			window.BaseSeq+uint16(diff), newBaseSeq, oldMask, newMask, shiftAmount)
	}
}

// slideWindow 滑动窗口以容纳新的序列号
func (p *Processor) slideWindow(window *SequenceWindow, seq uint16) {
	offset := p.calculateSeqDiff(seq, window.BaseSeq)
	slideAmount := offset - window.WindowSize + 1

	if slideAmount <= 0 {
		return
	}

	// 滑动窗口基准
	window.BaseSeq = uint16((int(window.BaseSeq) + slideAmount) % 65536)

	// 右移接收掩码
	window.ReceivedMask >>= slideAmount

	// 标记新序列号已接收
	newOffset := p.calculateSeqDiff(seq, window.BaseSeq)
	if newOffset >= 0 && newOffset < window.WindowSize {
		window.ReceivedMask |= (1 << newOffset)
	}

	// 更新最大序列号
	if p.isSeqNewer(seq, window.MaxSeq) {
		window.MaxSeq = seq
	}
}

// resetWindow 重置窗口
func (p *Processor) resetWindow(window *SequenceWindow, seq uint16) {
	window.BaseSeq = seq
	window.ReceivedMask = 1 // 只标记当前序列号已接收
	window.MaxSeq = seq
}

// getMissingSequences 获取窗口中缺失的序列号
func (p *Processor) getMissingSequences(window *SequenceWindow) []uint16 {
	var missing []uint16

	if window == nil {
		return missing
	}

	// 检查从baseSeq到maxSeq之间缺失的序列号
	maxOffset := p.calculateSeqDiff(window.MaxSeq, window.BaseSeq)
	if maxOffset < 0 || maxOffset >= window.WindowSize {
		return missing
	}

	for i := 0; i <= maxOffset; i++ {
		mask := uint32(1) << i
		if window.ReceivedMask&mask == 0 {
			// 这个序列号缺失
			missingSeq := uint16((int(window.BaseSeq) + i) % 65536)
			missing = append(missing, missingSeq)
		}
	}

	return missing
}

// getWindowStatus 获取窗口状态（用于调试）
func (p *Processor) getWindowStatus(window *SequenceWindow) string {
	if window == nil {
		return "nil"
	}

	missing := p.getMissingSequences(window)
	return fmt.Sprintf("Base:%d Max:%d Mask:0x%08x Missing:%v",
		window.BaseSeq, window.MaxSeq, window.ReceivedMask, missing)
}

// updateSessionInfo updates session information
func (p *Processor) updateSessionInfo(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader, isRTP bool) {
	now := time.Now()

	session := p.sessions[sessionKey]
	if session == nil {
		session = &SessionInfo{
			SSRC:     header.SSRC,
			LastSeen: now,
			MaxSeq:   header.SequenceNumber,
		}

		// 为远端会话初始化序列号窗口
		if sessionKey.Direction == DirectionFarToNear && isRTP {
			session.SeqWindow = p.newSequenceWindow(header.SequenceNumber)
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
			if p.calculateSeqDiff(header.SequenceNumber, session.MaxSeq) > 0 {
				session.MaxSeq = header.SequenceNumber
			}
		}
	} else {
		session.RTCPInfo = streamInfo
	}
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

// nackTimeoutRoutine handles NACK timeout processing
func (p *Processor) nackTimeoutRoutine() {
	ticker := time.NewTicker(10 * time.Millisecond) // 检查间隔
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.processNACKTimeouts()
		}
	}
}

// processNACKTimeouts processes buffered packets that have timed out
func (p *Processor) processNACKTimeouts() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()

	for sessionKey, buffer := range p.farEndBuffer {
		if len(buffer) == 0 {
			continue
		}

		session := p.sessions[sessionKey]
		if session == nil || session.SeqWindow == nil {
			continue
		}

		// 检查是否有缺失的包需要NACK
		missing := p.getMissingSequences(session.SeqWindow)
		if len(missing) == 0 {
			// 没有缺失包，检查是否可以转发缓存中的包
			p.forwardConsecutiveBufferedPacketsWithWindow(sessionKey)
			continue
		}

		// 检查最老的缓存包是否超时
		oldestPacket := buffer[0]
		timeoutDuration := now.Sub(oldestPacket.Timestamp)

		if timeoutDuration > p.config.NACKTimeout {
			// 第一次超时：发送NACK请求
			if timeoutDuration <= p.config.NACKTimeout*3 { // 3倍超时内继续NACK，更快响应
				for _, missingSeq := range missing {
					p.sendNACKForSequence(sessionKey, missingSeq)
				}
				if p.config.Debug {
					logf("NACK timeout: SSRC=%d, missing=%v, sent NACK requests (timeout: %v)",
						sessionKey.SSRC, missing, timeoutDuration)
				}
			} else {
				// 多次NACK失败，放弃等待，强制转发
				if p.config.Debug {
					logf("NACK failed after multiple attempts: SSRC=%d, missing=%v, flushing buffer (timeout: %v)",
						sessionKey.SSRC, missing, timeoutDuration)
				}
				p.flushBufferedPackets(sessionKey)
			}
		}
	}
}

// flushBufferedPackets sends all buffered packets for a session
func (p *Processor) flushBufferedPackets(sessionKey SessionKey) {
	buffer := p.farEndBuffer[sessionKey]
	if len(buffer) == 0 {
		return
	}

	// Sort by sequence number
	sort.Slice(buffer, func(i, j int) bool {
		return p.calculateSeqDiff(buffer[i].Header.SequenceNumber, buffer[j].Header.SequenceNumber) < 0
	})

	// Send all buffered packets
	for _, bufferedPacket := range buffer {
		if p.sender != nil {
			targetInterface := p.config.NearEndInterface
			if err := p.sender.SendPacket(bufferedPacket.Data, targetInterface); err != nil {
				logf("Failed to send buffered packet: %v", err)
				p.stats.FarEndStats.DroppedPackets++
			} else {
				p.stats.FarEndStats.ForwardedPackets++
				if p.config.Debug {
					logf("Flushed buffered packet: SSRC=%d, Seq=%d",
						bufferedPacket.Header.SSRC, bufferedPacket.Header.SequenceNumber)
				}
			}
		}
	}

	// Clear buffer
	p.farEndBuffer[sessionKey] = nil
}

// scheduleNACKForMissing 为缺失的序列号安排NACK
func (p *Processor) scheduleNACKForMissing(sessionKey SessionKey, missingSeq uint16) {
	time.Sleep(p.config.NACKTimeout)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 检查缺失的包是否已经到达
	session := p.sessions[sessionKey]
	if session == nil || session.SeqWindow == nil {
		return
	}

	// 检查这个序列号是否仍然缺失
	offset := p.calculateSeqDiff(missingSeq, session.SeqWindow.BaseSeq)
	if offset >= 0 && offset < session.SeqWindow.WindowSize {
		mask := uint32(1) << offset
		if session.SeqWindow.ReceivedMask&mask == 0 {
			// 仍然缺失，发送NACK
			p.sendNACKForSequence(sessionKey, missingSeq)
		}
	}
}

// sendNACKForSequence 为单个序列号发送NACK
func (p *Processor) sendNACKForSequence(sessionKey SessionKey, missingSeq uint16) {
	if p.sender == nil {
		return
	}

	session := p.sessions[sessionKey]
	if session == nil {
		return
	}

	// Build NACK packet
	nackPacket := p.buildNACKPacket(sessionKey.SSRC, missingSeq, missingSeq, session)
	if nackPacket == nil {
		return
	}

	// Send NACK to far-end interface
	targetInterface := p.config.FarEndInterface
	if err := p.sender.SendPacket(nackPacket, targetInterface); err != nil {
		logf("Failed to send NACK: %v", err)
	} else {
		p.stats.FarEndStats.NACKsSent++
		if p.config.Debug {
			logf("Sent NACK for SSRC=%d, missing seq %d", sessionKey.SSRC, missingSeq)
		}
	}
}

// buildNACKPacket builds an RTCP NACK packet
func (p *Processor) buildNACKPacket(ssrc uint32, startSeq, endSeq uint16, session *SessionInfo) []byte {
	// Use RTCP info if available, otherwise use RTP info
	var sourcePort, destPort uint16
	var srcIP, dstIP net.IP
	var srcMAC, dstMAC net.HardwareAddr

	if session.RTCPInfo != nil {
		// Use RTCP port (reverse direction)
		destPort = session.RTCPInfo.SourcePort
		sourcePort = session.RTCPInfo.DestPort
		srcIP = session.RTCPInfo.DestIP
		dstIP = session.RTCPInfo.SourceIP
		srcMAC = session.RTCPInfo.DestMAC
		dstMAC = session.RTCPInfo.SourceMAC

		if p.config.Debug {
			logf("Using RTCP session info for NACK: %s:%d -> %s:%d",
				srcIP, sourcePort, dstIP, destPort)
		}
	} else if session.RTPInfo != nil {
		// 如果没有RTCP会话信息，直接使用RTP端口发送NACK包
		destPort = session.RTPInfo.SourcePort // 直接使用RTP端口
		sourcePort = session.RTPInfo.DestPort
		srcIP = session.RTPInfo.DestIP
		dstIP = session.RTPInfo.SourceIP
		srcMAC = session.RTPInfo.DestMAC
		dstMAC = session.RTPInfo.SourceMAC

		if p.config.Debug {
			logf("Using RTP session info for NACK (same port): %s:%d -> %s:%d",
				srcIP, sourcePort, dstIP, destPort)
		}
	} else {
		if p.config.Debug {
			logf("No session info available for NACK packet construction")
		}
		return nil
	}

	// Build RTCP NACK packet
	nackPayload := make([]byte, 16)

	// RTCP header
	nackPayload[0] = 0x81                               // V=2, P=0, FMT=1
	nackPayload[1] = 205                                // PT=205 (RTCP NACK)
	binary.BigEndian.PutUint16(nackPayload[2:4], 3)     // Length
	binary.BigEndian.PutUint32(nackPayload[4:8], ssrc)  // Sender SSRC
	binary.BigEndian.PutUint32(nackPayload[8:12], ssrc) // Media SSRC

	// NACK FCI (Feedback Control Information)
	binary.BigEndian.PutUint16(nackPayload[12:14], startSeq) // PID
	binary.BigEndian.PutUint16(nackPayload[14:16], 0)        // BLP (for simplicity)

	// Build complete packet using gopacket
	eth := &layers.Ethernet{
		SrcMAC:       srcMAC,
		DstMAC:       dstMAC,
		EthernetType: layers.EthernetTypeIPv4,
	}

	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    srcIP,
		DstIP:    dstIP,
	}

	udp := &layers.UDP{
		SrcPort: layers.UDPPort(sourcePort),
		DstPort: layers.UDPPort(destPort),
	}
	udp.SetNetworkLayerForChecksum(ip)

	// Serialize the packet
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	if err := gopacket.SerializeLayers(buffer, opts, eth, ip, udp, gopacket.Payload(nackPayload)); err != nil {
		if p.config.Debug {
			logf("Failed to serialize NACK packet: %v", err)
		}
		return nil
	}

	if p.config.Debug {
		logf("Built NACK packet: %s:%d -> %s:%d, SSRC=%d, Seq=%d",
			srcIP, sourcePort, dstIP, destPort, ssrc, startSeq)
	}

	return buffer.Bytes()
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

// forwardConsecutiveBufferedPacketsWithWindow 使用窗口转发缓存中连续的包
func (p *Processor) forwardConsecutiveBufferedPacketsWithWindow(sessionKey SessionKey) {
	session := p.sessions[sessionKey]
	if session == nil || session.SeqWindow == nil {
		return
	}

	buffer := p.farEndBuffer[sessionKey]
	if len(buffer) == 0 {
		return
	}

	// 按序列号排序
	sort.Slice(buffer, func(i, j int) bool {
		return p.calculateSeqDiff(buffer[i].Header.SequenceNumber, buffer[j].Header.SequenceNumber) < 0
	})

	var remainingBuffer []*BufferedPacket
	forwardedCount := 0

	// 检查缓存中的包是否可以转发
	for _, bufferedPacket := range buffer {
		seq := bufferedPacket.Header.SequenceNumber

		// 检查这个序列号是否在窗口中且已标记为接收
		offset := p.calculateSeqDiff(seq, session.SeqWindow.BaseSeq)
		if offset >= 0 && offset < session.SeqWindow.WindowSize {
			mask := uint32(1) << offset
			if session.SeqWindow.ReceivedMask&mask != 0 {
				// 检查是否可以转发（前面的包都已接收）
				if p.canForwardFromWindow(session.SeqWindow, seq) {
					// 可以转发
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
					continue // 不加入remainingBuffer
				}
			}
		}

		// 不能转发的包保留在缓存中
		remainingBuffer = append(remainingBuffer, bufferedPacket)
	}

	// 更新缓存
	p.farEndBuffer[sessionKey] = remainingBuffer

	if forwardedCount > 0 && p.config.Debug {
		logf("Forwarded %d buffered packets for SSRC=%d, %d packets remain in buffer",
			forwardedCount, sessionKey.SSRC, len(remainingBuffer))
	}
}

// scheduleNACK schedules a NACK request for missing packets (deprecated - use scheduleNACKForMissing)
func (p *Processor) scheduleNACK(sessionKey SessionKey, expectedSeq, currentSeq uint16) {
	// This function is deprecated and replaced by scheduleNACKForMissing
	// Keep for compatibility but do nothing
	if p.config.Debug {
		logf("scheduleNACK called (deprecated): SSRC=%d, expected=%d, current=%d",
			sessionKey.SSRC, expectedSeq, currentSeq)
	}
}

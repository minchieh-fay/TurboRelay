package rtp

import (
	"time"

	"github.com/google/gopacket"
)

// processRTPPacket processes an RTP packet
func (p *Processor) processRTPPacket(packet gopacket.Packet, payload []byte, isNearEnd bool) (bool, error) {
	header, err := p.parseRTPHeader(payload)
	if err != nil || header.SSRC == 0 {
		return true, err
	}

	// Update RTP packet statistics (no lock for performance)
	p.stats.TotalRTPPackets++

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
		go p.processNearEndRTP(packet, sessionKey, header)
		return true, nil
	} else {
		return p.processFarEndRTP(packet, sessionKey, header, seqdiff)
	}
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
	// 如果p.nearEndBuffer[sessionKey]的长度大于1000，则删除老包，保留新包
	if len(p.nearEndBuffer[sessionKey]) > 1000 {
		p.nearEndBuffer[sessionKey] = p.nearEndBuffer[sessionKey][len(p.nearEndBuffer[sessionKey])-1000:]
	}
	p.mutex.Unlock()

	// Update statistics (no lock for performance)
	p.stats.NearEndStats.ReceivedPackets++
	p.stats.NearEndStats.BufferedPackets++
	p.stats.NearEndStats.ForwardedPackets++

	if p.config.Debug {
		logf("Near-end RTP: SSRC=%d, Seq=%d, buffered and forwarding",
			header.SSRC, header.SequenceNumber)
	}

	return true, nil // 近端包全部转发
}

// processFarEndRTP processes RTP packet from far-end (if2 -> if1)
func (p *Processor) processFarEndRTP(packet gopacket.Packet, sessionKey SessionKey, header RTPHeader, seqdiff int) (bool, error) {
	p.mutex.Lock()
	session := p.sessions[sessionKey]
	if session == nil {
		p.mutex.Unlock()
		// 新会话：初始化序列号窗口后再转发
		// Update statistics (no lock for performance)
		p.stats.FarEndStats.ReceivedPackets++
		p.stats.FarEndStats.ForwardedPackets++

		if p.config.Debug {
			logf("Far-end RTP: New session SSRC=%d, Seq=%d, initialized and forwarding",
				header.SSRC, header.SequenceNumber)
		}

		return true, nil
	}

	currentSeq := header.SequenceNumber

	// 检查并清理对应的NACK跟踪（包已收到）
	if _, exists := session.ActiveNACKs[currentSeq]; exists {
		delete(session.ActiveNACKs, currentSeq)
		if p.config.Debug {
			logf("Far-end RTP: SSRC=%d, Seq=%d, packet received, stopping NACK tracking",
				header.SSRC, currentSeq)
		}
	}

	// 丢包检测：seqdiff > 1 表示丢（乱序）了seqdiff-1个包
	// 举例： 原先是 6， currentSeq=9， seqdiff=3，表示丢（乱序）了2个包：7，8
	if seqdiff > 1 {
		for i := 1; i < seqdiff; i++ {
			var seq uint16
			// 正确处理uint16下溢回绕
			if currentSeq >= uint16(i) {
				seq = currentSeq - uint16(i)
			} else {
				// 发生下溢，需要回绕
				// 例如：currentSeq=1, i=2, 结果应该是65535
				// 计算方法：65536 - (i - currentSeq) = 65536 - i + currentSeq
				seq = 65535 - uint16(i) + currentSeq + 1
			}

			// 避免重复的NACK请求
			if _, exists := session.ActiveNACKs[seq]; !exists {
				nackinfo := &NACKInfo{
					SequenceNumber: seq,
					FirstNACKTime:  time.Now(),
					RetryCount:     0,
					MaxRetries:     3,
					GiveUpTime:     time.Now().Add(time.Second * 5),
				}
				session.ActiveNACKs[seq] = nackinfo
				go p.scheduleNACK(session, nackinfo, sessionKey)

				if p.config.Debug {
					logf("Far-end RTP: SSRC=%d, detected missing packet Seq=%d, triggering NACK",
						header.SSRC, seq)
				}
			}
		}
	}
	p.mutex.Unlock()

	// Update statistics (no lock for performance)
	p.stats.FarEndStats.ReceivedPackets++
	p.stats.FarEndStats.ForwardedPackets++

	// 远端包直接转发给近端
	if p.config.Debug {
		logf("Far-end RTP: SSRC=%d, Seq=%d, forwarding directly to near-end",
			header.SSRC, currentSeq)
	}
	return true, nil
}

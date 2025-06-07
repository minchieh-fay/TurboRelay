package rtp

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// scheduleNACK schedules a NACK request for missing packets (deprecated - use scheduleNACKForMissing)
func (p *Processor) scheduleNACK(session *SessionInfo, nackinfo *NACKInfo, sessionKey SessionKey) {

	expectedSeq := nackinfo.SequenceNumber
	if p.config.Debug {
		logf("1--------------------------------%d", expectedSeq)
	}
	// sleep 10ms
	time.Sleep(time.Millisecond * 10)

	// 检查session.ActiveNACKs， 如果不存在， 说明这个包已经来过了，可以不用处理了
	if nackinfo.Received {
		if p.config.Debug {
			logf("2--------------------------------%d", expectedSeq)
		}
		return
	}

	if p.config.Debug {
		logf("scheduleNACK called (deprecated): SSRC=%d, expected=%d",
			sessionKey.SSRC, expectedSeq)
	}

	// 创建新的NACK跟踪信息

	// 一边判断nackinfo.Received， 一边发送NACK， 执行5次， 每次间隔50ms
	for i := 0; i < 5; i++ {
		if nackinfo.Received {
			if p.config.Debug {
				logf("3--------------------------------%d", expectedSeq)
			}
			return
		}
		if p.config.Debug {
			logf("4--------------------------------%d", expectedSeq)
		}
		p.sendNACKForSequence(sessionKey, expectedSeq)
		time.Sleep(time.Millisecond * 50)
	}

	if p.config.Debug {
		logf("5--------------------------------%d", expectedSeq)
	}
	time.Sleep(time.Millisecond * 250)
	// 不管有没有来  清理nackinfo
	p.mutex.Lock()
	delete(session.ActiveNACKs, expectedSeq)
	p.mutex.Unlock()
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
		// Update statistics (no lock for performance)
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

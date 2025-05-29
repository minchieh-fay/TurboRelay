package net

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"turborelay/rtp"
)

// Forwarder network packet forwarder
type Forwarder struct {
	interface1   string
	interface2   string
	debug        bool
	if1IsNearEnd bool
	if2IsNearEnd bool
	handle1      *pcap.Handle
	handle2      *pcap.Handle

	// RTP processor
	rtpManager *rtp.ProcessorManager

	// Statistics
	mutex         sync.RWMutex
	if1ToIf2Stats DirectionStats
	if2ToIf1Stats DirectionStats
}

// NewForwarder creates a new forwarder
func NewForwarder(interface1, interface2 string, debug bool, if1IsNearEnd, if2IsNearEnd bool) *Forwarder {
	// 创建RTP处理器配置
	rtpConfig := rtp.ProcessorConfig{
		NearEndInterface: interface1,             // if1固定为近端
		FarEndInterface:  interface2,             // if2固定为远端
		BufferDuration:   200 * time.Millisecond, // 近端缓存200ms
		NACKTimeout:      30 * time.Millisecond,  // NACK超时30ms，更快响应
		Debug:            debug,
	}

	// 创建RTP处理器管理器
	rtpManager := rtp.NewProcessorManager(rtpConfig)

	forwarder := &Forwarder{
		interface1:   interface1,
		interface2:   interface2,
		debug:        debug,
		if1IsNearEnd: if1IsNearEnd,
		if2IsNearEnd: if2IsNearEnd,
		rtpManager:   rtpManager,
		if1ToIf2Stats: DirectionStats{
			SourceInterface: interface1,
			DestInterface:   interface2,
			EndType:         getEndType(if1IsNearEnd),
		},
		if2ToIf1Stats: DirectionStats{
			SourceInterface: interface2,
			DestInterface:   interface1,
			EndType:         getEndType(if2IsNearEnd),
		},
	}

	// 设置RTP包发送回调函数
	sender := rtp.NewPacketSender(func(data []byte, toInterface string) error {
		// 根据目标接口选择对应的handle
		var targetHandle *pcap.Handle
		if toInterface == interface1 {
			targetHandle = forwarder.handle1
		} else if toInterface == interface2 {
			targetHandle = forwarder.handle2
		} else {
			return fmt.Errorf("unknown target interface: %s", toInterface)
		}

		if targetHandle == nil {
			return fmt.Errorf("target handle not initialized for interface: %s", toInterface)
		}

		return forwarder.writePacket(targetHandle, data)
	})
	rtpManager.SetPacketSender(sender)

	return forwarder
}

// getEndType returns the end type string based on isNearEnd flag
func getEndType(isNearEnd bool) string {
	if isNearEnd {
		return "Near-end (Direct Device)"
	}
	return "Far-end (MCU)"
}

// Start starts the forwarder
func (f *Forwarder) Start() error {
	var err error

	log.Printf("Starting TurboRelay packet forwarder...")
	log.Printf("Interface1: %s, Interface2: %s", f.interface1, f.interface2)
	log.Printf("Debug mode: %t", f.debug)

	// Open first network interface
	log.Printf("Opening interface %s...", f.interface1)
	f.handle1, err = pcap.OpenLive(f.interface1, 1600, true, pcap.BlockForever)
	if err != nil {
		return fmt.Errorf("failed to open interface %s: %v", f.interface1, err)
	}
	log.Printf("Successfully opened interface %s (promiscuous mode enabled)", f.interface1)

	// Open second network interface
	log.Printf("Opening interface %s...", f.interface2)
	f.handle2, err = pcap.OpenLive(f.interface2, 1600, true, pcap.BlockForever)
	if err != nil {
		f.handle1.Close()
		return fmt.Errorf("failed to open interface %s: %v", f.interface2, err)
	}
	log.Printf("Successfully opened interface %s (promiscuous mode enabled)", f.interface2)

	// Set BPF filter: capture IP packets and ARP packets
	filter := "ip or arp"
	log.Printf("Setting BPF filter: %s", filter)

	if err := f.handle1.SetBPFFilter(filter); err != nil {
		log.Printf("Warning: failed to set BPF filter on %s: %v", f.interface1, err)
		// Try without any filter
		log.Printf("Trying without BPF filter on %s", f.interface1)
	} else {
		log.Printf("Successfully set BPF filter on %s: %s", f.interface1, filter)
	}

	if err := f.handle2.SetBPFFilter(filter); err != nil {
		log.Printf("Warning: failed to set BPF filter on %s: %v", f.interface2, err)
		// Try without any filter
		log.Printf("Trying without BPF filter on %s", f.interface2)
	} else {
		log.Printf("Successfully set BPF filter on %s: %s", f.interface2, filter)
	}

	log.Printf("Starting bidirectional packet forwarding: %s <-> %s", f.interface1, f.interface2)
	log.Printf("BPF filter applied: %s", filter)

	// 启动RTP处理器
	log.Printf("Starting RTP processor...")
	if err := f.rtpManager.Start(); err != nil {
		log.Printf("Warning: failed to start RTP processor: %v", err)
	} else {
		log.Printf("RTP processor started successfully")
	}

	// Start bidirectional forwarding
	go f.forwardPackets(f.handle1, f.handle2, f.interface1, f.interface2, f.if1IsNearEnd)
	go f.forwardPackets(f.handle2, f.handle1, f.interface2, f.interface1, f.if2IsNearEnd)

	log.Printf("TurboRelay is now running and forwarding packets...")
	return nil
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	log.Printf("Stopping TurboRelay...")

	// 停止RTP处理器
	if f.rtpManager != nil {
		log.Printf("Stopping RTP processor...")
		f.rtpManager.Stop()
		log.Printf("RTP processor stopped")
	}

	if f.handle1 != nil {
		f.handle1.Close()
		log.Printf("Closed interface %s", f.interface1)
	}
	if f.handle2 != nil {
		f.handle2.Close()
		log.Printf("Closed interface %s", f.interface2)
	}
}

// forwardPackets forwards packets between interfaces
func (f *Forwarder) forwardPackets(srcHandle, dstHandle *pcap.Handle, srcInterface, dstInterface string, isNearEnd bool) {
	endType := getEndType(isNearEnd)
	log.Printf("Starting packet forwarding goroutine: %s -> %s (%s)", srcInterface, dstInterface, endType)

	packetSource := gopacket.NewPacketSource(srcHandle, srcHandle.LinkType())
	packetSource.NoCopy = true

	// Determine which stats to update
	var stats *DirectionStats
	if srcInterface == f.interface1 {
		stats = &f.if1ToIf2Stats
	} else {
		stats = &f.if2ToIf1Stats
	}

	for packet := range packetSource.Packets() {
		f.mutex.Lock()
		stats.PacketCount++
		f.mutex.Unlock()

		if packet.ErrorLayer() != nil {
			if f.debug {
				log.Printf("Packet parsing error: %v", packet.ErrorLayer().Error())
			}
			continue
		}

		// Get raw packet data
		data := packet.Data()
		if len(data) == 0 {
			if f.debug {
				log.Printf("Empty packet data, skipping")
			}
			continue
		}

		// Check if this is an ICMP packet (ping) or ARP packet
		isICMP := false
		isARP := false
		if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
			ip, _ := ipLayer.(*layers.IPv4)
			if ip.Protocol == layers.IPProtocolICMPv4 {
				isICMP = true
			}
		}
		if packet.Layer(layers.LayerTypeARP) != nil {
			isARP = true
		}

		// Update packet type statistics
		f.mutex.Lock()
		if isICMP {
			stats.ICMPCount++
		} else if isARP {
			stats.ARPCount++
		} else {
			stats.OtherCount++
		}
		f.mutex.Unlock()

		// RTP处理：在writePacket之前劫持UDP包
		shouldForward := true
		if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
			// 这是UDP包，让RTP处理器分析
			isNearEnd := (srcInterface == f.interface1) // if1固定为近端

			processed, err := f.rtpManager.ProcessPacket(packet, isNearEnd)
			if err != nil {
				if f.debug {
					log.Printf("RTP processing error: %v", err)
				}
				// 出错时仍然转发包
			} else {
				shouldForward = processed
				if f.debug && !shouldForward {
					log.Printf("RTP processor decided not to forward UDP packet")
				}
			}
		}

		// 如果RTP处理器决定不转发，跳过此包
		if !shouldForward {
			continue
		}

		// Forward packet
		if err := f.writePacket(dstHandle, data); err != nil {
			f.mutex.Lock()
			stats.ErrorCount++
			f.mutex.Unlock()

			log.Printf("Failed to forward packet %s -> %s (%s): %v", srcInterface, dstInterface, endType, err)

			// 对于远端连接，丢包是预期的，不需要过多报错
			if !isNearEnd && stats.ErrorCount%100 == 1 {
				log.Printf("Far-end connection packet loss stats: %d/%d packets lost", stats.ErrorCount, stats.PacketCount)
			}
			continue
		}

		f.mutex.Lock()
		stats.SuccessCount++
		f.mutex.Unlock()

		// Log packet info - show all ICMP and ARP packets, and every 50th packet for others
		if 1 == 2 { // 暂时不打印
			if isICMP || isARP || f.debug || stats.PacketCount%50 == 1 {
				f.logPacketInfo(packet, srcInterface, dstInterface, int(stats.PacketCount))
			}
		}

		// 定期报告统计信息
		if stats.PacketCount%1000 == 0 {
			f.mutex.RLock()
			lossRate := float64(stats.ErrorCount) / float64(stats.PacketCount) * 100
			log.Printf("Forwarding stats %s->%s (%s): Total:%d Success:%d Failed:%d Loss:%.2f%%",
				srcInterface, dstInterface, endType, stats.PacketCount, stats.SuccessCount, stats.ErrorCount, lossRate)
			f.mutex.RUnlock()
		}
	}

	f.mutex.RLock()
	log.Printf("Packet forwarding goroutine ended: %s -> %s (%s)", srcInterface, dstInterface, endType)
	log.Printf("Final stats: Total:%d Success:%d Failed:%d", stats.PacketCount, stats.SuccessCount, stats.ErrorCount)
	f.mutex.RUnlock()
}

// writePacket writes packet data
func (f *Forwarder) writePacket(handle *pcap.Handle, data []byte) error {
	return handle.WritePacketData(data)
}

// logPacketInfo logs packet information
func (f *Forwarder) logPacketInfo(packet gopacket.Packet, srcInterface, dstInterface string, packetCount int) {
	timestamp := time.Now().Format("15:04:05.000")

	// Check if this is an ARP packet first
	if arpLayer := packet.Layer(layers.LayerTypeARP); arpLayer != nil {
		arp, _ := arpLayer.(*layers.ARP)
		var operation string
		switch arp.Operation {
		case layers.ARPRequest:
			operation = "Request"
		case layers.ARPReply:
			operation = "Reply"
		default:
			operation = fmt.Sprintf("Op:%d", arp.Operation)
		}

		srcMAC := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			arp.SourceHwAddress[0], arp.SourceHwAddress[1], arp.SourceHwAddress[2],
			arp.SourceHwAddress[3], arp.SourceHwAddress[4], arp.SourceHwAddress[5])
		dstMAC := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			arp.DstHwAddress[0], arp.DstHwAddress[1], arp.DstHwAddress[2],
			arp.DstHwAddress[3], arp.DstHwAddress[4], arp.DstHwAddress[5])

		srcIP := fmt.Sprintf("%d.%d.%d.%d", arp.SourceProtAddress[0], arp.SourceProtAddress[1],
			arp.SourceProtAddress[2], arp.SourceProtAddress[3])
		dstIP := fmt.Sprintf("%d.%d.%d.%d", arp.DstProtAddress[0], arp.DstProtAddress[1],
			arp.DstProtAddress[2], arp.DstProtAddress[3])

		log.Printf("[%s] #%d %s->%s: ARP %s %s(%s) -> %s(%s)",
			timestamp, packetCount, srcInterface, dstInterface, operation,
			srcIP, srcMAC, dstIP, dstMAC)
		return
	}

	// Get network layer information for IP packets
	var srcIP, dstIP, protocol string
	var length int

	if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		srcIP = ip.SrcIP.String()
		dstIP = ip.DstIP.String()
		protocol = ip.Protocol.String()
		length = int(ip.Length)
	} else if ipLayer := packet.Layer(layers.LayerTypeIPv6); ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv6)
		srcIP = ip.SrcIP.String()
		dstIP = ip.DstIP.String()
		protocol = ip.NextHeader.String()
		length = int(ip.Length)
	}

	// Get transport layer port information
	var srcPort, dstPort string
	var extraInfo string

	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		srcPort = fmt.Sprintf(":%d", tcp.SrcPort)
		dstPort = fmt.Sprintf(":%d", tcp.DstPort)
	} else if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
		udp, _ := udpLayer.(*layers.UDP)
		srcPort = fmt.Sprintf(":%d", udp.SrcPort)
		dstPort = fmt.Sprintf(":%d", udp.DstPort)
	} else if icmpLayer := packet.Layer(layers.LayerTypeICMPv4); icmpLayer != nil {
		icmp, _ := icmpLayer.(*layers.ICMPv4)
		extraInfo = fmt.Sprintf(" [ICMP Type:%d Code:%d]", icmp.TypeCode.Type(), icmp.TypeCode.Code())
		// ICMP doesn't have ports, but we can show type info
		srcPort = ""
		dstPort = ""
	}

	log.Printf("[%s] #%d %s->%s: %s %s%s -> %s%s (%d bytes)%s",
		timestamp, packetCount, srcInterface, dstInterface, protocol,
		srcIP, srcPort, dstIP, dstPort, length, extraInfo)
}

// GetStats returns forwarding statistics
func (f *Forwarder) GetStats() ForwardingStats {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	// Calculate loss rates
	if f.if1ToIf2Stats.PacketCount > 0 {
		f.if1ToIf2Stats.LossRate = float64(f.if1ToIf2Stats.ErrorCount) / float64(f.if1ToIf2Stats.PacketCount) * 100
	}
	if f.if2ToIf1Stats.PacketCount > 0 {
		f.if2ToIf1Stats.LossRate = float64(f.if2ToIf1Stats.ErrorCount) / float64(f.if2ToIf1Stats.PacketCount) * 100
	}

	// Calculate totals
	totalPackets := f.if1ToIf2Stats.PacketCount + f.if2ToIf1Stats.PacketCount
	totalSuccess := f.if1ToIf2Stats.SuccessCount + f.if2ToIf1Stats.SuccessCount
	totalErrors := f.if1ToIf2Stats.ErrorCount + f.if2ToIf1Stats.ErrorCount

	var overallLossRate float64
	if totalPackets > 0 {
		overallLossRate = float64(totalErrors) / float64(totalPackets) * 100
	}

	return ForwardingStats{
		If1ToIf2Stats:   f.if1ToIf2Stats,
		If2ToIf1Stats:   f.if2ToIf1Stats,
		TotalPackets:    totalPackets,
		TotalSuccess:    totalSuccess,
		TotalErrors:     totalErrors,
		OverallLossRate: overallLossRate,
	}
}

// SetDebugMode enables or disables debug mode
func (f *Forwarder) SetDebugMode(enabled bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.debug = enabled
	log.Printf("Debug mode %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

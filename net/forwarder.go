package net

import (
	"fmt"
	"log"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
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
}

// NewForwarder creates a new forwarder
func NewForwarder(interface1, interface2 string, debug bool, if1IsNearEnd, if2IsNearEnd bool) *Forwarder {
	return &Forwarder{
		interface1:   interface1,
		interface2:   interface2,
		debug:        debug,
		if1IsNearEnd: if1IsNearEnd,
		if2IsNearEnd: if2IsNearEnd,
	}
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

	// Start bidirectional forwarding
	go f.forwardPackets(f.handle1, f.handle2, f.interface1, f.interface2, f.if1IsNearEnd)
	go f.forwardPackets(f.handle2, f.handle1, f.interface2, f.interface1, f.if2IsNearEnd)

	log.Printf("TurboRelay is now running and forwarding packets...")
	return nil
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	log.Printf("Stopping TurboRelay...")
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
	endType := "远端(MCU)"
	if isNearEnd {
		endType = "近端(直连设备)"
	}
	log.Printf("Starting packet forwarding goroutine: %s -> %s (%s)", srcInterface, dstInterface, endType)

	packetSource := gopacket.NewPacketSource(srcHandle, srcHandle.LinkType())
	packetSource.NoCopy = true

	packetCount := 0
	successCount := 0
	errorCount := 0

	for packet := range packetSource.Packets() {
		packetCount++

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

		// Forward packet
		if err := f.writePacket(dstHandle, data); err != nil {
			errorCount++
			log.Printf("Failed to forward packet %s -> %s (%s): %v", srcInterface, dstInterface, endType, err)

			// 对于远端连接，丢包是预期的，不需要过多报错
			if !isNearEnd && errorCount%100 == 1 {
				log.Printf("远端连接丢包统计: %d/%d packets lost", errorCount, packetCount)
			}
			continue
		}

		successCount++

		// Log packet info - show all ICMP and ARP packets, and every 50th packet for others
		if 1 == 2 { // 暂时不打印
			if isICMP || isARP || f.debug || packetCount%50 == 1 {
				f.logPacketInfo(packet, srcInterface, dstInterface, packetCount)
			}
		}

		// 定期报告统计信息
		if packetCount%1000 == 0 {
			lossRate := float64(errorCount) / float64(packetCount) * 100
			log.Printf("转发统计 %s->%s (%s): 总计:%d 成功:%d 失败:%d 丢包率:%.2f%%",
				srcInterface, dstInterface, endType, packetCount, successCount, errorCount, lossRate)
		}
	}

	log.Printf("Packet forwarding goroutine ended: %s -> %s (%s)", srcInterface, dstInterface, endType)
	log.Printf("最终统计: 总计:%d 成功:%d 失败:%d", packetCount, successCount, errorCount)
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

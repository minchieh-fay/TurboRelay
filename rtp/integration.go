package rtp

import (
	"log"

	"github.com/google/gopacket"
)

// PacketSenderImpl implements PacketSender interface for integration with net package
type PacketSenderImpl struct {
	sendFunc func(data []byte, toInterface string) error
}

// NewPacketSender creates a new packet sender with the provided send function
func NewPacketSender(sendFunc func(data []byte, toInterface string) error) PacketSender {
	return &PacketSenderImpl{
		sendFunc: sendFunc,
	}
}

// SendPacket implements PacketSender interface
func (ps *PacketSenderImpl) SendPacket(data []byte, toInterface string) error {
	if ps.sendFunc == nil {
		log.Printf("Warning: packet sender function not set")
		return nil
	}
	return ps.sendFunc(data, toInterface)
}

// ProcessorManager manages RTP processor lifecycle and integration
type ProcessorManager struct {
	processor PacketProcessor
	enabled   bool
}

// NewProcessorManager creates a new processor manager
func NewProcessorManager(config ProcessorConfig) *ProcessorManager {
	return &ProcessorManager{
		processor: NewPacketProcessor(config),
		enabled:   true,
	}
}

// Start starts the RTP processor
func (pm *ProcessorManager) Start() error {
	if !pm.enabled {
		return nil
	}
	return pm.processor.Start()
}

// Stop stops the RTP processor
func (pm *ProcessorManager) Stop() {
	if !pm.enabled {
		return
	}
	pm.processor.Stop()
}

// SetPacketSender sets the packet sender for the processor
func (pm *ProcessorManager) SetPacketSender(sender PacketSender) {
	if !pm.enabled {
		return
	}
	pm.processor.SetPacketSender(sender)
}

// ProcessPacket processes a packet through the RTP processor
func (pm *ProcessorManager) ProcessPacket(packet gopacket.Packet, isNearEnd bool) (shouldForward bool, err error) {
	if !pm.enabled {
		return true, nil // Forward all packets if RTP processing is disabled
	}
	return pm.processor.ProcessPacket(packet, isNearEnd)
}

// GetStats returns RTP processing statistics
func (pm *ProcessorManager) GetStats() ProcessorStats {
	if !pm.enabled {
		return ProcessorStats{}
	}
	return pm.processor.GetStats()
}

// SetEnabled enables or disables RTP processing
func (pm *ProcessorManager) SetEnabled(enabled bool) {
	pm.enabled = enabled
}

// IsEnabled returns whether RTP processing is enabled
func (pm *ProcessorManager) IsEnabled() bool {
	return pm.enabled
}

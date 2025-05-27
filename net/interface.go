package net

// PacketForwarder defines the interface for network packet forwarding
type PacketForwarder interface {
	// Start starts the packet forwarder
	Start() error

	// Stop stops the packet forwarder
	Stop()

	// GetStats returns forwarding statistics
	GetStats() ForwardingStats

	// SetDebugMode enables or disables debug mode
	SetDebugMode(enabled bool)
}

// ForwardingStats contains packet forwarding statistics
type ForwardingStats struct {
	// Interface1 to Interface2 direction stats
	If1ToIf2Stats DirectionStats `json:"if1_to_if2"`

	// Interface2 to Interface1 direction stats
	If2ToIf1Stats DirectionStats `json:"if2_to_if1"`

	// Total stats across both directions
	TotalPackets    uint64  `json:"total_packets"`
	TotalSuccess    uint64  `json:"total_success"`
	TotalErrors     uint64  `json:"total_errors"`
	OverallLossRate float64 `json:"overall_loss_rate"`
}

// DirectionStats contains statistics for one forwarding direction
type DirectionStats struct {
	SourceInterface string  `json:"source_interface"`
	DestInterface   string  `json:"dest_interface"`
	EndType         string  `json:"end_type"` // "近端(直连设备)" or "远端(MCU)"
	PacketCount     uint64  `json:"packet_count"`
	SuccessCount    uint64  `json:"success_count"`
	ErrorCount      uint64  `json:"error_count"`
	LossRate        float64 `json:"loss_rate"`
	ICMPCount       uint64  `json:"icmp_count"`
	ARPCount        uint64  `json:"arp_count"`
	OtherCount      uint64  `json:"other_count"`
}

// ForwarderConfig contains configuration for the packet forwarder
type ForwarderConfig struct {
	Interface1   string `json:"interface1"`   // Near-end interface (direct connection)
	Interface2   string `json:"interface2"`   // Far-end interface (MCU connection)
	Debug        bool   `json:"debug"`        // Enable debug mode
	If1IsNearEnd bool   `json:"if1_near_end"` // Whether interface1 is near-end
	If2IsNearEnd bool   `json:"if2_near_end"` // Whether interface2 is near-end
}

// NewPacketForwarder creates a new packet forwarder instance
func NewPacketForwarder(config ForwarderConfig) PacketForwarder {
	return NewForwarder(
		config.Interface1,
		config.Interface2,
		config.Debug,
		config.If1IsNearEnd,
		config.If2IsNearEnd,
	)
}

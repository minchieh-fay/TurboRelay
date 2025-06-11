package rtp

import (
	"net"
	"time"

	"github.com/google/gopacket"
)

// PacketProcessor defines the interface for RTP/RTCP packet processing
type PacketProcessor interface {
	// ProcessPacket processes a UDP packet and returns whether it should be forwarded
	ProcessPacket(packet gopacket.Packet, isNearEnd bool) (shouldForward bool, err error)

	// GetStats returns RTP processing statistics
	GetStats() ProcessorStats

	// SetPacketSender sets the callback function for sending packets
	SetPacketSender(sender PacketSender)

	// Start starts the RTP processor
	Start() error

	// Stop stops the RTP processor
	Stop()
}

// PacketSender defines the callback interface for sending packets
type PacketSender interface {
	// SendPacket sends a packet to the specified interface
	SendPacket(data []byte, toInterface string) error
}

// ProcessorConfig contains configuration for the RTP processor
type ProcessorConfig struct {
	NearEndInterface string        // Near-end interface name
	FarEndInterface  string        // Far-end interface name
	BufferDuration   time.Duration // Buffer duration for near-end packets (100-500ms)
	NACKTimeout      time.Duration // NACK timeout for far-end packets (10-50ms)
	Debug            bool          // Enable debug mode
}

// ProcessorStats contains RTP processing statistics
type ProcessorStats struct {
	// Total statistics
	TotalUDPPackets    uint64 `json:"total_udp_packets"`
	TotalRTPPackets    uint64 `json:"total_rtp_packets"`
	TotalRTCPPackets   uint64 `json:"total_rtcp_packets"`
	TotalNonRTPPackets uint64 `json:"total_non_rtp_packets"`

	// Near-end statistics
	NearEndStats EndpointStats `json:"near_end_stats"`

	// Far-end statistics
	FarEndStats EndpointStats `json:"far_end_stats"`

	// SSRC sessions
	ActiveSessions map[uint32]*SessionInfo `json:"active_sessions"`
}

// EndpointStats contains statistics for one endpoint
type EndpointStats struct {
	ReceivedPackets   uint64 `json:"received_packets"`
	ForwardedPackets  uint64 `json:"forwarded_packets"`
	BufferedPackets   uint64 `json:"buffered_packets"`
	DroppedPackets    uint64 `json:"dropped_packets"`
	NACKsSent         uint64 `json:"nacks_sent"`
	NACKsReceived     uint64 `json:"nacks_received"`
	OutOfOrderPackets uint64 `json:"out_of_order_packets"`
}

// SessionInfo contains information about an RTP session
type SessionInfo struct {
	SSRC      uint32    `json:"ssrc"`
	LastSeen  time.Time `json:"last_seen"`
	FirstSeen time.Time `json:"first_seen"`

	PacketCount uint64 `json:"packet_count"`

	// Sequence tracking
	MaxSeq uint16 `json:"max_seq"`

	// Network information
	RTPInfo  *StreamInfo `json:"rtp_info,omitempty"`
	RTCPInfo *StreamInfo `json:"rtcp_info,omitempty"`

	// NACK tracking - 独立跟踪正在进行NACK的序列号
	ActiveNACKs map[uint16]*NACKInfo `json:"active_nacks,omitempty"`
}

// NACKInfo tracks NACK retry information for a specific sequence number
type NACKInfo struct {
	SequenceNumber uint16    `json:"sequence_number"`
	FirstNACKTime  time.Time `json:"first_nack_time"`
	LastNACKTime   time.Time `json:"last_nack_time"`
	RetryCount     int       `json:"retry_count"`
	MaxRetries     int       `json:"max_retries"`
	GiveUpTime     time.Time `json:"give_up_time"` // 何时放弃这个NACK
	// 是否已经收到
	Received bool `json:"received"`
}

// StreamInfo contains network information for RTP or RTCP stream
type StreamInfo struct {
	SourceIP   net.IP           `json:"source_ip"`
	SourcePort uint16           `json:"source_port"`
	SourceMAC  net.HardwareAddr `json:"source_mac"`
	DestIP     net.IP           `json:"dest_ip"`
	DestPort   uint16           `json:"dest_port"`
	DestMAC    net.HardwareAddr `json:"dest_mac"`
}

// RTPHeader represents RTP header structure
type RTPHeader struct {
	Version        uint8  `json:"version"`
	Padding        bool   `json:"padding"`
	Extension      bool   `json:"extension"`
	CSRCCount      uint8  `json:"csrc_count"`
	Marker         bool   `json:"marker"`
	PayloadType    uint8  `json:"payload_type"`
	SequenceNumber uint16 `json:"sequence_number"`
	Timestamp      uint32 `json:"timestamp"`
	SSRC           uint32 `json:"ssrc"`
}

// RTCPHeader represents RTCP header structure
type RTCPHeader struct {
	Version    uint8  `json:"version"`
	Padding    bool   `json:"padding"`
	Count      uint8  `json:"count"`
	PacketType uint8  `json:"packet_type"`
	Length     uint16 `json:"length"`
}

// BufferedPacket represents a buffered RTP packet
type BufferedPacket struct {
	Data      []byte    `json:"-"`
	Header    RTPHeader `json:"header"`
	Timestamp time.Time `json:"timestamp"`
	Interface string    `json:"interface"`
}

// NewPacketProcessor creates a new RTP packet processor
func NewPacketProcessor(config ProcessorConfig) PacketProcessor {
	return NewProcessor(config)
}

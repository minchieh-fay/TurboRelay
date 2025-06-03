package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"turborelay/net"
)

func main() {
	// Command line parameters
	var (
		interface1 = flag.String("if1", "", "First network interface name (near-end, direct connection)")
		interface2 = flag.String("if2", "", "Second network interface name (far-end, MCU connection)")
		debug      = flag.Bool("debug", false, "Enable debug mode")
	)
	flag.Parse()

	// Check required parameters
	if *interface1 == "" || *interface2 == "" {
		log.Fatal("Error: Both -if1 and -if2 parameters are required")
	}

	if *interface1 == *interface2 {
		log.Fatal("Error: Interface1 and Interface2 cannot be the same")
	}

	// Create forwarder configuration
	config := net.ForwarderConfig{
		Interface1:   *interface1,
		Interface2:   *interface2,
		Debug:        *debug,
		If1IsNearEnd: true,  // if1 is always near-end
		If2IsNearEnd: false, // if2 is always far-end
	}

	log.Printf("Configuration: %s is near-end (direct connection), %s is far-end (MCU)", *interface1, *interface2)

	log.Printf("=== TurboRelay Network Packet Forwarder ===")
	log.Printf("Version: 1.0")
	log.Printf("Interface1 (near-end): %s", *interface1)
	log.Printf("Interface2 (far-end): %s", *interface2)
	log.Printf("Debug mode: %t", *debug)
	log.Printf("Function: Bidirectional packet forwarding between two network interfaces")
	log.Printf("Note: This program requires root privileges to access network interfaces")

	// Create forwarder using the interface
	forwarder := net.NewPacketForwarder(config)

	// Start forwarder
	if err := forwarder.Start(); err != nil {
		log.Fatalf("Failed to start forwarder: %v", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	log.Printf("TurboRelay is running... Press Ctrl+C to stop")

	// Wait for signal in a separate goroutine to avoid blocking
	go func() {
		if 1 == 1 {
			for {
				time.Sleep(time.Second * 10)
			}
		}
		sig := <-sigChan
		log.Printf("Received signal: %v, stopping...", sig)

		// Print final statistics before stopping
		stats := forwarder.GetStats()
		log.Printf("=== Final Statistics ===")
		log.Printf("Total packets: %d, Success: %d, Errors: %d, Loss rate: %.2f%%",
			stats.TotalPackets, stats.TotalSuccess, stats.TotalErrors, stats.OverallLossRate)
		log.Printf("If1->If2: %d packets, %d success, %d errors, %.2f%% loss",
			stats.If1ToIf2Stats.PacketCount, stats.If1ToIf2Stats.SuccessCount,
			stats.If1ToIf2Stats.ErrorCount, stats.If1ToIf2Stats.LossRate)
		log.Printf("If2->If1: %d packets, %d success, %d errors, %.2f%% loss",
			stats.If2ToIf1Stats.PacketCount, stats.If2ToIf1Stats.SuccessCount,
			stats.If2ToIf1Stats.ErrorCount, stats.If2ToIf1Stats.LossRate)

		forwarder.Stop()
		log.Printf("TurboRelay stopped successfully")
		os.Exit(0)
	}()

	// Keep main goroutine alive
	select {}
}

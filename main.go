package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

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

	// Fixed configuration: if1 is always near-end, if2 is always far-end
	if1IsNearEnd := true
	if2IsNearEnd := false
	log.Printf("Configuration: %s is near-end (direct connection), %s is far-end (MCU)", *interface1, *interface2)

	log.Printf("=== TurboRelay Network Packet Forwarder ===")
	log.Printf("Version: 1.0")
	log.Printf("Interface1 (near-end): %s", *interface1)
	log.Printf("Interface2 (far-end): %s", *interface2)
	log.Printf("Debug mode: %t", *debug)
	log.Printf("Function: Bidirectional packet forwarding between two network interfaces")
	log.Printf("Note: This program requires root privileges to access network interfaces")

	// Create forwarder
	forwarder := net.NewForwarder(*interface1, *interface2, *debug, if1IsNearEnd, if2IsNearEnd)

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
		sig := <-sigChan
		log.Printf("Received signal: %v, stopping...", sig)
		forwarder.Stop()
		log.Printf("TurboRelay stopped successfully")
		os.Exit(0)
	}()

	// Keep main goroutine alive
	select {}
}

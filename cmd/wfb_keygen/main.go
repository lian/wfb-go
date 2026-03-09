// wfb_keygen generates WFB-NG compatible keypairs.
//
// Usage:
//
//	wfb_keygen [options]
//
// Examples:
//
//	wfb_keygen                      # Generate random keys: drone.key, gs.key
//	wfb_keygen -o /etc/wfb          # Output to /etc/wfb/drone.key, /etc/wfb/gs.key
//	wfb_keygen -p "mysecretpassword" # Derive keys from password
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lian/wfb-go/pkg/crypto"
	"github.com/lian/wfb-go/pkg/version"
)

func main() {
	var (
		outputDir   string
		password    string
		showVersion bool
		showHex     bool
	)

	flag.StringVar(&outputDir, "o", ".", "Output directory for key files")
	flag.StringVar(&password, "p", "", "Derive keys from password (Argon2i)")
	flag.BoolVar(&showHex, "hex", false, "Also print keys in hex format")
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_keygen - WFB-NG Key Generator\n\n")
		fmt.Fprintf(os.Stderr, "Generates matched keypairs for drone (TX) and ground station (GS/RX).\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s                        # Generate random keys in current directory\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -o /etc/wfb            # Output to /etc/wfb/\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -p \"secret\"            # Derive from password (reproducible)\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nKey files:\n")
		fmt.Fprintf(os.Stderr, "  drone.key  - Use with wfb_tx on the drone/vehicle\n")
		fmt.Fprintf(os.Stderr, "  gs.key     - Use with wfb_rx on the ground station\n")
		fmt.Fprintf(os.Stderr, "\nFile format (64 bytes each):\n")
		fmt.Fprintf(os.Stderr, "  drone.key = drone_secret_key (32) + gs_public_key (32)\n")
		fmt.Fprintf(os.Stderr, "  gs.key    = gs_secret_key (32) + drone_public_key (32)\n")
		fmt.Fprintf(os.Stderr, "\nVersion: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_keygen %s\n", version.String())
		os.Exit(0)
	}

	// Generate or derive keys
	var droneKey, gsKey []byte
	var err error

	if password != "" {
		fmt.Printf("Deriving keys from password using Argon2i...\n")
		droneKey, gsKey, err = crypto.DeriveKeysFromPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deriving keys: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Generating random keypairs...\n")
		droneKey, gsKey, err = crypto.GenerateWFBKeys()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating keys: %v\n", err)
			os.Exit(1)
		}
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Write key files
	droneKeyPath := filepath.Join(outputDir, "drone.key")
	gsKeyPath := filepath.Join(outputDir, "gs.key")

	if err := os.WriteFile(droneKeyPath, droneKey, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing drone.key: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(gsKeyPath, gsKey, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing gs.key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nKeys generated successfully:\n")
	fmt.Printf("  %s (drone/TX)\n", droneKeyPath)
	fmt.Printf("  %s (ground station/RX)\n", gsKeyPath)

	if showHex {
		fmt.Printf("\nHex values:\n")
		fmt.Printf("  drone.key:\n")
		fmt.Printf("    secret_key: %s\n", hex.EncodeToString(droneKey[0:32]))
		fmt.Printf("    gs_pubkey:  %s\n", hex.EncodeToString(droneKey[32:64]))
		fmt.Printf("  gs.key:\n")
		fmt.Printf("    secret_key:   %s\n", hex.EncodeToString(gsKey[0:32]))
		fmt.Printf("    drone_pubkey: %s\n", hex.EncodeToString(gsKey[32:64]))
	}

	fmt.Printf("\nUsage:\n")
	fmt.Printf("  Drone:  wfb_tx -K %s ...\n", droneKeyPath)
	fmt.Printf("  GS:     wfb_rx -K %s ...\n", gsKeyPath)
}

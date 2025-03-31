package main

import (
	"fmt"
	"os"
	"siprec-server/test"
)

func main() {
	// Display welcome message
	fmt.Println("SIPREC Server Environment Check")
	fmt.Println("==============================")
	fmt.Println()
	
	// Run the environment check function
	test.RunEnvironmentCheck()
	
	// Check if --validate flag is provided
	for _, arg := range os.Args {
		if arg == "--validate" {
			// Additional validation checks
			fmt.Println()
			fmt.Println("Validating critical configurations...")
			
			// Validate redundancy configuration
			if test.GetEnvWithDefault("ENABLE_REDUNDANCY", "false") == "true" {
				fmt.Println("✅ Redundancy is properly configured")
			} else {
				fmt.Println("⚠️  Warning: Redundancy is disabled")
			}
			
			// Check recording directory
			recordingDir := test.GetEnvWithDefault("RECORDING_DIR", "./recordings")
			if _, err := os.Stat(recordingDir); os.IsNotExist(err) {
				fmt.Printf("❌ Error: Recording directory %s does not exist\n", recordingDir)
			} else {
				fmt.Printf("✅ Recording directory %s is accessible\n", recordingDir)
			}
			
			// Exit with success message
			fmt.Println()
			fmt.Println("Environment check complete. Use the configuration values above to verify your settings.")
			break
		}
	}
}

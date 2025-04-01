// Package main provides a test launcher for all individual tests
package main

import (
	"fmt"
	"os"
	
	"siprec-server/test_audio"
	"siprec-server/test_sipgo"
	"siprec-server/test_siprec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Available tests:")
		fmt.Println("  audio  - Test audio processing")
		fmt.Println("  sipgo  - Test sipgo API")
		fmt.Println("  siprec - Test SIPREC implementation")
		fmt.Println("  all    - Run all tests")
		fmt.Println("\nUsage: go run run_tests.go [test]")
		return
	}
	
	testType := os.Args[1]
	
	switch testType {
	case "audio":
		fmt.Println("Running audio processing test...")
		test_audio.TestAudioProcessing()
		
	case "sipgo":
		fmt.Println("Running sipgo API test...")
		test_sipgo.TestSipGo()
		
	case "siprec":
		fmt.Println("Running SIPREC implementation test...")
		test_siprec.TestSiprec()
		
	case "all":
		fmt.Println("Running all tests...")
		
		fmt.Println("\n=== SIPGO TEST ===")
		test_sipgo.TestSipGo()
		
		fmt.Println("\n=== SIPREC TEST ===")
		test_siprec.TestSiprec()
		
		fmt.Println("\n=== AUDIO TEST ===")
		test_audio.TestAudioProcessing()
		
	default:
		fmt.Printf("Unknown test type: %s\n", testType)
		fmt.Println("Available tests: audio, sipgo, siprec, all")
	}
}
// Package main provides a test launcher for all individual tests
// This script uses os/exec to run individual test files
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Available tests:")
		fmt.Println("  sipgo  - Test sipgo API")
		fmt.Println("  siprec - Test SIPREC implementation")
		fmt.Println("  audio  - Test audio processing (requires UDP port 15000)")
		fmt.Println("  all    - Run all tests")
		fmt.Println("\nUsage: go run run_tests.go [test]")
		return
	}
	
	testType := os.Args[1]
	
	// Get current directory for test file paths
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Failed to get current directory: %v\n", err)
		os.Exit(1)
	}
	
	// Temporary directory to store modified test files
	tmpDir := filepath.Join(currentDir, "tmp_tests")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir) // Clean up when done
	
	switch testType {
	case "sipgo":
		fmt.Println("Running sipgo API test...")
		runSipgoTest(currentDir, tmpDir)
		
	case "siprec":
		fmt.Println("Running SIPREC implementation test...")
		runSiprecTest(currentDir, tmpDir)
		
	case "audio":
		fmt.Println("Running audio processing test...")
		runAudioTest(currentDir, tmpDir)
		
	case "all":
		fmt.Println("Running all tests...")
		
		fmt.Println("\n=== SIPGO TEST ===")
		runSipgoTest(currentDir, tmpDir)
		
		fmt.Println("\n=== SIPREC TEST ===")
		runSiprecTest(currentDir, tmpDir)
		
		fmt.Println("\n=== AUDIO TEST ===")
		runAudioTest(currentDir, tmpDir)
		
	default:
		fmt.Printf("Unknown test type: %s\n", testType)
		fmt.Println("Available tests: sipgo, siprec, audio, all")
	}
}

// runSipgoTest runs the sipgo API test
func runSipgoTest(baseDir, tmpDir string) {
	// Create temporary test file
	testFile := filepath.Join(tmpDir, "test_sipgo_main.go")
	
	// Copy test_sipgo.go to temporary file with package main
	cmd := exec.Command("bash", "-c", fmt.Sprintf("sed 's/package test_sipgo/package main/' %s/test_sipgo.go | sed 's/func TestSipGo/func main/' > %s", 
		baseDir, testFile))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to prepare sipgo test: %v\n", err)
		return
	}
	
	// Run the test
	runCmd := exec.Command("go", "run", testFile)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Sipgo test failed: %v\n", err)
	}
}

// runSiprecTest runs the SIPREC implementation test
func runSiprecTest(baseDir, tmpDir string) {
	// Create temporary test file
	testFile := filepath.Join(tmpDir, "test_siprec_main.go")
	
	// Copy test_siprec.go to temporary file with package main
	cmd := exec.Command("bash", "-c", fmt.Sprintf("sed 's/package test_siprec/package main/' %s/test_siprec.go | sed 's/func TestSiprec/func main/' > %s", 
		baseDir, testFile))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to prepare siprec test: %v\n", err)
		return
	}
	
	// Run the test
	runCmd := exec.Command("go", "run", testFile)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	
	if err := runCmd.Run(); err != nil {
		fmt.Printf("SIPREC test failed: %v\n", err)
	}
}

// runAudioTest runs the audio processing test
func runAudioTest(baseDir, tmpDir string) {
	// Create temporary test file
	testFile := filepath.Join(tmpDir, "test_audio_main.go")
	
	// Copy test_audio.go to temporary file with package main
	cmd := exec.Command("bash", "-c", fmt.Sprintf("sed 's/package test_audio/package main/' %s/test_audio.go | sed 's/func TestAudioProcessing/func main/' > %s", 
		baseDir, testFile))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to prepare audio test: %v\n", err)
		return
	}
	
	// Run the test
	runCmd := exec.Command("go", "run", testFile)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Audio test failed: %v\n", err)
	}
}
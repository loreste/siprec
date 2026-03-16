package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var pauseCmd = &cobra.Command{
	Use:   "pause <call-id>",
	Short: "Pause recording for a call",
	Long: `Pause the recording and transcription for a specific call.
The call continues but audio is not recorded until resumed.`,
	Args: cobra.ExactArgs(1),
	Run:  runPause,
}

var resumeCmd = &cobra.Command{
	Use:   "resume <call-id>",
	Short: "Resume recording for a call",
	Long:  `Resume recording and transcription for a previously paused call.`,
	Args:  cobra.ExactArgs(1),
	Run:   runResume,
}

var statusCmd = &cobra.Command{
	Use:   "status <call-id>",
	Short: "Get recording status for a call",
	Args:  cobra.ExactArgs(1),
	Run:   runStatus,
}

var pauseAllCmd = &cobra.Command{
	Use:   "pause-all",
	Short: "Pause all active recordings",
	Run:   runPauseAll,
}

var resumeAllCmd = &cobra.Command{
	Use:   "resume-all",
	Short: "Resume all paused recordings",
	Run:   runResumeAll,
}

func init() {
	rootCmd.AddCommand(pauseAllCmd)
	rootCmd.AddCommand(resumeAllCmd)
}

func runPause(cmd *cobra.Command, args []string) {
	callID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Post(ctx, "/api/pause/"+callID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	fmt.Printf("Recording paused for call %s\n", callID)
}

func runResume(cmd *cobra.Command, args []string) {
	callID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Post(ctx, "/api/resume/"+callID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	fmt.Printf("Recording resumed for call %s\n", callID)
}

func runStatus(cmd *cobra.Command, args []string) {
	callID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/status/"+callID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	var statusResp map[string]interface{}
	if err := json.Unmarshal(body, &statusResp); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Call: %s\n", callID)
	paused := "No"
	if p, ok := statusResp["paused"].(bool); ok && p {
		paused = "Yes"
	}
	fmt.Printf("Paused: %s\n", paused)
	if recording, ok := statusResp["recording"].(bool); ok {
		if recording {
			fmt.Println("Recording: Active")
		} else {
			fmt.Println("Recording: Inactive")
		}
	}
	if transcription, ok := statusResp["transcription"].(bool); ok {
		if transcription {
			fmt.Println("Transcription: Active")
		} else {
			fmt.Println("Transcription: Inactive")
		}
	}
}

func runPauseAll(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Post(ctx, "/api/pause/all", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err == nil {
		if count, ok := resp["paused_count"]; ok {
			fmt.Printf("Paused %v recordings\n", count)
			return
		}
	}
	fmt.Println("All recordings paused")
}

func runResumeAll(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Post(ctx, "/api/resume/all", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err == nil {
		if count, ok := resp["resumed_count"]; ok {
			fmt.Printf("Resumed %v recordings\n", count)
			return
		}
	}
	fmt.Println("All recordings resumed")
}

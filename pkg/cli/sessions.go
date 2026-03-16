package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage recording sessions",
	Long:  `List, view, and manage active recording sessions.`,
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	Run:   runSessionsList,
}

var sessionsGetCmd = &cobra.Command{
	Use:   "get <session-id>",
	Short: "Get session details",
	Args:  cobra.ExactArgs(1),
	Run:   runSessionsGet,
}

var sessionsTerminateCmd = &cobra.Command{
	Use:   "terminate <session-id>",
	Short: "Terminate a session",
	Args:  cobra.ExactArgs(1),
	Run:   runSessionsTerminate,
}

func init() {
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsGetCmd)
	sessionsCmd.AddCommand(sessionsTerminateCmd)
}

// Session represents a recording session
type Session struct {
	ID          string `json:"id"`
	CallID      string `json:"call_id"`
	SIPCallID   string `json:"sip_call_id"`
	State       string `json:"state"`
	StartTime   string `json:"start_time"`
	Duration    string `json:"duration"`
	CallerURI   string `json:"caller_uri"`
	CalleeURI   string `json:"callee_uri"`
	RecordingID string `json:"recording_id"`
	Paused      bool   `json:"paused"`
}

func runSessionsList(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/sessions")
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

	var sessions []Session
	if err := json.Unmarshal(body, &sessions); err != nil {
		// Try parsing as object with sessions array
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
		if sessArr, ok := resp["sessions"]; ok {
			sessJSON, _ := json.Marshal(sessArr)
			json.Unmarshal(sessJSON, &sessions)
		}
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCALL-ID\tSTATE\tDURATION\tPAUSED")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, s := range sessions {
		paused := ""
		if s.Paused {
			paused = "YES"
		}
		id := s.ID
		if len(id) > 20 {
			id = id[:20] + "..."
		}
		callID := s.CallID
		if callID == "" {
			callID = s.SIPCallID
		}
		if len(callID) > 25 {
			callID = callID[:25] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, callID, s.State, s.Duration, paused)
	}
	w.Flush()

	fmt.Printf("\nTotal: %d sessions\n", len(sessions))
}

func runSessionsGet(cmd *cobra.Command, args []string) {
	sessionID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/sessions/"+sessionID)
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

	var session map[string]interface{}
	if err := json.Unmarshal(body, &session); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session: %s\n", sessionID)
	fmt.Println(strings.Repeat("=", 50))
	for key, value := range session {
		fmt.Printf("%-20s %v\n", key+":", value)
	}
}

func runSessionsTerminate(cmd *cobra.Command, args []string) {
	sessionID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Delete(ctx, "/api/sessions/"+sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 && status != 204 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	fmt.Printf("Session %s terminated\n", sessionID)
}

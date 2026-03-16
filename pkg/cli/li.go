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

var liCmd = &cobra.Command{
	Use:   "li",
	Short: "Lawful intercept management",
	Long: `Manage lawful intercept operations including registering intercepts,
viewing active intercepts, and querying audit logs.

Note: These commands require the lawful intercept module to be enabled
on the SIPREC server.`,
}

var liListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active intercepts",
	Run:   runLIList,
}

var liGetCmd = &cobra.Command{
	Use:   "get <intercept-id>",
	Short: "Get intercept details",
	Args:  cobra.ExactArgs(1),
	Run:   runLIGet,
}

var liRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new intercept",
	Run:   runLIRegister,
}

var liRevokeCmd = &cobra.Command{
	Use:   "revoke <intercept-id>",
	Short: "Revoke an intercept",
	Args:  cobra.ExactArgs(1),
	Run:   runLIRevoke,
}

var liStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show intercept statistics",
	Run:   runLIStats,
}

var liAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query audit logs",
	Run:   runLIAudit,
}

var (
	liWarrantID  string
	liTargetID   string
	liTargetType string
	liReason     string
	liAuditLimit int
)

func init() {
	liCmd.AddCommand(liListCmd)
	liCmd.AddCommand(liGetCmd)
	liCmd.AddCommand(liRegisterCmd)
	liCmd.AddCommand(liRevokeCmd)
	liCmd.AddCommand(liStatsCmd)
	liCmd.AddCommand(liAuditCmd)

	// Register flags
	liRegisterCmd.Flags().StringVar(&liWarrantID, "warrant", "", "Warrant ID (required)")
	liRegisterCmd.Flags().StringVar(&liTargetID, "target", "", "Target identifier (required)")
	liRegisterCmd.Flags().StringVar(&liTargetType, "type", "phone", "Target type (phone, uri, ip)")
	liRegisterCmd.MarkFlagRequired("warrant")
	liRegisterCmd.MarkFlagRequired("target")

	// Revoke flags
	liRevokeCmd.Flags().StringVar(&liReason, "reason", "", "Revocation reason")

	// Audit flags
	liAuditCmd.Flags().IntVar(&liAuditLimit, "limit", 100, "Maximum entries to return")
}

// Intercept represents a lawful intercept
type Intercept struct {
	ID               string `json:"id"`
	WarrantID        string `json:"warrant_id"`
	TargetID         string `json:"target_id"`
	TargetType       string `json:"target_type"`
	Status           string `json:"status"`
	StartTime        string `json:"start_time"`
	EndTime          string `json:"end_time,omitempty"`
	CallsIntercepted int64  `json:"calls_intercepted"`
	BytesDelivered   int64  `json:"bytes_delivered"`
}

func runLIList(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/li/intercepts")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status == 404 {
		fmt.Println("Lawful intercept module not enabled")
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

	var intercepts []Intercept
	if err := json.Unmarshal(body, &intercepts); err != nil {
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
		if arr, ok := resp["intercepts"]; ok {
			arrJSON, _ := json.Marshal(arr)
			json.Unmarshal(arrJSON, &intercepts)
		}
	}

	if len(intercepts) == 0 {
		fmt.Println("No active intercepts")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tWARRANT\tTARGET\tTYPE\tSTATUS\tCALLS")
	fmt.Fprintln(w, strings.Repeat("-", 90))
	for _, i := range intercepts {
		id := i.ID
		if len(id) > 15 {
			id = id[:15] + "..."
		}
		warrant := i.WarrantID
		if len(warrant) > 15 {
			warrant = warrant[:15] + "..."
		}
		target := i.TargetID
		if len(target) > 20 {
			target = target[:20] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
			id, warrant, target, i.TargetType, i.Status, i.CallsIntercepted)
	}
	w.Flush()

	fmt.Printf("\nTotal: %d intercepts\n", len(intercepts))
}

func runLIGet(cmd *cobra.Command, args []string) {
	interceptID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/li/intercepts/"+interceptID)
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

	var intercept map[string]interface{}
	if err := json.Unmarshal(body, &intercept); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Intercept: %s\n", interceptID)
	fmt.Println(strings.Repeat("=", 50))
	for key, value := range intercept {
		fmt.Printf("%-20s %v\n", key+":", value)
	}
}

func runLIRegister(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	reqBody := map[string]interface{}{
		"warrant_id":  liWarrantID,
		"target_id":   liTargetID,
		"target_type": liTargetType,
	}

	body, status, err := client.Post(ctx, "/api/li/intercepts", reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 && status != 201 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err == nil {
		if id, ok := resp["id"]; ok {
			fmt.Printf("Intercept registered: %v\n", id)
			return
		}
	}
	fmt.Println("Intercept registered successfully")
}

func runLIRevoke(cmd *cobra.Command, args []string) {
	interceptID := args[0]
	client := NewClient(serverURL)
	ctx := context.Background()

	reqBody := map[string]interface{}{}
	if liReason != "" {
		reqBody["reason"] = liReason
	}

	body, status, err := client.Post(ctx, "/api/li/intercepts/"+interceptID+"/revoke", reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", ParseError(body))
		os.Exit(1)
	}

	fmt.Printf("Intercept %s revoked\n", interceptID)
}

func runLIStats(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/api/li/stats")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status == 404 {
		fmt.Println("Lawful intercept module not enabled")
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

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Lawful Intercept Statistics")
	fmt.Println(strings.Repeat("=", 40))
	for key, value := range stats {
		displayKey := strings.ReplaceAll(key, "_", " ")
		displayKey = strings.Title(displayKey)
		fmt.Printf("%-25s %v\n", displayKey+":", value)
	}
}

func runLIAudit(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	path := fmt.Sprintf("/api/li/audit?limit=%d", liAuditLimit)
	body, status, err := client.Get(ctx, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status == 404 {
		fmt.Println("Lawful intercept module not enabled")
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

	var entries []map[string]interface{}
	if err := json.Unmarshal(body, &entries); err != nil {
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
		if arr, ok := resp["entries"]; ok {
			arrJSON, _ := json.Marshal(arr)
			json.Unmarshal(arrJSON, &entries)
		}
	}

	if len(entries) == 0 {
		fmt.Println("No audit entries")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tEVENT\tWARRANT\tDESCRIPTION")
	fmt.Fprintln(w, strings.Repeat("-", 100))
	for _, e := range entries {
		ts := ""
		if v, ok := e["timestamp"].(string); ok {
			if len(v) > 19 {
				ts = v[:19]
			} else {
				ts = v
			}
		}
		event := ""
		if v, ok := e["event_type"].(string); ok {
			event = v
		}
		warrant := ""
		if v, ok := e["warrant_id"].(string); ok {
			warrant = v
			if len(warrant) > 15 {
				warrant = warrant[:15] + "..."
			}
		}
		desc := ""
		if v, ok := e["description"].(string); ok {
			desc = v
			if len(desc) > 40 {
				desc = desc[:40] + "..."
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, event, warrant, desc)
	}
	w.Flush()

	fmt.Printf("\nShowing %d entries\n", len(entries))
}

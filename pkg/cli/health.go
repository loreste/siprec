package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check server health",
	Long:  `Check the health status of the SIPREC server and its dependencies.`,
	Run:   runHealth,
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show server statistics",
	Long:  `Display statistics about active sessions, recordings, and resource usage.`,
	Run:   runStats,
}

func runHealth(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if outputJSON {
		fmt.Println(string(body))
		return
	}

	if status == 200 {
		fmt.Println("Status: HEALTHY")
	} else {
		fmt.Printf("Status: UNHEALTHY (HTTP %d)\n", status)
	}

	var health map[string]interface{}
	if err := json.Unmarshal(body, &health); err == nil {
		if checks, ok := health["checks"].(map[string]interface{}); ok {
			fmt.Println("\nDependency Checks:")
			for name, result := range checks {
				status := "OK"
				if resultMap, ok := result.(map[string]interface{}); ok {
					if s, ok := resultMap["status"].(string); ok {
						status = strings.ToUpper(s)
					}
				}
				fmt.Printf("  %-20s %s\n", name+":", status)
			}
		}
	}
}

func runStats(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	body, status, err := client.Get(ctx, "/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if status != 200 {
		fmt.Fprintf(os.Stderr, "Error: Server returned HTTP %d\n", status)
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

	fmt.Println("SIPREC Server Statistics")
	fmt.Println(strings.Repeat("=", 40))

	if v, ok := stats["version"]; ok {
		fmt.Printf("Version:          %v\n", v)
	}
	if v, ok := stats["uptime"]; ok {
		fmt.Printf("Uptime:           %v\n", v)
	}
	if v, ok := stats["active_sessions"]; ok {
		fmt.Printf("Active Sessions:  %v\n", v)
	}
	if v, ok := stats["total_sessions"]; ok {
		fmt.Printf("Total Sessions:   %v\n", v)
	}

	// Try to get metrics
	metricsBody, metricsStatus, err := client.Get(ctx, "/metrics")
	if err == nil && metricsStatus == 200 {
		if verbose {
			fmt.Println("\nPrometheus Metrics:")
			fmt.Println(strings.Repeat("-", 40))
			// Parse key metrics from Prometheus format
			lines := strings.Split(string(metricsBody), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "siprec_") && !strings.HasPrefix(line, "#") {
					fmt.Printf("  %s\n", line)
				}
			}
		}
	}
}

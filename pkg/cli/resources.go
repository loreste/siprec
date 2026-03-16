package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var resourcesCmd = &cobra.Command{
	Use:   "resources",
	Short: "Show resource usage",
	Long:  `Display current resource utilization including memory, CPU, and connection limits.`,
	Run:   runResources,
}

func runResources(cmd *cobra.Command, args []string) {
	client := NewClient(serverURL)
	ctx := context.Background()

	// Try to get status which may include resource info
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

	fmt.Println("Resource Usage")
	fmt.Println(strings.Repeat("=", 50))

	// Sessions
	if v, ok := stats["active_sessions"]; ok {
		fmt.Printf("Active Sessions:    %v\n", v)
	}

	// Memory (try to get from metrics)
	metricsBody, metricsStatus, _ := client.Get(ctx, "/metrics")
	if metricsStatus == 200 {
		metrics := parsePrometheusMetrics(string(metricsBody))

		if v, ok := metrics["go_memstats_alloc_bytes"]; ok {
			mb := v / (1024 * 1024)
			fmt.Printf("Memory Allocated:   %.1f MB\n", mb)
		}
		if v, ok := metrics["go_memstats_sys_bytes"]; ok {
			mb := v / (1024 * 1024)
			fmt.Printf("Memory System:      %.1f MB\n", mb)
		}
		if v, ok := metrics["go_goroutines"]; ok {
			fmt.Printf("Goroutines:         %.0f\n", v)
		}
		if v, ok := metrics["go_threads"]; ok {
			fmt.Printf("OS Threads:         %.0f\n", v)
		}

		// SIPREC specific metrics
		fmt.Println("\nSIPREC Metrics:")
		fmt.Println(strings.Repeat("-", 50))

		titleCaser := cases.Title(language.English)
		for key, value := range metrics {
			if strings.HasPrefix(key, "siprec_") {
				displayKey := strings.TrimPrefix(key, "siprec_")
				displayKey = strings.ReplaceAll(displayKey, "_", " ")
				displayKey = titleCaser.String(displayKey)
				fmt.Printf("%-30s %.0f\n", displayKey+":", value)
			}
		}
	}
}

func parsePrometheusMetrics(data string) map[string]float64 {
	metrics := make(map[string]float64)
	lines := strings.Split(data, "\n")

	for _, line := range lines {
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// Parse metric line: metric_name{labels} value
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[0]
			// Remove labels if present
			if idx := strings.Index(name, "{"); idx != -1 {
				name = name[:idx]
			}

			var value float64
			fmt.Sscanf(parts[len(parts)-1], "%f", &value)
			metrics[name] = value
		}
	}

	return metrics
}

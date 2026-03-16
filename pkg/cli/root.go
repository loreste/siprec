package cli

import (
	"fmt"
	"os"

	"siprec-server/pkg/version"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	serverURL  string
	outputJSON bool
	verbose    bool
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "siprecctl",
	Short: "CLI for IZI SIPREC Server",
	Long: `siprecctl is a command-line interface for managing the IZI SIPREC
Session Recording Server. It provides commands for session management,
recording control, health checks, and administrative operations.

Examples:
  siprecctl health              # Check server health
  siprecctl sessions list       # List active sessions
  siprecctl pause <call-id>     # Pause a recording
  siprecctl stats               # View server statistics`,
	Version: version.Version,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", getEnvOrDefault("SIPREC_SERVER", "http://localhost:8080"), "SIPREC server URL")
	rootCmd.PersistentFlags().BoolVarP(&outputJSON, "json", "j", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	// Add subcommands
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(resourcesCmd)
	rootCmd.AddCommand(liCmd)
	rootCmd.AddCommand(versionCmd)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// versionCmd shows version info
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("siprecctl version %s\n", version.Version)
	},
}

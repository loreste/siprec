package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"siprec-server/pkg/config"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long:  `Validate, generate, and manage configuration files.`,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate [config-file]",
	Short: "Validate a configuration file",
	Args:  cobra.MaximumNArgs(1),
	Run:   runConfigValidate,
}

var configGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate example configuration",
	Run:   runConfigGenerate,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Run:   runConfigShow,
}

var (
	configFormat string
	configOutput string
)

func init() {
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configGenerateCmd)
	configCmd.AddCommand(configShowCmd)

	configGenerateCmd.Flags().StringVarP(&configFormat, "format", "f", "yaml", "Output format (yaml, json)")
	configGenerateCmd.Flags().StringVarP(&configOutput, "output", "o", "", "Output file (default: stdout)")
}

func runConfigValidate(cmd *cobra.Command, args []string) {
	// Create a silent logger for validation
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Try to load config
	cfg, err := config.LoadConfig(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration invalid: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuration valid")
	if verbose {
		fmt.Printf("SIP Ports: %v\n", cfg.Ports)
		fmt.Printf("HTTP Port: %d\n", cfg.HTTPPort)
		fmt.Printf("Recording Directory: %s\n", cfg.RecordingDir)
		fmt.Printf("External IP: %s\n", cfg.ExternalIP)
	}
}

func runConfigGenerate(cmd *cobra.Command, args []string) {
	// Create example config with comments
	exampleConfig := map[string]interface{}{
		"network": map[string]interface{}{
			"host":      "0.0.0.0",
			"ports":     []int{5060},
			"http_port": 8080,
			"tls": map[string]interface{}{
				"enabled":   false,
				"cert_file": "/etc/siprec/tls/cert.pem",
				"key_file":  "/etc/siprec/tls/key.pem",
			},
		},
		"recording": map[string]interface{}{
			"directory":            "./recordings",
			"format":               "wav",
			"combine_legs":         true,
			"encryption_enabled":   false,
			"encryption_algorithm": "aes-256-gcm",
		},
		"rtp": map[string]interface{}{
			"port_min": 10000,
			"port_max": 20000,
			"timeout":  "30s",
		},
		"stt": map[string]interface{}{
			"enabled":            false,
			"default_vendor":     "google",
			"supported_vendors":  []string{"google", "deepgram"},
			"streaming_enabled":  true,
			"interim_results":    true,
		},
		"storage": map[string]interface{}{
			"enabled": false,
			"s3": map[string]interface{}{
				"enabled": false,
				"bucket":  "",
				"region":  "us-east-1",
			},
		},
		"resources": map[string]interface{}{
			"max_concurrent_calls": 500,
			"max_rtp_streams":      1500,
			"worker_pool_size":     0,
			"max_memory_mb":        0,
		},
		"lawful_intercept": map[string]interface{}{
			"enabled":           false,
			"delivery_endpoint": "",
			"audit_log_path":    "/var/log/siprec/li_audit.log",
			"mutual_tls":        true,
		},
	}

	var output []byte
	var err error

	switch configFormat {
	case "json":
		output, err = json.MarshalIndent(exampleConfig, "", "  ")
	default:
		output, err = yaml.Marshal(exampleConfig)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating config: %v\n", err)
		os.Exit(1)
	}

	if configOutput != "" {
		dir := filepath.Dir(configOutput)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(configOutput, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Configuration written to %s\n", configOutput)
	} else {
		fmt.Println(string(output))
	}
}

func runConfigShow(cmd *cobra.Command, args []string) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg, err := config.LoadConfig(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	var output []byte
	if outputJSON {
		output, err = json.MarshalIndent(cfg, "", "  ")
	} else {
		output, err = yaml.Marshal(cfg)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}

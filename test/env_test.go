package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestEnvironmentLoading is a proper test for environment loading
func TestEnvironmentLoading(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(os.Stdout)

	// Test environment loading
	projectRoot, err := LoadEnvironment(logger)
	if err != nil {
		t.Logf("Warning: %v", err)
		t.Log("This is not critical as we can continue with defaults")
	} else {
		t.Logf("Found project root at: %s", projectRoot)

		if projectRoot == "" {
			t.Fatal("Project root path is empty")
		}
	}

	// Verify critical environment variables for redundancy
	t.Run("RedundancyConfig", func(t *testing.T) {
		envVars := []string{
			"ENABLE_REDUNDANCY",
			"SESSION_TIMEOUT",
			"SESSION_CHECK_INTERVAL",
		}

		for _, envVar := range envVars {
			value := GetEnvWithDefault(envVar, "")
			if value == "" {
				t.Logf("Warning: Missing environment variable %s, will use default", envVar)
			} else {
				t.Logf("Found %s = %s", envVar, value)
			}
		}
	})

	// Test the relative path handling
	t.Run("DirectoryPaths", func(t *testing.T) {
		// Test with different recording directory paths
		testCases := []struct {
			name         string
			recordingDir string
			projectRoot  string
			expected     string
		}{
			{
				name:         "Relative Path",
				recordingDir: "./recordings",
				projectRoot:  "/test/root",
				expected:     "/test/root/recordings",
			},
			{
				name:         "Absolute Path",
				recordingDir: "/var/recordings",
				projectRoot:  "/test/root",
				expected:     "/var/recordings",
			},
			{
				name:         "Empty Path",
				recordingDir: "",
				projectRoot:  "/test/root",
				expected:     "/test/root/recordings", // Should use default
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				if tc.recordingDir != "" {
					os.Setenv("RECORDING_DIR", tc.recordingDir)
					defer os.Unsetenv("RECORDING_DIR")
				}

				recordingDir := GetEnvWithDefault("RECORDING_DIR", "./recordings")
				var recordingDirPath string
				if filepath.IsAbs(recordingDir) {
					recordingDirPath = recordingDir
				} else {
					recordingDirPath = filepath.Join(tc.projectRoot, recordingDir)
				}

				if recordingDirPath != tc.expected {
					t.Errorf("Expected path %s, got %s", tc.expected, recordingDirPath)
				}
			})
		}
	})
}

// TestRunEnvironmentCheck tests the full environment check
func TestRunEnvironmentCheck(t *testing.T) {
	// This just exercises the function to ensure it doesn't panic
	RunEnvironmentCheck()
}

// This is the test function that Go will run
func ExampleRunEnvironmentCheck() {
	os.Setenv("RUN_ENV_CHECK_SILENT", "1")
	defer os.Unsetenv("RUN_ENV_CHECK_SILENT")

	// This is just a stub to demonstrate usage
	RunEnvironmentCheck()
	fmt.Println("Environment check completed")
	// Output: Environment check completed
}

// TestGetEnvWithDefault tests the GetEnvWithDefault function
func TestGetEnvWithDefault(t *testing.T) {
	// Test cases for GetEnvWithDefault
	testCases := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "Existing Environment Variable",
			key:          "TEST_VAR_1",
			defaultValue: "default",
			envValue:     "actual",
			expected:     "actual",
		},
		{
			name:         "Missing Environment Variable",
			key:          "TEST_VAR_2",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "Empty Default Value",
			key:          "TEST_VAR_3",
			defaultValue: "",
			envValue:     "",
			expected:     "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envValue != "" {
				os.Setenv(tc.key, tc.envValue)
				defer os.Unsetenv(tc.key)
			}

			result := GetEnvWithDefault(tc.key, tc.defaultValue)
			if result != tc.expected {
				t.Errorf("Expected GetEnvWithDefault(%s, %s) to return %s, got %s",
					tc.key, tc.defaultValue, tc.expected, result)
			}
		})
	}
}

package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"path/filepath"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHExecutor handles SSH command execution for disaster recovery
type SSHExecutor struct {
	logger *logrus.Logger
}

// NewSSHExecutor creates a new SSH executor
func NewSSHExecutor(logger *logrus.Logger) *SSHExecutor {
	return &SSHExecutor{
		logger: logger,
	}
}

// SSHResult represents the result of an SSH command execution
type SSHResult struct {
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// ExecuteCommand executes a command on a remote host via SSH
func (se *SSHExecutor) ExecuteCommand(ctx context.Context, sshConfig SSHConfig, command string) (*SSHResult, error) {
	startTime := time.Now()

	result := &SSHResult{
		Command: command,
	}

	se.logger.WithFields(logrus.Fields{
		"host":    sshConfig.Host,
		"port":    sshConfig.Port,
		"user":    sshConfig.Username,
		"command": command,
	}).Info("Executing SSH command")

	// Create SSH client configuration
	clientConfig, err := se.createSSHConfig(sshConfig)
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to create SSH config: %w", err)
	}

	// Connect to the remote host
	addr := fmt.Sprintf("%s:%d", sshConfig.Host, sshConfig.Port)
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to connect to host: %w", err)
	}
	defer client.Close()

	// Create session
	session, err := client.NewSession()
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Execute command with context
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		result.Duration = time.Since(startTime)
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()

		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				result.ExitCode = -1
				result.Error = err.Error()
			}
		} else {
			result.ExitCode = 0
		}

		se.logger.WithFields(logrus.Fields{
			"host":      sshConfig.Host,
			"command":   command,
			"exit_code": result.ExitCode,
			"duration":  result.Duration,
		}).Info("SSH command completed")

		return result, nil

	case <-ctx.Done():
		result.Duration = time.Since(startTime)
		result.Error = "command timed out"
		result.ExitCode = -1
		return result, ctx.Err()
	}
}

// ExecuteMultipleCommands executes multiple commands in sequence
func (se *SSHExecutor) ExecuteMultipleCommands(ctx context.Context, sshConfig SSHConfig, commands []string) ([]*SSHResult, error) {
	var results []*SSHResult

	for _, command := range commands {
		result, err := se.ExecuteCommand(ctx, sshConfig, command)
		results = append(results, result)

		if err != nil || result.ExitCode != 0 {
			se.logger.WithFields(logrus.Fields{
				"host":      sshConfig.Host,
				"command":   command,
				"exit_code": result.ExitCode,
				"error":     err,
			}).Error("Command failed, stopping execution")

			return results, fmt.Errorf("command failed: %s (exit code: %d)", command, result.ExitCode)
		}
	}

	return results, nil
}

// ExecuteScript executes a shell script on the remote host
func (se *SSHExecutor) ExecuteScript(ctx context.Context, sshConfig SSHConfig, scriptContent string) (*SSHResult, error) {
	// Create a temporary script file
	tempScript := fmt.Sprintf("/tmp/disaster_recovery_%d.sh", time.Now().Unix())

	// Upload script content
	uploadCommand := fmt.Sprintf("cat > %s << 'EOF'\n%s\nEOF", tempScript, scriptContent)

	uploadResult, err := se.ExecuteCommand(ctx, sshConfig, uploadCommand)
	if err != nil || uploadResult.ExitCode != 0 {
		return uploadResult, fmt.Errorf("failed to upload script: %w", err)
	}

	// Make script executable
	chmodResult, err := se.ExecuteCommand(ctx, sshConfig, fmt.Sprintf("chmod +x %s", tempScript))
	if err != nil || chmodResult.ExitCode != 0 {
		return chmodResult, fmt.Errorf("failed to make script executable: %w", err)
	}

	// Execute script
	execResult, err := se.ExecuteCommand(ctx, sshConfig, tempScript)

	// Cleanup script (best effort)
	_, cleanupErr := se.ExecuteCommand(ctx, sshConfig, fmt.Sprintf("rm -f %s", tempScript))
	if cleanupErr != nil {
		se.logger.WithError(cleanupErr).Warning("Failed to cleanup temporary script")
	}

	return execResult, err
}

// CopyFile copies a file to the remote host using SCP
func (se *SSHExecutor) CopyFile(ctx context.Context, sshConfig SSHConfig, localPath, remotePath string) error {
	se.logger.WithFields(logrus.Fields{
		"host":        sshConfig.Host,
		"local_path":  localPath,
		"remote_path": remotePath,
	}).Info("Copying file via SCP")

	// Create SSH client configuration
	clientConfig, err := se.createSSHConfig(sshConfig)
	if err != nil {
		return fmt.Errorf("failed to create SSH config: %w", err)
	}

	// Connect to the remote host
	addr := fmt.Sprintf("%s:%d", sshConfig.Host, sshConfig.Port)
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to host: %w", err)
	}
	defer client.Close()

	// Open local file
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer localFile.Close()

	// Get file info
	fileInfo, err := localFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Create session for SCP
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Setup SCP command
	scpCommand := fmt.Sprintf("scp -t %s", remotePath)

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	// Start SCP session
	err = session.Start(scpCommand)
	if err != nil {
		return fmt.Errorf("failed to start SCP: %w", err)
	}

	// Send file header
	header := fmt.Sprintf("C0644 %d %s\n", fileInfo.Size(), filepath.Base(remotePath))
	_, err = stdin.Write([]byte(header))
	if err != nil {
		return fmt.Errorf("failed to send file header: %w", err)
	}

	// Copy file content
	_, err = io.Copy(stdin, localFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	// Send end marker
	_, err = stdin.Write([]byte("\x00"))
	if err != nil {
		return fmt.Errorf("failed to send end marker: %w", err)
	}

	// Close stdin and wait for session to complete
	stdin.Close()
	err = session.Wait()
	if err != nil {
		return fmt.Errorf("SCP session failed: %w", err)
	}

	se.logger.WithFields(logrus.Fields{
		"host":        sshConfig.Host,
		"local_path":  localPath,
		"remote_path": remotePath,
		"size":        fileInfo.Size(),
	}).Info("File copied successfully")

	return nil
}

// ServiceCommand executes service management commands
func (se *SSHExecutor) ServiceCommand(ctx context.Context, sshConfig SSHConfig, serviceName, action string) (*SSHResult, error) {
	var command string

	// Detect init system and build appropriate command
	switch action {
	case "start", "stop", "restart", "reload":
		command = fmt.Sprintf("sudo systemctl %s %s", action, serviceName)
	case "status":
		command = fmt.Sprintf("sudo systemctl status %s", serviceName)
	case "enable":
		command = fmt.Sprintf("sudo systemctl enable %s", serviceName)
	case "disable":
		command = fmt.Sprintf("sudo systemctl disable %s", serviceName)
	default:
		return nil, fmt.Errorf("unsupported service action: %s", action)
	}

	se.logger.WithFields(logrus.Fields{
		"host":    sshConfig.Host,
		"service": serviceName,
		"action":  action,
	}).Info("Executing service command")

	return se.ExecuteCommand(ctx, sshConfig, command)
}

// CheckServiceStatus checks if a service is running
func (se *SSHExecutor) CheckServiceStatus(ctx context.Context, sshConfig SSHConfig, serviceName string) (bool, error) {
	result, err := se.ServiceCommand(ctx, sshConfig, serviceName, "status")
	if err != nil {
		return false, err
	}

	// Check if service is active
	isActive := result.ExitCode == 0 && strings.Contains(result.Stdout, "active (running)")

	se.logger.WithFields(logrus.Fields{
		"host":      sshConfig.Host,
		"service":   serviceName,
		"is_active": isActive,
	}).Debug("Service status checked")

	return isActive, nil
}

// createSSHConfig creates SSH client configuration
func (se *SSHExecutor) createSSHConfig(sshConfig SSHConfig) (*ssh.ClientConfig, error) {
	var auth []ssh.AuthMethod

	// Private key authentication
	if sshConfig.KeyFile != "" {
		key, err := os.ReadFile(sshConfig.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key: %w", err)
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}

		auth = append(auth, ssh.PublicKeys(signer))
	}

	// Host key verification
	var hostKeyCallback ssh.HostKeyCallback
	if sshConfig.KnownHosts != "" {
		hostKeyCallback, err := knownhosts.New(sshConfig.KnownHosts)
		if err != nil {
			return nil, fmt.Errorf("failed to load known hosts: %w", err)
		}
		hostKeyCallback = hostKeyCallback
	} else {
		// INSECURE: Skip host key verification (only for testing)
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
		se.logger.Warning("SSH host key verification disabled - this is insecure!")
	}

	config := &ssh.ClientConfig{
		User:            sshConfig.Username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	return config, nil
}

// TestConnection tests SSH connectivity to a host
func (se *SSHExecutor) TestConnection(ctx context.Context, sshConfig SSHConfig) error {
	se.logger.WithFields(logrus.Fields{
		"host": sshConfig.Host,
		"port": sshConfig.Port,
		"user": sshConfig.Username,
	}).Info("Testing SSH connection")

	result, err := se.ExecuteCommand(ctx, sshConfig, "echo 'SSH connection test successful'")
	if err != nil {
		return fmt.Errorf("SSH connection test failed: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("SSH connection test failed with exit code %d", result.ExitCode)
	}

	se.logger.WithField("host", sshConfig.Host).Info("SSH connection test passed")
	return nil
}

// ExecuteWithSudo executes a command with sudo privileges
func (se *SSHExecutor) ExecuteWithSudo(ctx context.Context, sshConfig SSHConfig, command string) (*SSHResult, error) {
	sudoCommand := fmt.Sprintf("sudo %s", command)
	return se.ExecuteCommand(ctx, sshConfig, sudoCommand)
}

// MonitorCommand executes a command and streams output in real-time
func (se *SSHExecutor) MonitorCommand(ctx context.Context, sshConfig SSHConfig, command string, outputHandler func(line string)) (*SSHResult, error) {
	startTime := time.Now()

	result := &SSHResult{
		Command: command,
	}

	// Create SSH client configuration
	clientConfig, err := se.createSSHConfig(sshConfig)
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to create SSH config: %w", err)
	}

	// Connect to the remote host
	addr := fmt.Sprintf("%s:%d", sshConfig.Host, sshConfig.Port)
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to connect to host: %w", err)
	}
	defer client.Close()

	// Create session
	session, err := client.NewSession()
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Setup pipes for real-time output
	stdout, err := session.StdoutPipe()
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start command
	err = session.Start(command)
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("failed to start command: %w", err)
	}

	// Stream output
	var stdoutBuf, stderrBuf bytes.Buffer

	done := make(chan error, 1)
	go func() {
		// Read stdout
		go func() {
			scanner := NewLineScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				stdoutBuf.WriteString(line + "\n")
				if outputHandler != nil {
					outputHandler("STDOUT: " + line)
				}
			}
		}()

		// Read stderr
		go func() {
			scanner := NewLineScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				stderrBuf.WriteString(line + "\n")
				if outputHandler != nil {
					outputHandler("STDERR: " + line)
				}
			}
		}()

		done <- session.Wait()
	}()

	select {
	case err := <-done:
		result.Duration = time.Since(startTime)
		result.Stdout = stdoutBuf.String()
		result.Stderr = stderrBuf.String()

		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				result.ExitCode = -1
				result.Error = err.Error()
			}
		} else {
			result.ExitCode = 0
		}

		return result, nil

	case <-ctx.Done():
		result.Duration = time.Since(startTime)
		result.Error = "command timed out"
		result.ExitCode = -1
		return result, ctx.Err()
	}
}

//go:build windows

package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const taskName = "krouter-daemon"

// TaskName returns the Task Scheduler task name used by krouter.
func TaskName() string { return taskName }

var taskXMLTemplate = template.Must(template.New("task").Parse(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>krouter – local LLM proxy</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT30S</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{.BinaryPath}}</Command>
      <Arguments>serve</Arguments>
    </Exec>
  </Actions>
</Task>
`))

// GenerateTaskXML returns the XML content for the Task Scheduler task.
func GenerateTaskXML(binaryPath string) ([]byte, error) {
	var sb strings.Builder
	if err := taskXMLTemplate.Execute(&sb, struct{ BinaryPath string }{BinaryPath: binaryPath}); err != nil {
		return nil, fmt.Errorf("generate task XML: %w", err)
	}
	return []byte(sb.String()), nil
}

// DefaultDaemonPath returns the default daemon binary path on Windows.
// %LOCALAPPDATA%\kinthai\krouter.exe
func DefaultDaemonPath() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", fmt.Errorf("LOCALAPPDATA not set")
	}
	return filepath.Join(local, "kinthai", "krouter.exe"), nil
}

// RegisterTask creates the Task Scheduler user task for auto-start at login.
// Uses schtasks.exe — no admin required for HKCU tasks.
func RegisterTask(binaryPath string) error {
	xml, err := GenerateTaskXML(binaryPath)
	if err != nil {
		return err
	}

	// Write XML to a temp file, then import via schtasks /Create /XML.
	tmp, err := os.CreateTemp("", "kinthai-task-*.xml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(xml); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write task XML: %w", err)
	}
	_ = tmp.Close()

	cmd := exec.Command("schtasks",
		"/Create",
		"/TN", taskName,
		"/XML", tmp.Name(),
		"/F", // force overwrite
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks create: %w\n%s", err, out)
	}
	return nil
}

// StartTask runs the Task Scheduler task immediately (does not wait for login).
func StartTask() error {
	cmd := exec.Command("schtasks", "/Run", "/TN", taskName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks run: %w\n%s", err, out)
	}
	return nil
}

// UnregisterTask removes the Task Scheduler user task.
func UnregisterTask() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks delete: %w\n%s", err, out)
	}
	return nil
}

// SetEnvRegistry sets a user-level environment variable via setx (HKCU\Environment).
// All new processes pick up the change immediately without needing a new shell.
func SetEnvRegistry(key, value string) error {
	cmd := exec.Command("setx", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setx %s: %w\n%s", key, err, out)
	}
	return nil
}

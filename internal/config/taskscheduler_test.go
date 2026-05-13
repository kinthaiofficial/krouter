package config_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTaskXML_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub test only on non-Windows")
	}
	_, err := config.GenerateTaskXML(`C:\kinthai\krouter.exe`)
	assert.Error(t, err, "should return errWindowsOnly on non-Windows")
}

func TestGenerateTaskXML_ContainsRequiredFields(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("XML generation only works on Windows")
	}
	binaryPath := `C:\Users\test\AppData\Local\kinthai\krouter.exe`
	xml, err := config.GenerateTaskXML(binaryPath)
	require.NoError(t, err)

	content := string(xml)
	assert.True(t, strings.Contains(content, binaryPath), "XML must contain binary path")
	assert.True(t, strings.Contains(content, "LogonTrigger"), "XML must have logon trigger")
	assert.True(t, strings.Contains(content, "serve"), "XML must include 'serve' argument")
	assert.True(t, strings.Contains(content, "RestartOnFailure"), "XML must have restart policy")
}

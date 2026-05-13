package config_test

import (
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
)

// GeneratePlistContent is available on all platforms (defined in both build tag files).

func TestGeneratePlistContent_ContainsLabel(t *testing.T) {
	content := config.GeneratePlistContent("/home/user/.local/bin/krouter", "/home/user")
	s := string(content)
	assert.Contains(t, s, "com.kinthai.router")
}

func TestGeneratePlistContent_ContainsBinaryPath(t *testing.T) {
	content := config.GeneratePlistContent("/home/user/.local/bin/krouter", "/home/user")
	s := string(content)
	assert.Contains(t, s, "/home/user/.local/bin/krouter")
}

func TestGeneratePlistContent_ContainsServeArg(t *testing.T) {
	content := config.GeneratePlistContent("/usr/local/bin/krouter", "/Users/alice")
	s := string(content)
	assert.Contains(t, s, "<string>serve</string>")
}

func TestGeneratePlistContent_ContainsLogPaths(t *testing.T) {
	content := config.GeneratePlistContent("/bin/krouter", "/Users/bob")
	s := string(content)
	assert.Contains(t, s, "/Users/bob/.kinthai/daemon.log")
	assert.Contains(t, s, "/Users/bob/.kinthai/daemon-error.log")
}

func TestGeneratePlistContent_ValidXML(t *testing.T) {
	content := config.GeneratePlistContent("/bin/kr", "/home/u")
	s := string(content)
	assert.True(t, strings.HasPrefix(s, "<?xml"), "plist should start with XML declaration")
	assert.Contains(t, s, "<plist version=\"1.0\">")
	assert.Contains(t, s, "</plist>")
}

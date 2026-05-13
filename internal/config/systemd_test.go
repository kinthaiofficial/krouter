package config_test

import (
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGenerateServiceContent_ContainsBinaryPath(t *testing.T) {
	content := string(config.GenerateServiceContent("/usr/local/bin/krouter"))
	assert.Contains(t, content, "ExecStart=/usr/local/bin/krouter serve")
}

func TestGenerateServiceContent_HasRequiredSections(t *testing.T) {
	content := string(config.GenerateServiceContent("/home/user/.local/bin/krouter"))
	assert.True(t, strings.Contains(content, "[Unit]"), "[Unit] section missing")
	assert.True(t, strings.Contains(content, "[Service]"), "[Service] section missing")
	assert.True(t, strings.Contains(content, "[Install]"), "[Install] section missing")
	assert.Contains(t, content, "WantedBy=default.target")
	assert.Contains(t, content, "Restart=on-failure")
	assert.Contains(t, content, "Type=simple")
}

func TestGenerateServiceContent_HomePathEmbedded(t *testing.T) {
	content := string(config.GenerateServiceContent("/home/alice/.local/bin/krouter"))
	assert.Contains(t, content, "/home/alice/.local/bin/krouter")
}

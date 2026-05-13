package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows uses USERPROFILE instead of HOME
	return dir
}

func TestIsInstalled_False_WhenMarkerAbsent(t *testing.T) {
	withHome(t)
	assert.False(t, config.IsInstalled())
}

func TestIsInstalled_True_AfterMarkInstalled(t *testing.T) {
	home := withHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".kinthai"), 0700))
	require.NoError(t, config.MarkInstalled())
	assert.True(t, config.IsInstalled())
}

func TestMarkInstalled_CreatesDataDir(t *testing.T) {
	home := withHome(t)
	require.NoError(t, config.MarkInstalled())
	_, err := os.Stat(filepath.Join(home, ".kinthai", "installed"))
	assert.NoError(t, err)
}

func TestInstallDaemon_CopiesAndChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix-style executable permission bits")
	}
	home := withHome(t)

	// Create a fake source binary.
	src := filepath.Join(t.TempDir(), "fake-router")
	require.NoError(t, os.WriteFile(src, []byte("#!/bin/sh\necho ok"), 0755))

	dst, err := config.InstallDaemon(src)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".local", "bin", "krouter"), dst)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

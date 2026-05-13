package config_test

import (
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_NotNil(t *testing.T) {
	m := config.New("")
	assert.NotNil(t, m)
}

func TestManager_DefaultsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	s := m.Get()
	assert.Equal(t, "balanced", s.Preset)
	assert.Equal(t, "en", s.Language)
}

func TestManager_SetGet(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	require.NoError(t, m.Set(config.Settings{Preset: "saver", Language: "zh-CN"}))

	s := m.Get()
	assert.Equal(t, "saver", s.Preset)
	assert.Equal(t, "zh-CN", s.Language)
}

func TestManager_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	m1 := config.New(path)
	require.NoError(t, m1.Set(config.Settings{Preset: "quality"}))

	// A fresh manager reading the same file should see the persisted value.
	m2 := config.New(path)
	s := m2.Get()
	assert.Equal(t, "quality", s.Preset)
}

func TestManager_DefaultPreset_WhenPartialFile(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	// Set only Language; Preset should default to "balanced".
	require.NoError(t, m.Set(config.Settings{Language: "zh-CN"}))

	s := m.Get()
	assert.Equal(t, "balanced", s.Preset)
	assert.Equal(t, "zh-CN", s.Language)
}

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

func TestManager_DefaultDailyBudget_WhenNoFile(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	s := m.Get()
	assert.Equal(t, 50.0, s.BudgetWarnings["daily"],
		"first-run default daily budget should be $50")
}

func TestManager_DefaultDailyBudget_WhenKeyMissing(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	// Write a settings file with no budget_warnings key.
	require.NoError(t, m.Set(config.Settings{Preset: "saver"}))

	s := m.Get()
	assert.Equal(t, 50.0, s.BudgetWarnings["daily"],
		"daily key missing from saved file should default to $50")
}

func TestManager_DefaultDailyBudget_DoesNotOverrideExplicitZero(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	// User explicitly disables the budget by setting daily to 0.
	require.NoError(t, m.Set(config.Settings{
		BudgetWarnings: map[string]float64{"daily": 0},
	}))

	s := m.Get()
	assert.Equal(t, 0.0, s.BudgetWarnings["daily"],
		"explicit 0 must not be overridden by default")
}

func TestManager_DefaultDailyBudget_DoesNotOverrideUserValue(t *testing.T) {
	dir := t.TempDir()
	m := config.New(filepath.Join(dir, "settings.json"))

	require.NoError(t, m.Set(config.Settings{
		BudgetWarnings: map[string]float64{"daily": 100},
	}))

	s := m.Get()
	assert.Equal(t, 100.0, s.BudgetWarnings["daily"],
		"user-set limit must not be overridden by default")
}

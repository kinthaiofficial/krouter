package unit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
)

func TestConfigDefaults(t *testing.T) {
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))
	s := mgr.Get()
	if s.Preset != "balanced" {
		t.Errorf("default preset = %q, want balanced", s.Preset)
	}
	if s.Language != "en" {
		t.Errorf("default language = %q, want en", s.Language)
	}
}

func TestConfigRoundtrip(t *testing.T) {
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))

	s := mgr.Get()
	s.Preset = "saver"
	s.Language = "zh-CN"
	if err := mgr.Set(s); err != nil {
		t.Fatal(err)
	}

	s2 := mgr.Get()
	if s2.Preset != "saver" {
		t.Errorf("preset = %q, want saver", s2.Preset)
	}
	if s2.Language != "zh-CN" {
		t.Errorf("language = %q, want zh-CN", s2.Language)
	}
}

func TestConfigFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	mgr := config.New(path)
	if err := mgr.Set(mgr.Get()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0077 != 0 {
		t.Errorf("settings file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestConfigNotificationCategories(t *testing.T) {
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))
	s := mgr.Get()
	s.NotificationCategories = map[string]bool{
		"free_credit":     true,
		"kinthai_product": true,
		"tip":             false,
	}
	if err := mgr.Set(s); err != nil {
		t.Fatal(err)
	}

	s2 := mgr.Get()
	if !s2.NotificationCategories["free_credit"] {
		t.Error("free_credit should be true")
	}
	if s2.NotificationCategories["tip"] {
		t.Error("tip should be false")
	}
}

func TestConfigBudgetWarnings(t *testing.T) {
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))
	s := mgr.Get()
	s.BudgetWarnings = map[string]float64{
		"5h_window": 0.8,
		"weekly":    0.9,
	}
	if err := mgr.Set(s); err != nil {
		t.Fatal(err)
	}

	s2 := mgr.Get()
	if s2.BudgetWarnings["5h_window"] != 0.8 {
		t.Errorf("5h_window = %v, want 0.8", s2.BudgetWarnings["5h_window"])
	}
}

func TestConfigCorruptFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := config.New(path)
	s := mgr.Get()
	if s.Preset != "balanced" {
		t.Errorf("corrupt file should fall back to defaults, got preset %q", s.Preset)
	}
}

func TestShellInitBash(t *testing.T) {
	out := config.ShellInitOutput("bash")
	want := `export ANTHROPIC_BASE_URL="http://localhost:8402"`
	if !contains(out, want) {
		t.Errorf("bash output missing %q\ngot: %s", want, out)
	}
}

func TestShellInitZsh(t *testing.T) {
	out := config.ShellInitOutput("zsh")
	want := `export OPENAI_BASE_URL="http://localhost:8402/v1"`
	if !contains(out, want) {
		t.Errorf("zsh output missing %q\ngot: %s", want, out)
	}
}

func TestShellInitFish(t *testing.T) {
	out := config.ShellInitOutput("fish")
	want := `set -gx ANTHROPIC_BASE_URL "http://localhost:8402"`
	if !contains(out, want) {
		t.Errorf("fish output missing %q\ngot: %s", want, out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupInfo describes a config backup file.
type BackupInfo struct {
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	SizeKB    int       `json:"size_kb"`
}

// ListBackups returns backup files for configPath (*.kinthai-bak-*), newest first.
// Backup filename format: {base}.kinthai-bak-YYYY-MM-DD-HH-MM-SS
func ListBackups(configPath string) []BackupInfo {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	matches, err := filepath.Glob(filepath.Join(dir, base+".kinthai-bak-*"))
	if err != nil || len(matches) == 0 {
		return []BackupInfo{}
	}

	out := make([]BackupInfo, 0, len(matches))
	suffix := base + ".kinthai-bak-"
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		ts := strings.TrimPrefix(filepath.Base(m), suffix)
		t, _ := time.Parse("2006-01-02-15-04-05", ts)
		out = append(out, BackupInfo{
			Filename:  filepath.Base(m),
			Path:      m,
			CreatedAt: t,
			SizeKB:    int(fi.Size() / 1024),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// RestoreBackup overwrites configPath with the content from backupPath.
// Creates a safety backup of the current configPath first.
func RestoreBackup(configPath, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("backup: read %s: %w", backupPath, err)
	}
	current, err := os.ReadFile(configPath)
	if err == nil {
		if err := backupFile(configPath, current); err != nil {
			return err
		}
	}
	return os.WriteFile(configPath, data, 0600)
}

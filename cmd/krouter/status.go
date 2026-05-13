package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running (cannot read internal-token: %w)", err)
			}

			resp, err := callManagement(token, "/internal/status")
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("authentication failed (exit code 3)")
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}

			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			uptime := ""
			if secs, ok := body["uptime_seconds"].(float64); ok {
				uptime = formatUptime(int64(secs))
			}

			fmt.Printf("krouter running\n")
			fmt.Printf("  Version:   %v\n", body["version"])
			fmt.Printf("  Uptime:    %s\n", uptime)
			fmt.Printf("  PID:       %v\n", formatInt(body["pid"]))
			fmt.Printf("  Proxy:     127.0.0.1:%v\n", formatInt(body["proxy_port"]))
			fmt.Printf("  Mgmt:      127.0.0.1:%v\n", formatInt(body["mgmt_port"]))
			return nil
		},
	}
}

func formatUptime(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatInt(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%v", v)
}

// readInternalToken reads ~/.kinthai/internal-token.
func readInternalToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, ".kinthai", "internal-token"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// callManagement issues an authenticated GET to the management API.
func callManagement(token, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8403"+path, nil) //nolint:noctx
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

// callManagementPost issues an authenticated POST to the management API.
func callManagementPost(token, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8403"+path, body) //nolint:noctx
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

// callManagementDelete issues an authenticated DELETE to the management API.
func callManagementDelete(token, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, "http://127.0.0.1:8403"+path, nil) //nolint:noctx
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

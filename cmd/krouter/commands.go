package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/spf13/cobra"
)

// detectShell returns the shell name ("bash", "zsh", "fish", or "bash" as default).
func detectShell() string {
	return filepath.Base(os.Getenv("SHELL"))
}

// Stub subcommands for CLI operations that talk to the running daemon.
// Each implementation must:
//  1. Read internal-token from ~/.kinthai/internal-token
//  2. HTTP GET/POST to http://127.0.0.1:8403/internal/... with Bearer auth
//  3. Format response for terminal (TTY-aware)
//
// See spec/06-cli.md for output format requirements.
// See serve.go for the "serve" subcommand (implemented in M1.2).

func newShellInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Output shell integration code",
		Long:  "Outputs export statements to be eval'd in shell rc. Detects $SHELL and emits bash/zsh/fish syntax.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(config.ShellInitOutput(detectShell()))
			return nil
		},
	}
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get or set configuration",
	}
	cmd.AddCommand(newConfigGetCommand(), newConfigSetCommand(), newConfigListCommand())
	return cmd
}

func newConfigGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr := config.New("")
			v, err := configGetKey(mgr.Get(), args[0])
			if err != nil {
				return err
			}
			fmt.Println(v)
			return nil
		},
	}
}

func newConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr := config.New("")
			s, err := configSetKey(mgr.Get(), args[0], args[1])
			if err != nil {
				return err
			}
			if err := mgr.Set(s); err != nil {
				return fmt.Errorf("save settings: %w", err)
			}
			fmt.Printf("✓ %s updated to %s\n", args[0], args[1])
			return nil
		},
	}
}

func newConfigListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all config",
		RunE: func(_ *cobra.Command, _ []string) error {
			s := config.New("").Get()
			fmt.Printf("preset:    %s\n", s.Preset)
			fmt.Printf("language:  %s\n", s.Language)
			if len(s.NotificationCategories) > 0 {
				fmt.Println("notification_categories:")
				for k, v := range s.NotificationCategories {
					fmt.Printf("  %-20s %v\n", k+":", v)
				}
			}
			if len(s.BudgetWarnings) > 0 {
				fmt.Println("budget_warnings:")
				for k, v := range s.BudgetWarnings {
					fmt.Printf("  %-20s %g\n", k+":", v)
				}
			}
			return nil
		},
	}
}

// configGetKey returns the string representation of the named settings key.
// Valid keys: preset, language, notification_categories.<type>, budget_warnings.<quota>.
func configGetKey(s config.Settings, key string) (string, error) {
	switch key {
	case "preset":
		return s.Preset, nil
	case "language":
		return s.Language, nil
	default:
		if cat, ok := strings.CutPrefix(key, "notification_categories."); ok {
			v, exists := s.NotificationCategories[cat]
			if !exists {
				return "", fmt.Errorf("unknown notification category %q", cat)
			}
			return strconv.FormatBool(v), nil
		}
		if quota, ok := strings.CutPrefix(key, "budget_warnings."); ok {
			v, exists := s.BudgetWarnings[quota]
			if !exists {
				return "", fmt.Errorf("unknown budget_warnings key %q", quota)
			}
			return strconv.FormatFloat(v, 'g', -1, 64), nil
		}
		return "", fmt.Errorf("unknown key %q (valid: preset, language, notification_categories.<type>, budget_warnings.<quota>)", key)
	}
}

// configSetKey returns a copy of s with the named key updated to value.
func configSetKey(s config.Settings, key, value string) (config.Settings, error) {
	switch key {
	case "preset":
		if value != "saver" && value != "balanced" && value != "quality" {
			return s, fmt.Errorf("invalid preset %q (valid: saver, balanced, quality)", value)
		}
		s.Preset = value
	case "language":
		if value != "en" && value != "zh-CN" {
			return s, fmt.Errorf("invalid language %q (valid: en, zh-CN)", value)
		}
		s.Language = value
	default:
		if cat, ok := strings.CutPrefix(key, "notification_categories."); ok {
			b, err := strconv.ParseBool(value)
			if err != nil {
				return s, fmt.Errorf("notification_categories value must be true or false")
			}
			if s.NotificationCategories == nil {
				s.NotificationCategories = make(map[string]bool)
			}
			s.NotificationCategories[cat] = b
		} else if quota, ok := strings.CutPrefix(key, "budget_warnings."); ok {
			f, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return s, fmt.Errorf("budget_warnings value must be a number")
			}
			if s.BudgetWarnings == nil {
				s.BudgetWarnings = make(map[string]float64)
			}
			s.BudgetWarnings[quota] = f
		} else {
			return s, fmt.Errorf("unknown key %q (valid: preset, language, notification_categories.<type>, budget_warnings.<quota>)", key)
		}
	}
	return s, nil
}

func newBudgetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "budget",
		Short: "Show today's cost and savings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running (cannot read internal-token: %w)", err)
			}

			resp, err := callManagement(token, "/internal/usage")
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != 200 {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			var body map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			requests := 0
			if v, ok := body["requests_today"].(float64); ok {
				requests = int(v)
			}
			cost := 0.0
			if v, ok := body["cost_today_usd"].(float64); ok {
				cost = v
			}
			savings := 0.0
			if v, ok := body["savings_today_usd"].(float64); ok {
				savings = v
			}

			fmt.Printf("Today's usage:\n")
			fmt.Printf("  Requests:  %d\n", requests)
			fmt.Printf("  Cost:      $%.4f\n", cost)
			fmt.Printf("  Saved:     $%.4f (vs always using requested model)\n", savings)
			return nil
		},
	}
}

func newTestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Send test request to verify routing",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println("Sending test request to verify routing...")

			// Step 1: TCP connect to proxy.
			conn, err := net.DialTimeout("tcp", "127.0.0.1:8402", 3*time.Second)
			if err != nil {
				fmt.Println("  ✗ Proxy not reachable at 127.0.0.1:8402")
				return fmt.Errorf("proxy unreachable: %w", err)
			}
			_ = conn.Close()
			fmt.Println("  ✓ Proxy reachable at 127.0.0.1:8402")

			// Step 2: Read current preset from management API (best-effort).
			preset := "unknown"
			if token, tokenErr := readInternalToken(); tokenErr == nil {
				if resp, apiErr := callManagement(token, "/internal/preset"); apiErr == nil {
					defer func() { _ = resp.Body.Close() }()
					var body map[string]any
					if json.NewDecoder(resp.Body).Decode(&body) == nil {
						if p, ok := body["preset"].(string); ok {
							preset = p
						}
					}
				}
			}

			// Step 3: Make a real request through the proxy if an API key is available.
			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				fmt.Printf("  ✓ Management API responsive (preset: %s)\n", preset)
				fmt.Println("  ⚠ Skipping upstream test: ANTHROPIC_API_KEY not set")
				fmt.Println("\nConnectivity check PASSED. Set ANTHROPIC_API_KEY for full end-to-end test.")
				return nil
			}

			reqBody := `{"model":"claude-haiku-4-5","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
			req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8402/v1/messages", strings.NewReader(reqBody))
			if err != nil {
				return fmt.Errorf("build request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")

			probeStart := time.Now()
			resp, err := http.DefaultClient.Do(req)
			latency := time.Since(probeStart)
			if err != nil {
				fmt.Println("  ✗ Proxy request failed")
				return fmt.Errorf("proxy request failed: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)

			provider := resp.Header.Get("X-Krouter-Provider")
			model := resp.Header.Get("X-Krouter-Model")
			if provider != "" && model != "" {
				fmt.Printf("  ✓ Routing decided: %s/%s (%s preset)\n", provider, model, preset)
			}
			fmt.Printf("  ✓ Upstream response: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
			fmt.Printf("  ✓ Response time: %dms\n", latency.Milliseconds())

			if resp.StatusCode >= 400 {
				fmt.Printf("\nEnd-to-end test WARNING: upstream returned %d (check API key and provider config).\n", resp.StatusCode)
				return nil
			}
			fmt.Println("\nEnd-to-end test PASSED. Your routing is working correctly.")
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("krouter %s (built %s)\n", Version, BuildTime)
		},
	}
}

func newRemoteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote daemon access (LAN)",
		Long:  "Enable/disable remote daemon access for LAN-based GUI connections. See spec/10-remote-daemon.md.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Enable remote access and generate a pairing token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running: %w", err)
			}
			resp, err := callManagementPost(token, "/internal/remote/enable", nil)
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != 200 {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			var body map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			fmt.Printf("Remote access enabled.\nToken: %s\n", body["token"])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Disable remote access",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running: %w", err)
			}
			resp, err := callManagementPost(token, "/internal/remote/disable", nil)
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			fmt.Println("Remote access disabled.")
			return nil
		},
	})

	return cmd
}

func newPairCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Pairing token management (for remote daemon)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current pairing token and code",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running: %w", err)
			}
			resp, err := callManagement(token, "/internal/remote/status")
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			var body map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			if body["enabled"] != true {
				fmt.Println("Remote access is disabled. Run: krouter remote enable")
				return nil
			}
			fmt.Printf("Token:      %s\n", body["token"])
			if exp, ok := body["expires_in"].(float64); ok {
				fmt.Printf("Expires in: %.0fs\n", exp)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "devices",
		Short: "List paired devices",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running: %w", err)
			}
			resp, err := callManagement(token, "/internal/devices")
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			var items []map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&items); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			if len(items) == 0 {
				fmt.Println("No paired devices.")
				return nil
			}
			for _, d := range items {
				fmt.Printf("  %s  %s  (%s)\n", d["id"], d["name"], d["ip_address"])
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "revoke <device-id>",
		Short: "Revoke a paired device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running: %w", err)
			}
			resp, err := callManagementDelete(token, "/internal/devices/"+args[0])
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			fmt.Printf("Device %s revoked.\n", args[0])
			return nil
		},
	})

	return cmd
}

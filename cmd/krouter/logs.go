package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent routing logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := cmd.Flags().GetInt("lines")

			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running (cannot read internal-token: %w)", err)
			}

			resp, err := callManagement(token, fmt.Sprintf("/internal/logs?n=%d", n))
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

			var rows []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			if len(rows) == 0 {
				fmt.Println("(no requests logged yet)")
				return nil
			}

			for _, row := range rows {
				ts := parseTime(row["ts"])
				provider := strVal(row["provider"])
				model := strVal(row["model"])
				inTok := int64Val(row["input_tokens"])
				outTok := int64Val(row["output_tokens"])
				costUSD := floatVal(row["cost_usd"])
				latMS := int64Val(row["latency_ms"])
				status := int64Val(row["status_code"])

				fmt.Printf("[%s] %s/%s  (%dK in / %dK out / $%.4f / %dms / %d)\n",
					ts.Format("2006-01-02 15:04:05"),
					provider, model,
					inTok/1000, outTok/1000,
					costUSD,
					latMS,
					status,
				)
			}
			return nil
		},
	}
	cmd.Flags().IntP("lines", "n", 50, "Number of lines to show")
	return cmd
}

func parseTime(v any) time.Time {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Local()
		}
	}
	return time.Time{}
}

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func floatVal(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func int64Val(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

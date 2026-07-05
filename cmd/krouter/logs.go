package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent routing logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := cmd.Flags().GetInt("lines")
			follow, _ := cmd.Flags().GetBool("follow")

			token, err := readInternalToken()
			if err != nil {
				return fmt.Errorf("daemon not running (cannot read internal-token: %w)", err)
			}

			// In follow mode, subscribe to the event stream BEFORE fetching the
			// snapshot so a request completing in between is not lost; rows the
			// snapshot already printed are skipped by id when the stream drains.
			var events *http.Response
			if follow {
				events, err = callManagement(token, "/internal/events")
				if err != nil {
					return fmt.Errorf("daemon unreachable: %w", err)
				}
				defer func() { _ = events.Body.Close() }()
				if events.StatusCode == http.StatusUnauthorized {
					return fmt.Errorf("authentication failed (exit code 3)")
				}
				if events.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status %d", events.StatusCode)
				}
			}

			rows, err := fetchLogRows(token, n)
			if err != nil {
				return err
			}

			if len(rows) == 0 && !follow {
				fmt.Println("(no requests logged yet)")
				return nil
			}

			// The API returns newest-first; print oldest-first so the latest
			// entry sits at the bottom, like tail.
			seen := make(map[string]bool, len(rows))
			for i := len(rows) - 1; i >= 0; i-- {
				fmt.Println(formatLogRow(rows[i]))
				seen[fmt.Sprintf("%v", rows[i]["id"])] = true
			}

			if !follow {
				return nil
			}
			return followLogs(events.Body, seen)
		},
	}
	cmd.Flags().IntP("lines", "n", 50, "Number of lines to show")
	cmd.Flags().BoolP("follow", "f", false, "Stream new requests as they complete (like tail -f)")
	return cmd
}

// fetchLogRows returns the most recent n request rows from the management API,
// newest first (the API's native order).
func fetchLogRows(token string, n int) ([]map[string]any, error) {
	resp, err := callManagement(token, fmt.Sprintf("/internal/logs?n=%d", n))
	if err != nil {
		return nil, fmt.Errorf("daemon unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (exit code 3)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return rows, nil
}

// followLogs streams request_completed SSE events and prints each as a log
// line, skipping ids already shown in the snapshot. Blocks until the stream
// ends (daemon shutdown) or the process is interrupted.
func followLogs(stream io.Reader, seen map[string]bool) error {
	err := scanSSE(stream, func(event string, data []byte) {
		if event != "request_completed" {
			return
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			return
		}
		if seen[fmt.Sprintf("%v", row["id"])] {
			return
		}
		fmt.Println(formatLogRow(row))
	})
	if err != nil {
		return fmt.Errorf("event stream: %w", err)
	}
	return fmt.Errorf("event stream closed (daemon stopped?)")
}

// scanSSE parses a Server-Sent Events stream, invoking fn once per complete
// event (terminated by a blank line) with its event name and data payload.
// Comment lines (": ping" heartbeats) are ignored. Returns when the stream
// ends; a partial event cut off mid-frame is not dispatched.
func scanSSE(r io.Reader, fn func(event string, data []byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event string
	var data []byte
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if event != "" || len(data) > 0 {
				fn(event, data)
			}
			event, data = "", nil
		case strings.HasPrefix(line, ":"):
			// comment / heartbeat
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, chunk...)
		}
	}
	return sc.Err()
}

// formatLogRow renders one request row (from /internal/logs or the mirrored
// request_completed SSE payload) as a single log line.
func formatLogRow(row map[string]any) string {
	ts := parseTime(row["ts"])
	provider := strVal(row["provider"])
	model := strVal(row["model"])
	inTok := int64Val(row["input_tokens"])
	outTok := int64Val(row["output_tokens"])
	costUSD := floatVal(row["cost_usd"])
	latMS := int64Val(row["latency_ms"])
	status := int64Val(row["status_code"])

	return fmt.Sprintf("[%s] %s/%s  (%dK in / %dK out / $%.4f / %dms / %d)",
		ts.Format("2006-01-02 15:04:05"),
		provider, model,
		inTok/1000, outTok/1000,
		costUSD,
		latMS,
		status,
	)
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

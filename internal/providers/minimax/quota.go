package minimax

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

const quotaAPIURL = "https://api.minimaxi.com/v1/token_plan/remains"

// QuotaPoller fetches MiniMax subscription quota from token_plan/remains and
// writes the result to the subscription_quota_cache table.
//
// It uses the most recently cached OAuth token (set by CacheOAuthToken on each
// proxied MiniMax request). If no token has been seen yet, polling is skipped.
//
// Poll schedule:
//   - Normal interval: 30 minutes
//   - When current window ends in < 30 minutes: poll every 5 minutes
//   - Skips poll if GetCachedToken() == "" (no MiniMax traffic seen yet)
type QuotaPoller struct {
	store      *storage.Store
	httpClient *http.Client
}

// NewQuotaPoller creates a QuotaPoller backed by the given store and HTTP client.
func NewQuotaPoller(store *storage.Store, client *http.Client) *QuotaPoller {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &QuotaPoller{store: store, httpClient: client}
}

// Start runs the polling loop until ctx is cancelled.
func (p *QuotaPoller) Start(ctx context.Context) {
	// Initial poll after a short delay so daemon startup is not blocked.
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = p.PollOnce(ctx)
			timer.Reset(p.nextInterval(ctx))
		}
	}
}

// PollOnce performs a single quota fetch and persists results. Exported for testing.
func (p *QuotaPoller) PollOnce(ctx context.Context) error {
	token := GetCachedToken()
	if token == "" {
		return nil // no MiniMax traffic yet; skip silently
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaAPIURL, nil)
	if err != nil {
		return fmt.Errorf("minimax quota: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("minimax quota: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return fmt.Errorf("minimax quota: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("minimax quota: upstream %d: %s", resp.StatusCode, body)
	}

	quotas, err := parseQuotaResponse(body)
	if err != nil {
		return fmt.Errorf("minimax quota: parse: %w", err)
	}

	now := time.Now().UTC()
	for _, q := range quotas {
		q.FetchedAt = now
		if err := p.store.UpsertSubscriptionQuota(ctx, q); err != nil {
			return fmt.Errorf("minimax quota: save %s: %w", q.ModelPattern, err)
		}
	}
	return nil
}

// nextInterval returns the polling interval based on how soon the current window ends.
func (p *QuotaPoller) nextInterval(ctx context.Context) time.Duration {
	quotas, err := p.store.GetAllSubscriptionQuotas(ctx)
	if err != nil || len(quotas) == 0 {
		return 30 * time.Minute
	}
	now := time.Now().UTC()
	var earliest time.Time
	for _, q := range quotas {
		if q.Provider != "minimax" {
			continue
		}
		if earliest.IsZero() || q.WindowEnd.Before(earliest) {
			earliest = q.WindowEnd
		}
	}
	if !earliest.IsZero() && earliest.Sub(now) < 30*time.Minute {
		return 5 * time.Minute
	}
	return 30 * time.Minute
}

// quotaAPIResponse mirrors the JSON returned by token_plan/remains.
type quotaAPIResponse struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	ModelRemains []struct {
		ModelName                   string `json:"model_name"`
		StartTime                   int64  `json:"start_time"`
		EndTime                     int64  `json:"end_time"`
		CurrentIntervalTotalCount   int64  `json:"current_interval_total_count"`
		CurrentIntervalUsageCount   int64  `json:"current_interval_usage_count"`
	} `json:"model_remains"`
}

// parseQuotaResponse converts the raw API response into storage rows.
// Only text model plans (MiniMax-M*) are stored; speech/video plans are skipped.
func parseQuotaResponse(data []byte) ([]storage.SubscriptionQuota, error) {
	var resp quotaAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if resp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("API error %d: %s", resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg)
	}

	var out []storage.SubscriptionQuota
	for _, m := range resp.ModelRemains {
		// Only track text generation models (MiniMax-M*).
		if !strings.HasPrefix(m.ModelName, "MiniMax-M") {
			continue
		}
		if m.CurrentIntervalTotalCount == 0 {
			continue // weekly-only quota or inactive plan; skip
		}
		highspeed := strings.Contains(strings.ToLower(m.ModelName), "highspeed")
		out = append(out, storage.SubscriptionQuota{
			Provider:     "minimax",
			ModelPattern: m.ModelName,
			WindowStart:  time.UnixMilli(m.StartTime).UTC(),
			WindowEnd:    time.UnixMilli(m.EndTime).UTC(),
			TotalCount:   m.CurrentIntervalTotalCount,
			UsedCount:    m.CurrentIntervalUsageCount,
			Highspeed:    highspeed,
		})
	}
	return out, nil
}

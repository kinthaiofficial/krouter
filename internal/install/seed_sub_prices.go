package install

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/kinthaiofficial/krouter/data"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// subPricesSeedJSON aliases the canonical embedded copy held in the data
// package. The source-of-truth file lives at data/token_price_sub.json in
// the repo root so it can be served simultaneously by GitHub raw (see
// internal/subpricing for the daemon's fetch loop) and by this embed —
// same content, two distribution channels.
var subPricesSeedJSON = data.SubPricesSeedJSON

// subPricesFile mirrors data/token_price_sub.json so we can decode it.
// Fields with zero JSON value are tolerated: pricing data we don't know
// (e.g. cny_to_usd missing for a future non-CNY vendor) becomes 0 in DB,
// which the consumers treat as "unknown SKU, effective cost 0".
type subPricesFile struct {
	SchemaVersion int            `json:"schema_version"`
	Tiers         []subPriceTier `json:"tiers"`
}

type subPriceTier struct {
	Provider        string  `json:"provider"`
	TierPattern     string  `json:"tier_pattern"`
	TotalCount      int64   `json:"total_count"`
	Highspeed       bool    `json:"highspeed"`
	MonthlyPriceCNY float64 `json:"monthly_price_cny"`
	WindowHours     int     `json:"window_hours"`
	CNYToUSD        float64 `json:"cny_to_usd"`
	DataSourceURL   string  `json:"data_source_url"`
}

// SeedSubPrices opens the daemon's DB (creating it if absent), runs the
// migrations (idempotent), and upserts each tier from the embedded
// token_price_sub.json into the token_price_sub table.
//
// Called by Orchestrator.Install before RegisterService so the daemon
// has prices the moment it boots. Re-running the installer (upgrade
// path) re-applies the embedded JSON, picking up any pricing edits the
// developer made between releases.
func (o *Orchestrator) SeedSubPrices() error {
	if o.opt.DryRun {
		return nil
	}

	dbPath, err := installDBPath()
	if err != nil {
		o.ui.Warn("  seed sub prices: " + err.Error())
		return nil // non-fatal — daemon will create DB itself, just no seed
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	var file subPricesFile
	if err := json.Unmarshal(subPricesSeedJSON, &file); err != nil {
		return fmt.Errorf("parse embedded token_price_sub.json: %w", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	for _, t := range file.Tiers {
		row := storage.SubscriptionPrice{
			Provider:        t.Provider,
			TierPattern:     t.TierPattern,
			TotalCount:      t.TotalCount,
			Highspeed:       t.Highspeed,
			MonthlyPriceCNY: t.MonthlyPriceCNY,
			WindowHours:     t.WindowHours,
			CNYToUSD:        t.CNYToUSD,
			DataSourceURL:   t.DataSourceURL,
			UpdatedAt:       now,
		}
		if err := store.UpsertSubscriptionPrice(ctx, row); err != nil {
			return fmt.Errorf("upsert %s/%d: %w", t.Provider, t.TotalCount, err)
		}
	}
	o.ui.Progress(fmt.Sprintf("  → %d subscription tiers seeded", len(file.Tiers)))
	return nil
}

// installDBPath returns ~/.kinthai/data.db (matches cmd/krouter/serve.go's
// defaultDBPath; duplicated here to avoid the installer package importing
// from main).
func installDBPath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data.db"), nil
}

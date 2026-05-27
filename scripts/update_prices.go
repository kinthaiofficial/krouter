//go:build ignore

// update_prices fetches the LiteLLM model pricing JSON, merges it with the
// local supplement file (data/token_prices_ext.json), and writes the result
// to data/token_prices.json in krouter's canonical format.
//
// Usage:
//
//	go run scripts/update_prices.go
//
// The script is designed to be run from the repository root. It is also
// invoked by the .github/workflows/update-litellm-prices.yml GitHub Action
// on a daily schedule so that running daemons pick up new model prices within
// 24 hours without requiring a binary release.
//
// Output format: a thin JSON wrapper around the raw LiteLLM model map so the
// daemon's existing liteLLMEntry parser can be reused without modification.
// All LiteLLM fields are preserved verbatim — nothing is renamed or dropped.
// The supplement file's entries override LiteLLM entries with the same key.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	liteLLMURL  = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	extPath     = "data/token_prices_ext.json"
	outputPath  = "data/token_prices.json"
	maxBodySize = 100 << 20 // 100 MB
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "update_prices: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Load supplement (models absent from LiteLLM).
	ext, err := loadExt(extPath)
	if err != nil {
		return fmt.Errorf("load ext: %w", err)
	}
	fmt.Fprintf(os.Stderr, "ext: %d supplement models\n", len(ext))

	// 2. Fetch LiteLLM pricing JSON.
	body, sha, err := fetchLiteLLM()
	if err != nil {
		return fmt.Errorf("fetch litellm: %w", err)
	}
	fmt.Fprintf(os.Stderr, "litellm: fetched %d bytes, sha256=%s\n", len(body), sha[:12])

	// 3. Parse LiteLLM flat map (model_id → raw JSON object).
	var litellm map[string]json.RawMessage
	if err := json.Unmarshal(body, &litellm); err != nil {
		return fmt.Errorf("parse litellm json: %w", err)
	}
	fmt.Fprintf(os.Stderr, "litellm: %d models\n", len(litellm))

	// 4. Merge: start with LiteLLM, then overlay ext (ext wins on conflict).
	merged := make(map[string]json.RawMessage, len(litellm)+len(ext))
	for k, v := range litellm {
		merged[k] = v
	}
	for k, v := range ext {
		merged[k] = v
	}
	fmt.Fprintf(os.Stderr, "merged: %d total models (%d ext overrides)\n", len(merged), len(ext))

	// 5. Sort keys for a stable, diff-friendly output.
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	orderedModels := make(map[string]json.RawMessage, len(merged))
	for _, k := range keys {
		orderedModels[k] = merged[k]
	}

	// 6. Write output file with thin wrapper.
	out := struct {
		SchemaVersion int                        `json:"schema_version"`
		GeneratedAt   string                     `json:"generated_at"`
		SourceSHA256  string                     `json:"source_sha256"`
		Models        map[string]json.RawMessage `json:"models"`
	}{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		SourceSHA256:  sha,
		Models:        orderedModels,
	}

	outBytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	outBytes = append(outBytes, '\n')

	if err := os.WriteFile(outputPath, outBytes, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", outputPath, len(outBytes))
	return nil
}

// fetchLiteLLM downloads the LiteLLM pricing JSON and returns its bytes and
// hex-encoded SHA-256 hash.
func fetchLiteLLM() ([]byte, string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(liteLLMURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, liteLLMURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	sum := sha256.Sum256(body)
	return body, fmt.Sprintf("%x", sum), nil
}

// loadExt reads data/token_prices_ext.json and returns the models map with
// each entry re-encoded as a json.RawMessage for direct injection into the
// merged output.
func loadExt(path string) (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file struct {
		SchemaVersion int                        `json:"schema_version"`
		Models        map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported schema_version=%d in %s", file.SchemaVersion, path)
	}
	return file.Models, nil
}

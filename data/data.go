// Package data exposes static seed data files (currently just MiniMax
// subscription pricing) to other packages via go:embed.
//
// The JSON files in this directory are the *source of truth* and serve two
// roles simultaneously:
//
//  1. They are embedded into the installer binary at build time (see
//     SubPricesSeedJSON below), so a fresh install always has at least the
//     pricing snapshot that shipped with that binary.
//
//  2. They are served by GitHub raw at
//     https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/
//     so a running daemon can pick up edits between releases by polling
//     that URL with an ETag — see internal/subpricing for the fetch loop.
//
// Editing one of these files and committing to main is therefore the
// canonical way to roll a pricing update: all existing v2.x daemons learn
// the new prices on their next daily poll, *without* the user upgrading the
// binary. The next release that builds from the same commit picks up the
// edit automatically through go:embed.
//
// **Do not move these files** without updating both the daemon's fetch URL
// (internal/subpricing/sync.go) and the installer's seed reference
// (internal/install/seed_sub_prices.go). The on-disk path is part of the
// HTTP contract that running daemons in the wild already depend on.
package data

import _ "embed"

// SubPricesSeedJSON is the byte content of data/token_price_sub.json,
// embedded at compile time. Installer uses it to seed the token_price_sub
// DB table on first install. The daemon's remote-sync loop later refreshes
// the same table from the GitHub raw URL.
//
//go:embed token_price_sub.json
var SubPricesSeedJSON []byte

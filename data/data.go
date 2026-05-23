// Package data exposes static JSON files (curated by krouter operators)
// to the rest of the codebase via go:embed.
//
// Files in this directory share two distribution channels:
//
//  1. They are embedded into the installer binary at build time, providing
//     an offline seed used at first daemon launch.
//  2. They are served by https://krouter.kinthai.ai/data/<file> and
//     mirrored at https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/<file>,
//     so running daemons can pick up edits without a binary upgrade.
//
// **The on-disk path is part of an HTTP contract** — moving these files
// breaks the live URL that running v2.x daemons in the wild poll. Don't
// rename without coordinating with operations to update mirrors.
//
// Editing a JSON here and committing to main is the canonical way to roll
// an update: existing daemons learn the change on their next 24-hour
// poll, and the next krouter release picks up the same edit via go:embed.
package data

import _ "embed"

// FreeTokensSeedJSON is the byte content of data/free_tokens.json,
// embedded at compile time. Provides the offline-seed list of providers
// offering free trial credits / daily quotas / free tiers, with signup
// URLs the daemon exposes through /internal/free-providers.
//
// Refreshed at runtime by internal/freeproviders/sync.go from
// https://krouter.kinthai.ai/data/free_tokens.json (primary) with
// fallback to https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/free_tokens.json.
//
//go:embed free_tokens.json
var FreeTokensSeedJSON []byte

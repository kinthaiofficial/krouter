// Package webui embeds the compiled Web UI static assets.
// The dist/ directory is populated by `npm run build` in the frontend/ source tree.
package webui

import "embed"

//go:embed all:dist
var Assets embed.FS

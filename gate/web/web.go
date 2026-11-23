// Package web embeds the launcher page and its static assets into the binary.
package web

import "embed"

// Assets holds index.html and the static/ directory.
//
//go:embed index.html static
var Assets embed.FS

package web

import "embed"

// Assets contains the static web UI bundled into the binary.
//
//go:embed all:web
var Assets embed.FS
